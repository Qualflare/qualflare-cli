package cli

import "testing"

// TestResolveLoginToken (SEC-01) covers the non-interactive precedence: an
// explicit argv token wins, else QF_TOKEN is used — so a token never has to be
// passed on the command line where it leaks to ps/history.
func TestResolveLoginToken(t *testing.T) {
	if tok, err := resolveLoginToken([]string{"id", "argtok"}); err != nil || tok != "argtok" {
		t.Fatalf("arg token: got %q err %v, want argtok", tok, err)
	}

	t.Setenv("QF_TOKEN", "envtok")
	if tok, err := resolveLoginToken([]string{"id"}); err != nil || tok != "envtok" {
		t.Fatalf("env token: got %q err %v, want envtok", tok, err)
	}
}
