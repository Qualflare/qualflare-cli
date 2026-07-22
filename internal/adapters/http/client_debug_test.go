package http

import (
	"net/http"
	"testing"

	"resty.dev/v3"
)

// TestRedactDebugLog (OBS-06) proves the --debug log entry has the API token
// scrubbed before it is ever formatted/printed. Headers are built via Set so the
// keys are canonicalized exactly as resty stores them.
func TestRedactDebugLog(t *testing.T) {
	h := http.Header{}
	h.Set("QF_TOKEN", "qf_supersecret")
	h.Set("Authorization", "Bearer qf_supersecret")
	h.Set("Accept", "application/json")
	dl := &resty.DebugLog{Request: &resty.DebugLogRequest{Header: h}}

	redactDebugLog(dl)

	if got := dl.Request.Header.Get("QF_TOKEN"); got != "***REDACTED***" {
		t.Fatalf("QF_TOKEN not redacted: %q", got)
	}
	if dl.Request.Header.Get("Authorization") != "" {
		t.Fatal("Authorization header must be removed from debug output")
	}
	if dl.Request.Header.Get("Accept") != "application/json" {
		t.Fatal("non-sensitive headers must be preserved")
	}

	// nil-safe
	redactDebugLog(nil)
	redactDebugLog(&resty.DebugLog{})
}
