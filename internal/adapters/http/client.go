package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"qualflare-cli/internal/version"
	"strconv"
	"time"
)

// Client handles HTTP communication with the API
type Client struct {
	client   *http.Client
	config   ports.ConfigProvider
	endpoint string

	// Retry configuration
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

// ClientOption is a function that configures the client
type ClientOption func(*Client)

// NewHTTPClient creates a new HTTP client
func NewHTTPClient(config ports.ConfigProvider, opts ...ClientOption) *Client {
	maxRetries, baseDelay, maxDelay := config.GetRetryConfig()

	c := &Client{
		client: &http.Client{
			Timeout: config.GetTimeout(),
		},
		config:     config,
		endpoint:   config.GetAPIEndpoint(),
		maxRetries: maxRetries,
		baseDelay:  baseDelay,
		maxDelay:   maxDelay,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.client = client
	}
}

// WithEndpoint overrides the API endpoint
func WithEndpoint(endpoint string) ClientOption {
	return func(c *Client) {
		c.endpoint = endpoint
	}
}

// SendReport sends a report to the API with retry logic
func (c *Client) SendReport(ctx context.Context, report *domain.Launch) error {
	jsonData, err := json.Marshal(report)
	if err != nil {
		return &APIError{
			Op:      "marshal",
			Message: "failed to marshal report",
			Err:     err,
		}
	}

	if c.config.IsVerbose() {
		fmt.Printf("Upload request body size: %d bytes\n", len(jsonData))
	}

	_, err = c.doWithRetry(ctx, "send", func() ([]byte, error) {
		return c.doRequestWithMethod(ctx, http.MethodPost, c.endpoint+"/api/v1/collect", jsonData)
	})
	return err
}

// doWithRetry executes an action with retry logic, exponential backoff, and Retry-After support
func (c *Client) doWithRetry(ctx context.Context, op string, action func() ([]byte, error)) ([]byte, error) {
	var lastErr error
	var retryAfterDelay time.Duration

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			delay := c.calculateBackoff(attempt)
			// Retry-After takes precedence over computed backoff
			if retryAfterDelay > 0 {
				delay = retryAfterDelay
				retryAfterDelay = 0
			}
			select {
			case <-ctx.Done():
				return nil, &APIError{Op: op, Message: "request cancelled", Err: ctx.Err()}
			case <-time.After(delay):
			}
		}

		respBody, err := action()
		if err == nil {
			return respBody, nil
		}

		lastErr = err

		var apiErr *APIError
		if errors.As(err, &apiErr) {
			if !apiErr.Retryable {
				return nil, err
			}
			retryAfterDelay = apiErr.RetryAfter
		}
	}

	return nil, &APIError{
		Op:      op,
		Message: fmt.Sprintf("failed after %d attempts", c.maxRetries+1),
		Err:     lastErr,
	}
}

// doRequestWithMethod performs a single HTTP request with the given method and URL
func (c *Client) doRequestWithMethod(ctx context.Context, method, reqURL string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, &APIError{
			Op:        "create_request",
			Message:   "failed to create request",
			Err:       err,
			Retryable: false,
		}
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Accept", "application/json")

	if apiKey := c.config.GetAPIKey(); apiKey != "" {
		req.Header.Set("QF_TOKEN", apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, &APIError{
			Op:        "send",
			Message:   "failed to send request",
			Err:       err,
			Retryable: true,
		}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBody, nil
	}

	apiErr := &APIError{
		Op:         "send",
		StatusCode: resp.StatusCode,
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err == nil {
		apiErr.Code = errResp.Code
		if friendlyMsg := getUserFriendlyMessage(errResp.Code); friendlyMsg != "" {
			apiErr.Message = friendlyMsg
		} else if errResp.Error != "" {
			apiErr.Message = errResp.Error
		} else if errResp.Message != "" {
			apiErr.Message = errResp.Message
		} else {
			apiErr.Message = fmt.Sprintf("API request failed with status %d", resp.StatusCode)
		}
	} else {
		apiErr.Message = fmt.Sprintf("API request failed with status %d", resp.StatusCode)
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		apiErr.Retryable = true
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				apiErr.RetryAfter = time.Duration(seconds) * time.Second
			} else if t, err := http.ParseTime(retryAfter); err == nil {
				apiErr.RetryAfter = time.Until(t)
			}
		}
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		apiErr.Retryable = true
	case http.StatusInternalServerError:
		apiErr.Retryable = true
	default:
		apiErr.Retryable = false
	}

	return nil, apiErr
}

// Get performs a GET request to the API with retry logic
func (c *Client) Get(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	reqURL := c.endpoint + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	if c.config.IsVerbose() {
		fmt.Printf("GET %s\n", reqURL)
	}

	respBody, err := c.doWithRetry(ctx, "get", func() ([]byte, error) {
		return c.doRequestWithMethod(ctx, http.MethodGet, reqURL, nil)
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(respBody), nil
}

// calculateBackoff calculates the delay for a retry attempt with jitter
func (c *Client) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: baseDelay * 2^attempt
	delay := float64(c.baseDelay) * math.Pow(2, float64(attempt-1))

	// Add jitter (0-25% of delay)
	jitter := delay * 0.25 * rand.Float64()
	delay += jitter

	// Cap at maxDelay
	if delay > float64(c.maxDelay) {
		delay = float64(c.maxDelay)
	}

	return time.Duration(delay)
}

// APIError represents an API error
type APIError struct {
	Op         string
	Message    string
	Code       string
	StatusCode int
	Err        error
	Retryable  bool
	RetryAfter time.Duration
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

// IsRetryable returns whether the error is retryable
func (e *APIError) IsRetryable() bool {
	return e.Retryable
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
