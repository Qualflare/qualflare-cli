package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

// stubConfig is a ports.ConfigProvider that keeps the client pointed at an httptest
// server. Retries are off by default so a test asserting on a 5xx doesn't wait for the
// real backoff schedule; retryMax is opened up only by the retry test itself.
type stubConfig struct {
	endpoint string
	apiKey   string
	debug    bool
	verbose  bool
	retryMax int
}

func (c *stubConfig) GetAPIKey() string             { return c.apiKey }
func (c *stubConfig) GetAPIEndpoint() string        { return c.endpoint }
func (c *stubConfig) GetEnvironment() string        { return "test" }
func (c *stubConfig) SetEnvironmentFallback(string) {}
func (c *stubConfig) GetLanguage() string           { return "en-US" }
func (c *stubConfig) GetPlatform() string           { return "linux" }
func (c *stubConfig) GetMilestone() int64           { return 0 }
func (c *stubConfig) GetMaxFileSize() int64         { return 1 << 20 }
func (c *stubConfig) GetCLIVersion() string         { return "test" }
func (c *stubConfig) GetBranch() string             { return "main" }
func (c *stubConfig) GetCommit() string             { return "abc123" }
func (c *stubConfig) GetRetryConfig() (int, time.Duration, time.Duration) {
	return c.retryMax, time.Millisecond, 2 * time.Millisecond
}
func (c *stubConfig) GetTimeout() time.Duration { return 5 * time.Second }
func (c *stubConfig) IsVerbose() bool           { return c.verbose }
func (c *stubConfig) IsQuiet() bool             { return false }
func (c *stubConfig) IsDryRun() bool            { return false }
func (c *stubConfig) IsDebug() bool             { return c.debug }
func (c *stubConfig) IsNoCaptureOutput() bool   { return false }
func (c *stubConfig) IsShard() bool             { return false }
func (c *stubConfig) Validate() error           { return nil }

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := NewHTTPClient(&stubConfig{endpoint: srv.URL, apiKey: "qf_secret"})
	t.Cleanup(c.Close)
	return c
}

// TestNewHTTPClient_TrimsEndpointSlash: the endpoint is concatenated with a leading-slash
// path, so a configured trailing slash would produce `//api/v1/collect`.
func TestNewHTTPClient_TrimsEndpointSlash(t *testing.T) {
	c := NewHTTPClient(&stubConfig{endpoint: "https://api.example.com/"})
	defer c.Close()
	if c.endpoint != "https://api.example.com" {
		t.Errorf("endpoint = %q, want the trailing slash trimmed", c.endpoint)
	}
}

func TestNewHTTPClient_AppliesOptions(t *testing.T) {
	called := false
	c := NewHTTPClient(&stubConfig{endpoint: "https://x"}, func(cl *Client) { called = true })
	defer c.Close()
	if !called {
		t.Error("ClientOption was not applied")
	}
}

// TestSendReport_Success also pins the two headers the upload contract depends on: the
// auth token (CLI-H2) and a stable Idempotency-Key, without which a retried 5xx could
// double-create a launch.
func TestSendReport_Success(t *testing.T) {
	var gotToken, gotKey, gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("QF_TOKEN")
		gotKey = r.Header.Get("Idempotency-Key")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	if err := c.SendReport(context.Background(), &domain.Launch{}); err != nil {
		t.Fatalf("SendReport() = %v, want nil", err)
	}
	if gotPath != apiBasePath+"/collect" {
		t.Errorf("path = %q, want %q", gotPath, apiBasePath+"/collect")
	}
	if gotToken != "qf_secret" {
		t.Errorf("QF_TOKEN = %q, want the configured key", gotToken)
	}
	if gotKey == "" {
		t.Error("Idempotency-Key header was not sent")
	}
}

func TestSendReport_APIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"auth.invalid_token","message":"token expired","request_id":"req-7"}`))
	})

	err := c.SendReport(context.Background(), &domain.Launch{})
	if err == nil {
		t.Fatal("SendReport() = nil, want an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Op != "send" || apiErr.Code != "auth.invalid_token" || apiErr.RequestID != "req-7" {
		t.Errorf("got op=%q code=%q requestID=%q", apiErr.Op, apiErr.Code, apiErr.RequestID)
	}
	// The server's own message wins over any hardcoded friendly string (SYNC-01/10).
	if apiErr.Message != "token expired" {
		t.Errorf("Message = %q, want the server's message", apiErr.Message)
	}
}

func TestSendReport_TransportError(t *testing.T) {
	c := NewHTTPClient(&stubConfig{endpoint: "http://127.0.0.1:1"})
	defer c.Close()

	err := c.SendReport(context.Background(), &domain.Launch{})
	if err == nil {
		t.Fatal("SendReport() = nil, want a transport error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	// StatusCode stays 0 for a transport failure, and the cause must be unwrappable.
	if apiErr.StatusCode != 0 || apiErr.Unwrap() == nil {
		t.Errorf("StatusCode = %d, Unwrap() = %v; want 0 and a non-nil cause", apiErr.StatusCode, apiErr.Unwrap())
	}
}

// TestSendReport_DoesNotFollowRedirects guards the token leak in NewHTTPClient's comment:
// the auth middleware re-runs on every hop, so following a cross-host redirect would
// forward QF_TOKEN to that host.
func TestSendReport_DoesNotFollowRedirects(t *testing.T) {
	var elsewhereHit bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer elsewhere.Close()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/steal", http.StatusTemporaryRedirect)
	})

	if err := c.SendReport(context.Background(), &domain.Launch{}); err == nil {
		t.Error("SendReport() = nil; a redirect must not be followed silently")
	}
	if elsewhereHit {
		t.Error("the redirect target was contacted — QF_TOKEN could leak to another host")
	}
}

// TestSendReport_RetriesTransientStatus covers CLI-H2: resty v3 refuses to retry POST
// unless non-idempotent retry is enabled, which would leave the retry policy dead for
// the CLI's primary job.
func TestSendReport_RetriesTransientStatus(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHTTPClient(&stubConfig{endpoint: srv.URL, retryMax: 2})
	defer c.Close()

	if err := c.SendReport(context.Background(), &domain.Launch{}); err != nil {
		t.Fatalf("SendReport() = %v, want the retry to succeed", err)
	}
	if attempts < 2 {
		t.Errorf("attempts = %d, want the 503 to be retried", attempts)
	}
}

func TestGet_SuccessWithParams(t *testing.T) {
	var gotQuery url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"items":[1,2]}`))
	})

	raw, err := c.Get(context.Background(), "/api/v1/suites", url.Values{"limit": {"10"}})
	if err != nil {
		t.Fatalf("Get() = %v, want nil", err)
	}
	var body struct {
		Items []int `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("response is not the raw JSON body: %v", err)
	}
	if len(body.Items) != 2 {
		t.Errorf("items = %v, want 2 entries", body.Items)
	}
	if gotQuery.Get("limit") != "10" {
		t.Errorf("query limit = %q, want %q", gotQuery.Get("limit"), "10")
	}
}

func TestGet_APIErrorWithoutJSONBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("<html>gateway exploded</html>"))
	})

	_, err := c.Get(context.Background(), "/api/v1/suites", nil)
	if err == nil {
		t.Fatal("Get() = nil, want an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *APIError", err)
	}
	if apiErr.Op != "get" {
		t.Errorf("Op = %q, want %q", apiErr.Op, "get")
	}
	// A non-JSON body must still produce a usable message rather than an empty one.
	if !strings.Contains(apiErr.Message, "500") {
		t.Errorf("Message = %q, want it to mention the status", apiErr.Message)
	}
}

func TestGet_ContextCancelled(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Get(ctx, "/api/v1/suites", nil); err == nil {
		t.Error("Get() with a cancelled context = nil, want an error")
	}
}

// TestNewIdempotencyKey checks the RFC 4122 v4 shape the server validates against, and
// that keys differ between invocations — a constant key would collapse separate uploads
// into one launch server-side.
func TestNewIdempotencyKey(t *testing.T) {
	uuidV4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	seen := make(map[string]bool, 100)
	for range 100 {
		key := newIdempotencyKey()
		if !uuidV4.MatchString(key) {
			t.Fatalf("newIdempotencyKey() = %q, want an RFC 4122 v4 UUID", key)
		}
		if seen[key] {
			t.Fatalf("newIdempotencyKey() repeated %q", key)
		}
		seen[key] = true
	}
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name  string
		err   *APIError
		wants []string
	}{
		{
			"status and request id",
			&APIError{Op: "send", Message: "nope", StatusCode: 401, RequestID: "req-1"},
			[]string{"send: nope", "status: 401", "qf login", "request_id: req-1"},
		},
		{
			"forbidden hint",
			&APIError{Op: "get", Message: "denied", StatusCode: 403},
			[]string{"get: denied", "lacks access"},
		},
		{
			"payment hint",
			&APIError{Op: "send", Message: "limit", StatusCode: 402},
			[]string{"plan limit"},
		},
		{
			"wrapped transport error",
			&APIError{Op: "send", Message: "failed to send request", Err: errors.New("dial tcp")},
			[]string{"send: failed to send request", "dial tcp"},
		},
		{
			"bare message",
			&APIError{Op: "send", Message: "something"},
			[]string{"send: something"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			for _, want := range tt.wants {
				if !strings.Contains(got, want) {
					t.Errorf("Error() = %q, missing %q", got, want)
				}
			}
		})
	}
}

func TestAPIError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	if got := (&APIError{Err: cause}).Unwrap(); !errors.Is(got, cause) {
		t.Errorf("Unwrap() = %v, want the wrapped cause", got)
	}
	if got := (&APIError{}).Unwrap(); got != nil {
		t.Errorf("Unwrap() with no cause = %v, want nil", got)
	}
}
