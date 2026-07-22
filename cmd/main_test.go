package main

import (
	"errors"
	"fmt"
	"testing"

	qfhttp "qualflare-cli/internal/adapters/http"
)

// TestExitCodeForError (API-04) pins the CI-facing exit-code contract: transient
// failures (429/5xx) are distinct from auth (401), quota/forbidden (402/403), and
// not-found (404) so a pipeline can retry only what is retryable.
func TestExitCodeForError(t *testing.T) {
	cases := []struct {
		status int
		want   int
	}{
		{401, exitAuth},
		{402, exitForbidden},
		{403, exitForbidden},
		{404, exitNotFound},
		{429, exitTransient},
		{500, exitTransient},
		{503, exitTransient},
		{400, exitGeneric},
	}
	for _, tc := range cases {
		err := fmt.Errorf("send: %w", &qfhttp.APIError{StatusCode: tc.status, Message: "x"})
		if got := exitCodeForError(err); got != tc.want {
			t.Errorf("status %d: exit code = %d, want %d", tc.status, got, tc.want)
		}
	}

	// A non-API error is generic.
	if got := exitCodeForError(errors.New("boom")); got != exitGeneric {
		t.Errorf("non-API error: exit code = %d, want %d", got, exitGeneric)
	}
}
