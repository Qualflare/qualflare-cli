// Package http provides a resty-based HTTP client for the Qualflare API.
package http

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"qualflare-cli/internal/version"
	"strings"

	"resty.dev/v3"
)

const apiBasePath = "/api/v1"

// Client handles HTTP communication with the API
type Client struct {
	resty    *resty.Client
	config   ports.ConfigProvider
	endpoint string
}

// ClientOption is a function that configures the client
type ClientOption func(*Client)

// NewHTTPClient creates a new HTTP client
func NewHTTPClient(config ports.ConfigProvider, opts ...ClientOption) *Client {
	maxRetries, baseDelay, maxDelay := config.GetRetryConfig()

	rc := resty.New().
		SetTimeout(config.GetTimeout()).
		SetRetryCount(maxRetries).
		SetRetryWaitTime(baseDelay).
		SetRetryMaxWaitTime(maxDelay).
		// resty v3 refuses to retry non-idempotent methods (POST) unless this is
		// set, so without it the whole retry policy above is dead for the CLI's
		// primary job — a single transient 429/503 fails the upload (CLI-H2).
		// Safe here because SendReport sends a stable Idempotency-Key that the
		// server resolves to the existing launch, so a retry cannot double-create.
		SetAllowNonIdempotentRetry(true).
		// Do not follow redirects: the auth middleware re-runs on every hop, so
		// a redirect to a different host would forward QF_TOKEN to that host.
		SetRedirectPolicy(resty.NoRedirectPolicy()).
		SetHeader("User-Agent", version.UserAgent()).
		SetHeader("Accept", "application/json").
		AddRetryConditions(func(resp *resty.Response, err error) bool {
			if err != nil {
				return true
			}
			sc := resp.StatusCode()
			return sc == http.StatusTooManyRequests ||
				sc == http.StatusInternalServerError ||
				sc == http.StatusBadGateway ||
				sc == http.StatusServiceUnavailable ||
				sc == http.StatusGatewayTimeout
		})

	// Add auth header middleware
	rc.AddRequestMiddleware(func(c *resty.Client, req *resty.Request) error {
		if apiKey := config.GetAPIKey(); apiKey != "" {
			req.SetHeader("QF_TOKEN", apiKey)
		}
		return nil
	})

	// --debug logs the full request/response to stderr. The OnDebugLog callback
	// runs BEFORE resty formats the output, so redacting the token header here
	// guarantees the credential never appears in the log (OBS-06).
	if config.IsDebug() {
		rc.EnableDebug()
		rc.OnDebugLog(redactDebugLog)
	}

	c := &Client{
		resty:    rc,
		config:   config,
		endpoint: strings.TrimRight(config.GetAPIEndpoint(), "/"),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Close releases resources held by the client
func (c *Client) Close() {
	_ = c.resty.Close()
}

// SendReport sends a report to the API
func (c *Client) SendReport(ctx context.Context, report *domain.Launch) error {
	url := c.endpoint + apiBasePath + "/collect"
	if c.config.IsVerbose() {
		fmt.Fprintf(os.Stderr, "POST %s\n", url) // diagnostics on stderr (BUG-04)
	}
	req := c.resty.R().
		SetContext(ctx).
		SetBody(report)
	// Idempotency-Key dedupes retries server-side: resty reuses this same request
	// across its retry attempts, so a 5xx/timeout that fires after the server
	// already committed won't create a duplicate launch (or double-count quota).
	if key := newIdempotencyKey(); key != "" {
		req.SetHeader("Idempotency-Key", key)
	}
	resp, err := req.Post(url)
	if err != nil {
		return &APIError{Op: "send", Message: "failed to send request", Err: err}
	}

	if resp.IsSuccess() {
		return nil
	}

	return c.buildAPIError("send", resp)
}

// newIdempotencyKey returns a random RFC 4122 v4 UUID string (≤255 chars, the
// server's limit). One key is generated per collect invocation; resty reuses the
// underlying request across retries so the key stays stable, letting the server
// resolve a retried upload to the existing launch. Returns "" on the (practically
// impossible) RNG failure so the caller omits the header rather than failing.
func newIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10 (RFC 4122)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Get performs a GET request to the API. path must include the full API path (e.g. /api/v1/suites).
func (c *Client) Get(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	reqURL := c.endpoint + path

	if c.config.IsVerbose() {
		display := reqURL
		if len(params) > 0 {
			display += "?" + params.Encode()
		}
		fmt.Fprintf(os.Stderr, "GET %s\n", display) // diagnostics on stderr (BUG-04)
	}

	req := c.resty.R().SetContext(ctx)
	if len(params) > 0 {
		req.SetQueryParamsFromValues(params)
	}

	resp, err := req.Get(reqURL)
	if err != nil {
		return nil, &APIError{Op: "get", Message: "failed to send request", Err: err}
	}

	if resp.IsSuccess() {
		return json.RawMessage(resp.Bytes()), nil
	}

	return nil, c.buildAPIError("get", resp)
}

// redactDebugLog strips the API token from a --debug log entry before resty
// formats it. resty invokes this callback BEFORE the formatter, so the mutation
// takes effect and the credential never reaches stderr (OBS-06).
func redactDebugLog(dl *resty.DebugLog) {
	if dl == nil || dl.Request == nil || dl.Request.Header == nil {
		return
	}
	if dl.Request.Header.Get("QF_TOKEN") != "" {
		dl.Request.Header.Set("QF_TOKEN", "***REDACTED***")
	}
	dl.Request.Header.Del("Authorization")
}

// buildAPIError creates an APIError from a non-success response
func (c *Client) buildAPIError(op string, resp *resty.Response) *APIError {
	apiErr := &APIError{
		Op:         op,
		StatusCode: resp.StatusCode(),
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(resp.Bytes(), &errResp); err == nil {
		apiErr.Code = errResp.Code
		apiErr.RequestID = errResp.RequestID
		apiErr.Message = resolveErrorMessage(errResp, resp.StatusCode())
	} else {
		apiErr.Message = fmt.Sprintf("API request failed with status %d", resp.StatusCode())
	}

	return apiErr
}

// resolveErrorMessage picks the message to show the user. It prefers the server's
// own message (the enriched envelope is client-safe for 4xx), falling back to a
// CLI-specific friendly hint only when the server gave no message (SYNC-01/10).
// The previous order let a hardcoded friendly string override the server — and
// mapped the GENERIC 404 code common.resource_not_found to "Language not found",
// so a missing cluster or suite rendered as a BCP-47 language error.
func resolveErrorMessage(errResp ErrorResponse, statusCode int) string {
	switch {
	case errResp.Message != "":
		return errResp.Message
	case getUserFriendlyMessage(errResp.Code) != "":
		return getUserFriendlyMessage(errResp.Code)
	case errResp.Error != "":
		return errResp.Error
	default:
		return fmt.Sprintf("API request failed with status %d", statusCode)
	}
}

// APIError represents an API error
type APIError struct {
	Op         string
	Message    string
	Code       string
	StatusCode int
	RequestID  string
	Err        error
}

// actionHint returns a short next-step for common auth/authz failures so the
// error is fixable, not just descriptive (OBS-05).
func (e *APIError) actionHint() string {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return " — run `qf login <identifier> <token>` to re-authenticate"
	case http.StatusForbidden:
		return " — the token lacks access to this project"
	case http.StatusPaymentRequired:
		return " — a plan limit was reached; check your Qualflare subscription"
	}
	return ""
}

func (e *APIError) Error() string {
	// Surface the server's request_id so a user can quote it to support (OBS-01).
	suffix := e.actionHint()
	if e.RequestID != "" {
		suffix += fmt.Sprintf(" [request_id: %s]", e.RequestID)
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s: %s (status: %d)%s", e.Op, e.Message, e.StatusCode, suffix)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v%s", e.Op, e.Message, e.Err, suffix)
	}
	return fmt.Sprintf("%s: %s%s", e.Op, e.Message, suffix)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// ErrorResponse mirrors the server's enriched error envelope
// {code, message?, request_id?, fields?}. `error` is legacy/back-compat.
type ErrorResponse struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

// API error codes — must match server's result.Code constants (dot-notation).
// NOTE: common.resource_not_found is the GENERIC 404 and must NOT be aliased to a
// specific resource — doing so rendered every 404 as "Language not found" (SYNC-10).
const (
	ErrCodeEnvironmentNotFound = "environment.not_found"
	ErrCodeMilestoneNotFound   = "milestone.not_found"
	ErrCodeValidationFailed    = "common.validation_failed"
)

// getUserFriendlyMessage returns a CLI-specific hint for a few known codes. It is
// only used as a fallback when the server sent no message (see buildAPIError).
func getUserFriendlyMessage(code string) string {
	switch code {
	case ErrCodeEnvironmentNotFound:
		return "Environment not found. Please check the environment name or create it in Qualflare."
	case ErrCodeMilestoneNotFound:
		return "Milestone not found. Please check the milestone sequence number or create it in Qualflare."
	case ErrCodeValidationFailed:
		return "Validation failed. Please check your request data."
	default:
		return ""
	}
}
