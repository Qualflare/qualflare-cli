package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"qualflare-cli/internal/version"

	"resty.dev/v3"
)

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
		SetAllowNonIdempotentRetry(true).
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
		endpoint: config.GetAPIEndpoint(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Close releases resources held by the client
func (c *Client) Close() {
	c.resty.Close()
}

// WithEndpoint overrides the API endpoint
func WithEndpoint(endpoint string) ClientOption {
	return func(c *Client) {
		c.endpoint = endpoint
	}
}

// SendReport sends a report to the API
func (c *Client) SendReport(ctx context.Context, report *domain.Launch) error {
	if c.config.IsVerbose() {
		jsonData, _ := json.Marshal(report)
		fmt.Printf("Upload request body size: %d bytes\n", len(jsonData))
	}

	resp, err := c.resty.R().
		SetContext(ctx).
		SetBody(report).
		Post(c.endpoint + "/api/v1/collect")
	if err != nil {
		return &APIError{Op: "send", Message: "failed to send request", Err: err}
	}

	if resp.IsSuccess() {
		return nil
	}

	return c.buildAPIError("send", resp)
}

// Get performs a GET request to the API
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

// API error codes
const (
	ErrCodeEnvironmentNotFound = "ENVIRONMENT_NOT_FOUND"
	ErrCodeMilestoneNotFound   = "MILESTONE_NOT_FOUND"
	ErrCodeValidationFailed    = "VALIDATION_FAILED"
	ErrCodeLanguageNotFound    = "LANGUAGE_NOT_FOUND"
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
