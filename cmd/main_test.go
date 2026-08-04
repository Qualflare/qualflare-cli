package main

import (
	"errors"
	"fmt"
	"strings"
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

// TestRewriteUnknownCommandError covers the `qf <identifier>` slip: cobra's raw
// "unknown command" is rewritten into a login hint, but only when the token actually
// looks like an identifier. Anything else must pass through untouched, so a genuine
// typo still reports as a typo rather than as a missing login.
func TestRewriteUnknownCommandError(t *testing.T) {
	t.Run("rewrites a valid identifier", func(t *testing.T) {
		err := rewriteUnknownCommandError(errors.New(`unknown command "myapp" for "qf"`))
		got := err.Error()
		for _, want := range []string{`no identifier "myapp"`, "qf login myapp <token>", "qf projects"} {
			if !strings.Contains(got, want) {
				t.Errorf("message %q missing %q", got, want)
			}
		}
	})

	// Both cases below compare identity on purpose. The assertion is that the error is
	// returned *untouched*, which is stricter than errors.Is — that would also accept a
	// wrapped error, and wrapping here would still lose the original message the user
	// needs to see.
	t.Run("passes through non-identifier candidates", func(t *testing.T) {
		// Uppercase is not a legal identifier, so this is a real typo, not a login slip.
		in := errors.New(`unknown command "COLLECT" for "qf"`)
		if got := rewriteUnknownCommandError(in); got != in { //nolint:errorlint // identity is the assertion
			t.Errorf("expected the original error, got %v", got)
		}
	})

	t.Run("passes through unrelated errors", func(t *testing.T) {
		in := errors.New("some other failure")
		if got := rewriteUnknownCommandError(in); got != in { //nolint:errorlint // identity is the assertion
			t.Errorf("expected the original error, got %v", got)
		}
	})

	t.Run("nil stays nil", func(t *testing.T) {
		if got := rewriteUnknownCommandError(nil); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}
