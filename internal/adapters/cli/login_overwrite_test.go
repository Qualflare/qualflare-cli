package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"qualflare-cli/internal/auth"
)

// withPipedStdin replaces os.Stdin with a pipe carrying content, so the non-TTY
// branches of resolveLoginToken/confirmOverwrite can be exercised. Returns to the real
// stdin on cleanup.
func withPipedStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
	go func() {
		defer func() { _ = w.Close() }()
		_, _ = w.WriteString(content)
	}()
}

// SEC-01: a token piped on stdin must work, so CI never has to put it on the command
// line where it leaks to the process list and shell history.
func TestResolveLoginToken_FromPipedStdin(t *testing.T) {
	t.Setenv("QF_TOKEN", "") // ensure the env branch does not short-circuit
	withPipedStdin(t, "qf_piped_token\n")

	tok, err := resolveLoginToken([]string{"myapp"})
	if err != nil {
		t.Fatalf("resolveLoginToken = %v", err)
	}
	if tok != "qf_piped_token" {
		t.Errorf("token = %q, want %q (surrounding whitespace trimmed)", tok, "qf_piped_token")
	}
}

// An explicit argv token still wins over a piped one — precedence is argv, env, tty, pipe.
func TestResolveLoginToken_ArgBeatsStdin(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	withPipedStdin(t, "from_pipe\n")

	tok, err := resolveLoginToken([]string{"myapp", "from_arg"})
	if err != nil || tok != "from_arg" {
		t.Fatalf("token = %q, err = %v; want from_arg", tok, err)
	}
}

// confirmOverwrite fails closed when stdin is not a terminal: in CI there is nobody to
// answer the prompt, so the safe answer is "no" plus a pointer at --force. Under
// `go test` stdin is never a TTY, which is exactly the case being asserted.
func TestConfirmOverwrite_FailsClosedWithoutTTY(t *testing.T) {
	ok, err := confirmOverwrite("myapp")
	if ok {
		t.Error("confirmOverwrite = true without a terminal; it must fail closed")
	}
	if err == nil {
		t.Fatal("confirmOverwrite = nil error, want an explanation")
	}
	for _, want := range []string{"already exists", "not a terminal", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

// The same guard seen from the command layer: re-logging in to an existing identifier
// without --force must refuse rather than silently replacing the stored token.
func TestLogin_ExistingIdentifierWithoutForceRefuses(t *testing.T) {
	c, storePath := newCLIForLoginTest(t)
	c.store.Set("myapp", "qf_original")
	if err := c.store.Save(); err != nil {
		t.Fatal(err)
	}

	cmd := c.createLoginCommand()
	cmd.SetArgs([]string{"myapp", "qf_replacement"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("login over an existing identifier without --force = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error %q should point at --force", err)
	}

	// The stored token must be untouched.
	loaded, err := auth.Load(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if tok, _ := loaded.Get("myapp"); tok != "qf_original" {
		t.Errorf("stored token = %q, want the original to survive a refused overwrite", tok)
	}
}

// --force takes the same path but overwrites, and the new value must be persisted.
func TestLogin_ForceOverwritesExisting(t *testing.T) {
	c, storePath := newCLIForLoginTest(t)
	c.store.Set("myapp", "qf_original")
	if err := c.store.Save(); err != nil {
		t.Fatal(err)
	}

	cmd := c.createLoginCommand()
	cmd.SetArgs([]string{"myapp", "qf_replacement", "--force"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("login --force = %v", err)
	}

	loaded, err := auth.Load(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if tok, _ := loaded.Get("myapp"); tok != "qf_replacement" {
		t.Errorf("stored token = %q, want it overwritten", tok)
	}
}

// An invalid identifier is rejected before the token is even resolved, so a bad alias
// never causes a prompt or a stdin read.
func TestLogin_RejectsInvalidIdentifierBeforeTokenResolution(t *testing.T) {
	c, _ := newCLIForLoginTest(t)
	cmd := c.createLoginCommand()
	cmd.SetArgs([]string{"Not Valid"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err == nil {
		t.Fatal("login with an invalid identifier = nil, want a validation error")
	}
}
