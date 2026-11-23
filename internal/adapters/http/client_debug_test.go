package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"

	"resty.dev/v3"
)

// TestRedactDebugLog (OBS-06) proves the --debug log entry has the API token
// scrubbed before it is ever formatted/printed. Headers are built via Set so the
// keys are canonicalized exactly as resty stores them.
func TestRedactDebugLog(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer qf_supersecret")
	h.Set("Accept", "application/json")
	dl := &resty.DebugLog{Request: &resty.DebugLogRequest{Header: h}}

	redactDebugLog(dl)

	if got := dl.Request.Header.Get("Authorization"); got != "Bearer ***REDACTED***" {
		t.Fatalf("Authorization not redacted: %q", got)
	}
	if dl.Request.Header.Get("Accept") != "application/json" {
		t.Fatal("non-sensitive headers must be preserved")
	}

	// nil-safe
	redactDebugLog(nil)
	redactDebugLog(&resty.DebugLog{})
}

// TestClient_DebugOutputOmitsTheToken runs a real request with --debug on and
// checks the formatted log never contains the token, while the server still
// receives it.
func TestClient_DebugOutputOmitsTheToken(t *testing.T) {
	const token = "qf_debugsecret"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := NewHTTPClient(&stubConfig{endpoint: srv.URL, apiKey: token, debug: true})
	t.Cleanup(c.Close)
	log := &captureLogger{}
	c.resty.SetLogger(log)

	if err := c.SendReport(context.Background(), &domain.Launch{}); err != nil {
		t.Fatalf("SendReport() = %v, want nil", err)
	}
	if gotAuth != "Bearer "+token {
		t.Errorf("Authorization = %q, want the bearer token", gotAuth)
	}
	out := log.buf.String()
	if !strings.Contains(out, "REDACTED") {
		t.Fatalf("debug output has no redacted header; got:\n%s", out)
	}
	if strings.Contains(out, token) {
		t.Errorf("debug output contains the token:\n%s", out)
	}
}
