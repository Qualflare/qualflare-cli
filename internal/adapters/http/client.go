// Package http provides a resty-based HTTP client for the Qualflare API.
package http

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
		fmt.Printf("POST %s\n", url)
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
		fmt.Printf("GET %s\n", display)
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

// buildAPIError creates an APIError from a non-success response
func (c *Client) buildAPIError(op string, resp *resty.Response) *APIError {
	apiErr := &APIError{
		Op:         op,
		StatusCode: resp.StatusCode(),
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(resp.Bytes(), &errResp); err == nil {
		apiErr.Code = errResp.Code
		if friendlyMsg := getUserFriendlyMessage(errResp.Code); friendlyMsg != "" {
			apiErr.Message = friendlyMsg
		} else if errResp.Error != "" {
			apiErr.Message = errResp.Error
		} else if errResp.Message != "" {
			apiErr.Message = errResp.Message
		} else {
			apiErr.Message = fmt.Sprintf("API request failed with status %d", resp.StatusCode())
		}
	} else {
		apiErr.Message = fmt.Sprintf("API request failed with status %d", resp.StatusCode())
	}

	return apiErr
}

// APIError represents an API error
type APIError struct {
	Op         string
	Message    string
	Code       string
	StatusCode int
	Err        error
}

func (e *APIError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s: %s (status: %d)", e.Op, e.Message, e.StatusCode)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// API error codes — must match server's result.Code constants (dot-notation).
const (
	ErrCodeEnvironmentNotFound = "environment.not_found"
	ErrCodeMilestoneNotFound   = "milestone.not_found"
	ErrCodeValidationFailed    = "common.validation_failed"
	ErrCodeLanguageNotFound    = "common.resource_not_found"
)

// getUserFriendlyMessage returns a user-friendly error message for known error codes
func getUserFriendlyMessage(code string) string {
	switch code {
	case ErrCodeEnvironmentNotFound:
		return "Environment not found. Please check the environment name or create it in Qualflare."
	case ErrCodeMilestoneNotFound:
		return "Milestone not found. Please check the milestone ID or create it in Qualflare."
	case ErrCodeLanguageNotFound:
		return "Language not found. Please use a valid BCP 47 language code (e.g., en-US, de-DE)."
	case ErrCodeValidationFailed:
		return "Validation failed. Please check your request data."
	default:
		return ""
	}
}
