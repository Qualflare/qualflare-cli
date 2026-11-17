package oauth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// loginWithBrowser runs Login in loopback mode; the browser stub hands the
// authorize URL to act, which plays the user and the browser.
func loginWithBrowser(t *testing.T, f *fakeAS, timeout time.Duration, act func(authURL string)) (Credential, string, error) {
	t.Helper()
	var out bytes.Buffer
	cred, err := Login(t.Context(), f.metadata(), LoginOptions{
		OpenBrowser: func(u string) error { act(u); return nil },
		Out:         &out,
		Timeout:     timeout,
	})
	return cred, out.String(), err
}

func TestLogin_LoopbackSuccess(t *testing.T) {
	f := newFakeAS(t)
	var body string
	before := time.Now()
	cred, out, err := loginWithBrowser(t, f, 10*time.Second, func(authURL string) {
		q := f.authorize(authURL, "code-1")
		if !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") || !strings.HasSuffix(q.Get("redirect_uri"), "/callback") {
			t.Errorf("redirect_uri = %q, want http://127.0.0.1:<port>/callback", q.Get("redirect_uri"))
		}
		var status int
		status, body = browserGet(t, callbackURL(q.Get("redirect_uri"), map[string]string{
			"code": "code-1", "state": q.Get("state"), "iss": f.srv.URL,
		}))
		if status != 200 {
			t.Errorf("callback status = %d, want 200", status)
		}
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if cred.AccessToken != "at-secret" || cred.RefreshToken != "rt-secret" {
		t.Fatalf("credential = %+v, want the issued pair", cred)
	}
	if d := cred.ExpiresAt.Sub(before); d < 3590*time.Second || d > 3610*time.Second {
		t.Errorf("ExpiresAt is %v from now, want about 3600s", d)
	}
	if !strings.Contains(body, "You can close this tab") {
		t.Errorf("tab body = %q, want it to say You can close this tab", body)
	}
	if !authURLPattern.MatchString(out) {
		t.Errorf("authorize URL not printed; out = %q", out)
	}
	if !strings.Contains(out, "--no-browser") {
		t.Errorf("out does not point SSH/remote users at --no-browser: %q", out)
	}
	if strings.Contains(out, "at-secret") || strings.Contains(out, "rt-secret") {
		t.Errorf("output leaks a token: %q", out)
	}
	if n := f.exchanges.Load(); n != 1 {
		t.Errorf("exchanges = %d, want 1", n)
	}
}

func TestLogin_StateMismatchRefused(t *testing.T) {
	f := newFakeAS(t)
	var body string
	_, _, err := loginWithBrowser(t, f, 10*time.Second, func(authURL string) {
		q := f.authorize(authURL, "code-1")
		_, body = browserGet(t, callbackURL(q.Get("redirect_uri"), map[string]string{
			"code": "code-1", "state": "not-the-state", "iss": f.srv.URL,
		}))
	})
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("Login = %v, want a state mismatch error", err)
	}
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("Login = %v, want an immediate refusal, not a timeout", err)
	}
	if body == "" || strings.Contains(body, "You can close this tab") {
		t.Errorf("tab body = %q, want a short failure text", body)
	}
	if n := f.exchanges.Load(); n != 0 {
		t.Errorf("exchanges = %d, want 0 (no exchange on a state mismatch)", n)
	}
}

func TestLogin_IssMismatchRefused(t *testing.T) {
	for name, iss := range map[string]string{"wrong": "https://evil.example", "missing": ""} {
		t.Run(name, func(t *testing.T) {
			f := newFakeAS(t)
			_, _, err := loginWithBrowser(t, f, 10*time.Second, func(authURL string) {
				q := f.authorize(authURL, "code-1")
				params := map[string]string{"code": "code-1", "state": q.Get("state")}
				if iss != "" {
					params["iss"] = iss
				}
				browserGet(t, callbackURL(q.Get("redirect_uri"), params))
			})
			if err == nil || !strings.Contains(err.Error(), "issuer") {
				t.Fatalf("Login = %v, want an issuer mismatch error", err)
			}
			if n := f.exchanges.Load(); n != 0 {
				t.Errorf("exchanges = %d, want 0", n)
			}
		})
	}
}

func TestLogin_Cancelled(t *testing.T) {
	f := newFakeAS(t)
	start := time.Now()
	_, _, err := loginWithBrowser(t, f, 30*time.Second, func(authURL string) {
		q := f.authorize(authURL, "code-1")
		browserGet(t, callbackURL(q.Get("redirect_uri"), map[string]string{
			"error": "access_denied", "state": q.Get("state"), "iss": f.srv.URL,
		}))
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Login = %v, want ErrCancelled", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("cancel took %v, want it to return immediately", time.Since(start))
	}
	if n := f.exchanges.Load(); n != 0 {
		t.Errorf("exchanges = %d, want 0", n)
	}
}

// A server error is printed quoted, so its text cannot forge terminal lines.
func TestLogin_ServerErrorQuoted(t *testing.T) {
	f := newFakeAS(t)
	_, _, err := loginWithBrowser(t, f, 10*time.Second, func(authURL string) {
		q := f.authorize(authURL, "code-1")
		browserGet(t, callbackURL(q.Get("redirect_uri"), map[string]string{
			"error": "server_error", "error_description": "bad\nOK Signed in", "state": q.Get("state"), "iss": f.srv.URL,
		}))
	})
	if err == nil {
		t.Fatal("Login = nil error for an error callback")
	}
	if strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), `"bad\nOK Signed in"`) || !strings.Contains(err.Error(), `"server_error"`) {
		t.Errorf("error = %q, want the code and description quoted", err)
	}
}

func TestLogin_Timeout(t *testing.T) {
	f := newFakeAS(t)
	_, _, err := loginWithBrowser(t, f, 150*time.Millisecond, func(string) {})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Login = %v, want ErrTimeout", err)
	}
	// The default wait is five minutes; this is the message the user sees then.
	const want = "Sign-in timed out after 5 minutes. Run qf login <alias> again."
	if got := (timeoutError{after: DefaultTimeout}).Error(); got != want {
		t.Errorf("timeout message = %q, want %q", got, want)
	}
	if DefaultTimeout != 5*time.Minute {
		t.Errorf("DefaultTimeout = %v, want 5m", DefaultTimeout)
	}
}

func TestLogin_ContextCancelled(t *testing.T) {
	f := newFakeAS(t)
	ctx, cancel := context.WithCancel(t.Context())
	_, err := Login(ctx, f.metadata(), LoginOptions{
		OpenBrowser: func(string) error { cancel(); return nil },
		Out:         &bytes.Buffer{},
		Timeout:     30 * time.Second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Login = %v, want context.Canceled", err)
	}
}

func TestLogin_BrowserOpenFails(t *testing.T) {
	f := newFakeAS(t)
	urls := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The user copies the printed URL into a browser themselves.
		authURL := <-urls
		q := f.authorize(authURL, "code-2")
		browserGet(t, callbackURL(q.Get("redirect_uri"), map[string]string{
			"code": "code-2", "state": q.Get("state"), "iss": f.srv.URL,
		}))
	}()
	var out bytes.Buffer
	cred, err := Login(t.Context(), f.metadata(), LoginOptions{
		OpenBrowser: func(u string) error { urls <- u; return errors.New("no browser here") },
		Out:         &out,
		Timeout:     10 * time.Second,
	})
	<-done
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if cred.AccessToken != "at-secret" {
		t.Fatalf("AccessToken = %q", cred.AccessToken)
	}
	if !authURLPattern.MatchString(out.String()) {
		t.Errorf("authorize URL not printed when the browser failed; out = %q", out.String())
	}
	var failed string
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(l, "Could not open a browser") {
			failed = l
		}
	}
	if !strings.Contains(failed, "--no-browser") {
		t.Errorf("failed-open line %q should mention --no-browser", failed)
	}
}

func TestLogin_OtherPathsDoNotEndWait(t *testing.T) {
	f := newFakeAS(t)
	cred, _, err := loginWithBrowser(t, f, 10*time.Second, func(authURL string) {
		q := f.authorize(authURL, "code-3")
		base := strings.TrimSuffix(q.Get("redirect_uri"), "/callback")
		for _, p := range []string{"/", "/favicon.ico", "/callbackx"} {
			if status, _ := browserGet(t, base+p); status != 404 {
				t.Errorf("GET %s = %d, want 404", p, status)
			}
		}
		browserGet(t, callbackURL(q.Get("redirect_uri"), map[string]string{
			"code": "code-3", "state": q.Get("state"), "iss": f.srv.URL,
		}))
	})
	if err != nil || cred.AccessToken != "at-secret" {
		t.Fatalf("Login = %+v, %v; want success after stray requests", cred, err)
	}
}

func TestLogin_NoBrowser(t *testing.T) {
	f := newFakeAS(t)
	var out bytes.Buffer
	var label string
	cred, err := Login(t.Context(), f.metadata(), LoginOptions{
		NoBrowser:   true,
		OpenBrowser: func(string) error { t.Error("OpenBrowser called with NoBrowser"); return nil },
		Prompt: func(l string) (string, error) {
			label = l
			authURL := authURLPattern.FindString(out.String())
			if authURL == "" {
				t.Errorf("authorize URL not printed before the prompt; out = %q", out.String())
				return "", errors.New("no url")
			}
			q := f.authorize(authURL, "code-4")
			if want := "https://app.example.test/oauth/cli-code"; q.Get("redirect_uri") != want {
				t.Errorf("redirect_uri = %q, want %q", q.Get("redirect_uri"), want)
			}
			return "  code-4\n", nil
		},
		Out:     &out,
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if cred.AccessToken != "at-secret" || cred.RefreshToken != "rt-secret" {
		t.Fatalf("credential = %+v", cred)
	}
	if label == "" {
		t.Error("Prompt called with an empty label")
	}
}

func TestLogin_NoBrowserEmptyCode(t *testing.T) {
	f := newFakeAS(t)
	_, err := Login(t.Context(), f.metadata(), LoginOptions{
		NoBrowser: true,
		Prompt:    func(string) (string, error) { return "  \n", nil },
		Out:       &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("Login with an empty pasted code = nil error")
	}
	if n := f.exchanges.Load(); n != 0 {
		t.Errorf("exchanges = %d, want 0", n)
	}
}

// On an SSH session the browser would open on the remote machine, if at all,
// so the --no-browser hint comes first, before the URL.
func TestLogin_SSHHintFirst(t *testing.T) {
	cases := []struct {
		name, conn, tty string
		hintFirst       bool
	}{
		{"SSH_CONNECTION", "203.0.113.5 52000 192.0.2.10 22", "", true},
		{"SSH_TTY", "", "/dev/pts/3", true},
		{"local", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SSH_CONNECTION", tc.conn)
			t.Setenv("SSH_TTY", tc.tty)
			f := newFakeAS(t)
			_, out, err := loginWithBrowser(t, f, 50*time.Millisecond, func(string) {})
			if !errors.Is(err, ErrTimeout) {
				t.Fatalf("Login = %v, want ErrTimeout", err)
			}
			const hint = "This looks like an SSH session"
			url := authURLPattern.FindStringIndex(out)
			if url == nil {
				t.Fatalf("authorize URL not printed; out = %q", out)
			}
			at := strings.Index(out, hint)
			if tc.hintFirst && (at < 0 || at > url[0] || !strings.Contains(out[:url[0]], "--no-browser")) {
				t.Errorf("out = %q, want the SSH --no-browser hint before the URL", out)
			}
			if !tc.hintFirst && at >= 0 {
				t.Errorf("out = %q, want no SSH hint", out)
			}
		})
	}
}
