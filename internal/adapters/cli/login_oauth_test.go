package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/oauth"
)

var testMetadata = oauth.Metadata{
	Issuer:                "https://api.example.test",
	AuthorizationEndpoint: "https://app.example.test/oauth/authorize",
	TokenEndpoint:         "https://api.example.test/oauth/token",
	RevocationEndpoint:    "https://api.example.test/oauth/revoke",
}

// loginHooks records what the OAuth seams saw. Every seam fails the test unless
// the test installs a behaviour for it.
type loginHooks struct {
	discoverCalls int
	loginCalls    int
	endpoint      string
	opts          oauth.LoginOptions
}

// stubLoginHooks swaps the terminal check, the hidden prompt and the OAuth
// calls for the duration of the test.
func stubLoginHooks(t *testing.T, tty bool) *loginHooks {
	t.Helper()
	h := &loginHooks{}
	origTTY, origHidden, origDiscover, origLogin := stdinIsTerminal, readHiddenToken, discoverOAuth, runOAuthLogin
	origSecret, origSave := readSecret, saveTerminal
	t.Cleanup(func() {
		stdinIsTerminal, readHiddenToken, discoverOAuth, runOAuthLogin = origTTY, origHidden, origDiscover, origLogin
		readSecret, saveTerminal = origSecret, origSave
	})
	readSecret = func(io.Writer, string) (string, error) {
		t.Error("hidden code prompt used unexpectedly")
		return "", errors.New("unexpected prompt")
	}
	saveTerminal = func() func() { return func() {} }
	stdinIsTerminal = func() bool { return tty }
	readHiddenToken = func() (string, error) {
		t.Error("hidden token prompt used unexpectedly")
		return "", errors.New("unexpected prompt")
	}
	discoverOAuth = func(_ context.Context, endpoint string) (oauth.Metadata, error) {
		h.discoverCalls++
		h.endpoint = endpoint
		return testMetadata, nil
	}
	runOAuthLogin = func(_ context.Context, _ oauth.Metadata, _ oauth.LoginOptions) (oauth.Credential, error) {
		t.Error("OAuth sign-in started unexpectedly")
		return oauth.Credential{}, errors.New("unexpected sign-in")
	}
	return h
}

func (h *loginHooks) succeed(expires time.Time) {
	runOAuthLogin = func(_ context.Context, md oauth.Metadata, opts oauth.LoginOptions) (oauth.Credential, error) {
		h.loginCalls++
		h.opts = opts
		if md != testMetadata {
			return oauth.Credential{}, fmt.Errorf("Login got metadata %+v", md)
		}
		return oauth.Credential{AccessToken: "at-secret", RefreshToken: "rt-secret", ExpiresAt: expires}, nil
	}
}

func runLoginCmd(t *testing.T, c *CLI, args ...string) (string, error) {
	t.Helper()
	cmd := c.createLoginCommand()
	cmd.SetArgs(args)
	var stderr bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	return stderr.String(), err
}

func reloadEntry(t *testing.T, storePath string) (auth.Identifier, bool) {
	t.Helper()
	s, err := auth.Load(storePath)
	if err != nil {
		t.Fatal(err)
	}
	return s.Entry("myapp")
}

func TestLogin_OAuthByDefaultOnTTY(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, storePath := newCLIForLoginTest(t)
	c.config.APIEndpoint = "https://api.example.test"
	h := stubLoginHooks(t, true)
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	h.succeed(expires)

	stderr, err := runLoginCmd(t, c, "myapp")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if h.endpoint != "https://api.example.test" {
		t.Errorf("discovery endpoint = %q, want the configured API endpoint", h.endpoint)
	}
	if h.opts.NoBrowser || h.opts.OpenBrowser == nil || h.opts.Prompt == nil || h.opts.Out == nil {
		t.Errorf("LoginOptions = %+v, want browser mode with an opener, a prompt and an output", h.opts)
	}
	if h.opts.Timeout != oauth.DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", h.opts.Timeout, oauth.DefaultTimeout)
	}

	got, ok := reloadEntry(t, storePath)
	if !ok {
		t.Fatal("no entry saved")
	}
	want := auth.Identifier{
		Kind: auth.KindOAuth, AccessToken: "at-secret", RefreshToken: "rt-secret",
		ExpiresAt: expires, Issuer: testMetadata.Issuer,
	}
	if got.Kind != want.Kind || got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken ||
		got.Issuer != want.Issuer || !got.ExpiresAt.Equal(want.ExpiresAt) || got.Token != "" {
		t.Fatalf("saved entry = %+v, want %+v", got, want)
	}
	const success = `Signed in. Saved identifier "myapp". Use it as the first arg, e.g. qf myapp cases.`
	if !strings.Contains(stderr, success) {
		t.Errorf("stderr = %q, want the line %q", stderr, success)
	}
	if strings.Contains(stderr, "at-secret") || strings.Contains(stderr, "rt-secret") {
		t.Errorf("stderr leaks a token: %q", stderr)
	}
}

func TestLogin_NoBrowserFlag(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, _ := newCLIForLoginTest(t)
	h := stubLoginHooks(t, true)
	h.succeed(time.Now().Add(time.Hour))

	if _, err := runLoginCmd(t, c, "myapp", "--no-browser"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if !h.opts.NoBrowser || h.opts.Prompt == nil {
		t.Errorf("LoginOptions = %+v, want NoBrowser with a prompt", h.opts)
	}
}

func TestLogin_TokenSourcesSkipOAuth(t *testing.T) {
	t.Run("argument", func(t *testing.T) {
		t.Setenv("QF_TOKEN", "")
		c, storePath := newCLIForLoginTest(t)
		stubLoginHooks(t, true)
		if _, err := runLoginCmd(t, c, "myapp", "qf_arg"); err != nil {
			t.Fatalf("login: %v", err)
		}
		assertPasted(t, storePath, "qf_arg")
	})
	t.Run("QF_TOKEN", func(t *testing.T) {
		t.Setenv("QF_TOKEN", "qf_env")
		c, storePath := newCLIForLoginTest(t)
		stubLoginHooks(t, true)
		if _, err := runLoginCmd(t, c, "myapp"); err != nil {
			t.Fatalf("login: %v", err)
		}
		assertPasted(t, storePath, "qf_env")
	})
	t.Run("piped stdin", func(t *testing.T) {
		t.Setenv("QF_TOKEN", "")
		c, storePath := newCLIForLoginTest(t)
		stubLoginHooks(t, false)
		withPipedStdin(t, "qf_piped\n")
		if _, err := runLoginCmd(t, c, "myapp"); err != nil {
			t.Fatalf("login: %v", err)
		}
		assertPasted(t, storePath, "qf_piped")
	})
	t.Run("--with-token", func(t *testing.T) {
		t.Setenv("QF_TOKEN", "")
		c, storePath := newCLIForLoginTest(t)
		stubLoginHooks(t, true)
		readHiddenToken = func() (string, error) { return "qf_hidden", nil }
		if _, err := runLoginCmd(t, c, "myapp", "--with-token"); err != nil {
			t.Fatalf("login: %v", err)
		}
		assertPasted(t, storePath, "qf_hidden")
	})
}

func assertPasted(t *testing.T, storePath, want string) {
	t.Helper()
	got, ok := reloadEntry(t, storePath)
	if !ok || got.Token != want || got.Kind != "" {
		t.Fatalf("saved entry = %+v (ok=%v), want a pasted token %q", got, ok, want)
	}
}

func TestLogin_NoAuthServerFallsBackToPrompt(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, storePath := newCLIForLoginTest(t)
	stubLoginHooks(t, true)
	discoverOAuth = func(context.Context, string) (oauth.Metadata, error) {
		return oauth.Metadata{}, fmt.Errorf("wrapped: %w", oauth.ErrNoAuthServer)
	}
	readHiddenToken = func() (string, error) { return "qf_fallback", nil }

	stderr, err := runLoginCmd(t, c, "myapp")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.Contains(stderr, "This server has no browser sign-in; paste a token instead.") {
		t.Errorf("stderr = %q, want the fallback line", stderr)
	}
	assertPasted(t, storePath, "qf_fallback")
}

func TestLogin_DiscoveryErrorStops(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, storePath := newCLIForLoginTest(t)
	stubLoginHooks(t, true)
	discoverOAuth = func(context.Context, string) (oauth.Metadata, error) {
		return oauth.Metadata{}, errors.New("issuer mismatch")
	}
	if _, err := runLoginCmd(t, c, "myapp"); err == nil || !strings.Contains(err.Error(), "issuer mismatch") {
		t.Fatalf("login = %v, want the discovery error", err)
	}
	if _, ok := reloadEntry(t, storePath); ok {
		t.Fatal("an entry was saved after a failed discovery")
	}
}

func TestLogin_OAuthFailureSavesNothing(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, storePath := newCLIForLoginTest(t)
	stubLoginHooks(t, true)
	runOAuthLogin = func(context.Context, oauth.Metadata, oauth.LoginOptions) (oauth.Credential, error) {
		return oauth.Credential{}, oauth.ErrCancelled
	}
	if _, err := runLoginCmd(t, c, "myapp"); !errors.Is(err, oauth.ErrCancelled) {
		t.Fatalf("login = %v, want ErrCancelled", err)
	}
	if _, ok := reloadEntry(t, storePath); ok {
		t.Fatal("an entry was saved after a cancelled sign-in")
	}
}

func TestLogin_OverwritePromptRunsBeforeOAuth(t *testing.T) {
	seed := func(t *testing.T) (*CLI, string) {
		c, storePath := newCLIForLoginTest(t)
		c.store.Set("myapp", "qf_original")
		if err := c.store.Save(); err != nil {
			t.Fatal(err)
		}
		return c, storePath
	}
	t.Run("declined", func(t *testing.T) {
		t.Setenv("QF_TOKEN", "")
		c, storePath := seed(t)
		h := stubLoginHooks(t, true)
		withPipedStdin(t, "n\n")
		if _, err := runLoginCmd(t, c, "myapp"); err != nil {
			t.Fatalf("login: %v", err)
		}
		if h.discoverCalls != 0 {
			t.Error("discovery ran before the overwrite was confirmed")
		}
		assertPasted(t, storePath, "qf_original")
	})
	t.Run("accepted", func(t *testing.T) {
		t.Setenv("QF_TOKEN", "")
		c, storePath := seed(t)
		h := stubLoginHooks(t, true)
		h.succeed(time.Now().Add(time.Hour))
		withPipedStdin(t, "y\n")
		if _, err := runLoginCmd(t, c, "myapp"); err != nil {
			t.Fatalf("login: %v", err)
		}
		got, _ := reloadEntry(t, storePath)
		if got.Kind != auth.KindOAuth || got.AccessToken != "at-secret" || got.Token != "" {
			t.Fatalf("entry = %+v, want it replaced by the OAuth sign-in", got)
		}
	})
}

// --no-browser reads the code at a hidden prompt, like a pasted token.
func TestLogin_NoBrowserCodeIsHidden(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, _ := newCLIForLoginTest(t)
	stubLoginHooks(t, true)
	var gotLabel string
	readSecret = func(_ io.Writer, label string) (string, error) {
		gotLabel = label
		return "code-x", nil
	}
	runOAuthLogin = func(_ context.Context, _ oauth.Metadata, opts oauth.LoginOptions) (oauth.Credential, error) {
		code, err := opts.Prompt("Paste the code shown after signing in: ")
		if err != nil || code != "code-x" {
			return oauth.Credential{}, fmt.Errorf("Prompt = %q: %w", code, err)
		}
		return oauth.Credential{AccessToken: "at", RefreshToken: "rt"}, nil
	}
	if _, err := runLoginCmd(t, c, "myapp", "--no-browser"); err != nil {
		t.Fatalf("login: %v", err)
	}
	if gotLabel != "Paste the code shown after signing in: " {
		t.Errorf("hidden prompt label = %q", gotLabel)
	}
}

// Ctrl-C while the hidden code prompt is open abandons the read, so the
// terminal state saved before the sign-in is restored on the way out, and the
// error just says the sign-in was cancelled.
func TestLogin_InterruptRestoresTerminal(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, storePath := newCLIForLoginTest(t)
	stubLoginHooks(t, true)
	restored := false
	saveTerminal = func() func() { return func() { restored = true } }
	runOAuthLogin = func(context.Context, oauth.Metadata, oauth.LoginOptions) (oauth.Credential, error) {
		if restored {
			t.Error("terminal restored before the sign-in ended")
		}
		return oauth.Credential{}, fmt.Errorf("sign-in interrupted: %w", context.Canceled)
	}
	_, err := runLoginCmd(t, c, "myapp", "--no-browser")
	if err == nil || err.Error() != "sign-in cancelled" {
		t.Fatalf("login = %v, want exactly \"sign-in cancelled\"", err)
	}
	if !restored {
		t.Error("terminal state not restored after an interrupted sign-in")
	}
	if _, ok := reloadEntry(t, storePath); ok {
		t.Error("an entry was saved after an interrupted sign-in")
	}
}

// Ctrl-C during discovery is a cancellation, not a reason to suggest --with-token.
func TestLogin_InterruptDuringDiscovery(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, _ := newCLIForLoginTest(t)
	stubLoginHooks(t, true)
	discoverOAuth = func(context.Context, string) (oauth.Metadata, error) {
		return oauth.Metadata{}, fmt.Errorf("fetch sign-in metadata: %w", context.Canceled)
	}
	_, err := runLoginCmd(t, c, "myapp")
	if err == nil || err.Error() != "sign-in cancelled" {
		t.Fatalf("login = %v, want exactly \"sign-in cancelled\"", err)
	}
}

// The timeout message names the alias the user typed.
func TestLogin_TimeoutNamesAlias(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, _ := newCLIForLoginTest(t)
	stubLoginHooks(t, true)
	runOAuthLogin = func(ctx context.Context, md oauth.Metadata, _ oauth.LoginOptions) (oauth.Credential, error) {
		return oauth.Login(ctx, md, oauth.LoginOptions{
			OpenBrowser: func(string) error { return nil },
			Out:         io.Discard,
			Timeout:     20 * time.Millisecond,
		})
	}
	_, err := runLoginCmd(t, c, "myapp")
	if !errors.Is(err, oauth.ErrTimeout) {
		t.Fatalf("login = %v, want ErrTimeout", err)
	}
	if !strings.Contains(err.Error(), "Run qf login myapp again.") || strings.Contains(err.Error(), "<alias>") {
		t.Errorf("timeout message = %q, want it to name myapp", err)
	}
}

// --no-browser is a browser sign-in. It is refused, with nothing saved, where a
// token source would win or there is no terminal to paste the code into,
// instead of being silently ignored.
func TestLogin_NoBrowserRefusedWhereItCannotApply(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		tty     bool
		piped   string
		args    []string
		wantErr string
	}{
		{name: "with --with-token", tty: true, args: []string{"myapp", "--no-browser", "--with-token"},
			wantErr: "[no-browser with-token] were all set"},
		{name: "token argument", tty: true, args: []string{"myapp", "qf_arg", "--no-browser"},
			wantErr: "--no-browser signs in through the browser and cannot be used with a token argument"},
		{name: "QF_TOKEN", env: "qf_env", tty: true, args: []string{"myapp", "--no-browser"},
			wantErr: "--no-browser signs in through the browser, but QF_TOKEN is set: unset it, or drop --no-browser"},
		{name: "stdin not a terminal", piped: "qf_piped\n", args: []string{"myapp", "--no-browser"},
			wantErr: "--no-browser needs a terminal to paste the sign-in code into, and stdin is not one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("QF_TOKEN", tc.env)
			c, storePath := newCLIForLoginTest(t)
			h := stubLoginHooks(t, tc.tty)
			if tc.piped != "" {
				withPipedStdin(t, tc.piped)
			}
			_, err := runLoginCmd(t, c, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("login = %v, want an error containing %q", err, tc.wantErr)
			}
			if h.discoverCalls != 0 {
				t.Error("discovery ran")
			}
			if _, ok := reloadEntry(t, storePath); ok {
				t.Error("an entry was saved")
			}
		})
	}
}

// Replacing an OAuth alias revokes the old sign-in's grant at its issuer, best
// effort, once the new credentials are saved, as logout does.
func TestLogin_ReplacingAnOAuthAliasRevokesTheOldGrant(t *testing.T) {
	type revoke struct {
		issuer, token string
		savedAccess   string // what the store held when the revoke was sent
	}
	setup := func(t *testing.T, revokeErr error) (*CLI, string, *loginHooks, *[]revoke) {
		t.Helper()
		t.Setenv("QF_TOKEN", "")
		c, storePath := newCLIForLoginTest(t)
		err := c.saveLocked(func(s *auth.Store) {
			s.SetOAuth("myapp", auth.Identifier{AccessToken: "at-old", RefreshToken: "rt-old",
				ExpiresAt: time.Now().Add(time.Hour), Issuer: "https://old.example.test"})
		})
		if err != nil {
			t.Fatal(err)
		}
		h := stubLoginHooks(t, true)
		discoverOAuth = func(_ context.Context, endpoint string) (oauth.Metadata, error) {
			h.discoverCalls++
			if endpoint == "https://old.example.test" {
				return oauth.Metadata{Issuer: endpoint, RevocationEndpoint: endpoint + "/oauth/revoke"}, nil
			}
			return testMetadata, nil
		}
		var got []revoke
		origRevoke := revokeOAuth
		t.Cleanup(func() { revokeOAuth = origRevoke })
		revokeOAuth = func(_ context.Context, md oauth.Metadata, rt string) error {
			e, _ := reloadEntry(t, storePath)
			got = append(got, revoke{issuer: md.Issuer, token: rt, savedAccess: e.AccessToken + e.Token})
			return revokeErr
		}
		return c, storePath, h, &got
	}

	t.Run("browser sign-in", func(t *testing.T) {
		c, storePath, h, got := setup(t, nil)
		h.succeed(time.Now().Add(time.Hour))
		if _, err := runLoginCmd(t, c, "myapp", "--force"); err != nil {
			t.Fatalf("login: %v", err)
		}
		want := []revoke{{issuer: "https://old.example.test", token: "rt-old", savedAccess: "at-secret"}}
		if !slices.Equal(*got, want) {
			t.Errorf("revocations = %+v, want %+v (after the new sign-in was saved)", *got, want)
		}
		if e, _ := reloadEntry(t, storePath); e.AccessToken != "at-secret" {
			t.Errorf("entry = %+v, want the new sign-in", e)
		}
	})
	t.Run("pasted token", func(t *testing.T) {
		c, storePath, _, got := setup(t, nil)
		if _, err := runLoginCmd(t, c, "myapp", "qf_new", "--force"); err != nil {
			t.Fatalf("login: %v", err)
		}
		want := []revoke{{issuer: "https://old.example.test", token: "rt-old", savedAccess: "qf_new"}}
		if !slices.Equal(*got, want) {
			t.Errorf("revocations = %+v, want %+v", *got, want)
		}
		assertPasted(t, storePath, "qf_new")
	})
	t.Run("server without browser sign-in", func(t *testing.T) {
		c, storePath, _, got := setup(t, nil)
		discoverOAuth = func(ctx context.Context, endpoint string) (oauth.Metadata, error) {
			if err := ctx.Err(); err != nil {
				return oauth.Metadata{}, err
			}
			if endpoint == "https://old.example.test" {
				return oauth.Metadata{Issuer: endpoint, RevocationEndpoint: endpoint + "/oauth/revoke"}, nil
			}
			return oauth.Metadata{}, oauth.ErrNoAuthServer
		}
		readHiddenToken = func() (string, error) { return "qf_fallback", nil }
		if _, err := runLoginCmd(t, c, "myapp", "--force"); err != nil {
			t.Fatalf("login: %v", err)
		}
		want := []revoke{{issuer: "https://old.example.test", token: "rt-old", savedAccess: "qf_fallback"}}
		if !slices.Equal(*got, want) {
			t.Errorf("revocations = %+v, want %+v", *got, want)
		}
		assertPasted(t, storePath, "qf_fallback")
	})
	t.Run("revoke fails", func(t *testing.T) {
		c, storePath, h, _ := setup(t, errors.New("unexpected status 503"))
		h.succeed(time.Now().Add(time.Hour))
		stderr, err := runLoginCmd(t, c, "myapp", "--force")
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		if !strings.Contains(stderr, `warning: could not revoke the previous sign-in for "myapp"`) ||
			strings.Contains(stderr, "rt-old") {
			t.Errorf("stderr = %q, want a warning that names no token", stderr)
		}
		if e, _ := reloadEntry(t, storePath); e.AccessToken != "at-secret" {
			t.Errorf("entry = %+v, want the new sign-in kept", e)
		}
	})
}

// Replacing a pasted token revokes nothing.
func TestLogin_ReplacingAPastedAliasRevokesNothing(t *testing.T) {
	t.Setenv("QF_TOKEN", "")
	c, _ := newCLIForLoginTest(t)
	if err := c.saveLocked(func(s *auth.Store) { s.Set("myapp", "qf_old") }); err != nil {
		t.Fatal(err)
	}
	h := stubLoginHooks(t, true)
	h.succeed(time.Now().Add(time.Hour))
	origRevoke := revokeOAuth
	t.Cleanup(func() { revokeOAuth = origRevoke })
	revokeOAuth = func(context.Context, oauth.Metadata, string) error {
		t.Error("revoke called for a pasted token")
		return nil
	}
	if _, err := runLoginCmd(t, c, "myapp", "--force"); err != nil {
		t.Fatalf("login: %v", err)
	}
}
