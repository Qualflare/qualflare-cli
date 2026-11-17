package oauth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	// ClientID is the authorization server's pre-registered public client.
	ClientID = "qualflare-cli"
	// Scope is requested on every sign-in.
	Scope = "qualflare:read qualflare:write"
	// DefaultTimeout bounds the wait for the browser to come back.
	DefaultTimeout = 5 * time.Minute

	cliCodePath     = "/oauth/cli-code"
	exchangeTimeout = 30 * time.Second
)

var (
	// ErrCancelled means the user declined the sign-in in the browser.
	ErrCancelled = errors.New("sign-in was cancelled in the browser")
	// ErrTimeout means no callback arrived within LoginOptions.Timeout.
	ErrTimeout = errors.New("sign-in timed out")
)

// LoginOptions configures one sign-in.
type LoginOptions struct {
	// NoBrowser selects copy-the-code: the authorize URL is printed, the
	// server's code page shows a code, and Prompt reads it back.
	NoBrowser bool
	// OpenBrowser opens the authorize URL. An error is reported to Out and the
	// wait continues, since the user can open the printed URL themselves.
	OpenBrowser func(string) error
	// Prompt shows label and returns the line the user typed.
	Prompt func(label string) (string, error)
	// Out receives the authorize URL and progress lines. Never a token.
	Out io.Writer
	// Timeout bounds the wait for the browser callback; 0 means DefaultTimeout.
	Timeout time.Duration
}

// Credential is the token pair a sign-in produced.
type Credential struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// timeoutError is ErrTimeout with the wording the user sees.
type timeoutError struct{ after time.Duration }

func (e timeoutError) Error() string {
	return "Sign-in timed out after " + humanDuration(e.after) + ". Run qf login <alias> again."
}

func (e timeoutError) Is(target error) bool { return target == ErrTimeout }

// Login runs the authorization code flow with PKCE S256 against md and returns
// the issued tokens. The PKCE verifier and the state stay in memory.
func Login(ctx context.Context, md Metadata, opts LoginOptions) (Credential, error) {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	verifier := oauth2.GenerateVerifier()
	state := rand.Text()
	cfg := &oauth2.Config{
		ClientID: ClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:   md.AuthorizationEndpoint,
			TokenURL:  md.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
		Scopes: strings.Fields(Scope),
	}

	var code string
	var err error
	if opts.NoBrowser {
		code, err = codeFromUser(ctx, cfg, md, state, verifier, opts, out)
	} else {
		code, err = codeFromLoopback(ctx, cfg, md, state, verifier, opts, out, timeout)
	}
	if err != nil {
		return Credential{}, err
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, noRedirectClient(exchangeTimeout))
	tok, err := cfg.Exchange(ctx, code,
		oauth2.VerifierOption(verifier),
		oauth2.SetAuthURLParam("resource", md.Issuer),
	)
	if err != nil {
		return Credential{}, fmt.Errorf("exchange the sign-in code: %w", err)
	}
	if !strings.EqualFold(tok.Type(), "Bearer") {
		return Credential{}, fmt.Errorf("exchange the sign-in code: unexpected token type %q", clip(tok.Type()))
	}
	return Credential{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, ExpiresAt: tok.Expiry}, nil
}

func authCodeURL(cfg *oauth2.Config, md Metadata, state, verifier string) string {
	return cfg.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("resource", md.Issuer),
	)
}

// codeFromLoopback opens the browser and waits for the redirect to 127.0.0.1.
func codeFromLoopback(ctx context.Context, cfg *oauth2.Config, md Metadata, state, verifier string,
	opts LoginOptions, out io.Writer, timeout time.Duration) (string, error) {
	lb, err := startLoopback(ctx, state, md.Issuer)
	if err != nil {
		return "", err
	}
	defer lb.shutdown()
	cfg.RedirectURL = lb.redirectURI
	authURL := authCodeURL(cfg, md, state, verifier)

	_, _ = fmt.Fprintf(out, "Opening your browser to sign in. If it does not open, visit the URL below,\n"+
		"or on a remote or SSH session run qf login again with --no-browser:\n\n  %s\n\n", authURL)
	if opts.OpenBrowser != nil {
		if err := opts.OpenBrowser(authURL); err != nil {
			_, _ = fmt.Fprintf(out, "Could not open a browser (%v). Open the URL above, or on a remote or SSH session run qf login again with --no-browser.\n", err)
		}
	}
	_, _ = fmt.Fprintln(out, "Waiting for sign-in...")

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-lb.results:
		return r.code, r.err
	case <-timer.C:
		return "", timeoutError{after: timeout}
	case <-ctx.Done():
		return "", fmt.Errorf("sign-in interrupted: %w", ctx.Err())
	}
}

// codeFromUser prints the authorize URL with the server's code page as the
// redirect and reads back the code the user pastes.
func codeFromUser(ctx context.Context, cfg *oauth2.Config, md Metadata, state, verifier string,
	opts LoginOptions, out io.Writer) (string, error) {
	if opts.Prompt == nil {
		return "", errors.New("sign-in without a browser needs a prompt")
	}
	origin, err := originOf(md.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("authorization endpoint %q: %w", md.AuthorizationEndpoint, err)
	}
	cfg.RedirectURL = origin + cliCodePath
	authURL := authCodeURL(cfg, md, state, verifier)
	_, _ = fmt.Fprintf(out, "Open this URL in a browser on any device to sign in:\n\n  %s\n\n", authURL)

	type line struct {
		s   string
		err error
	}
	lines := make(chan line, 1)
	go func() {
		s, err := opts.Prompt("Paste the code shown after signing in: ")
		lines <- line{s, err}
	}()
	select {
	case l := <-lines:
		if l.err != nil {
			return "", fmt.Errorf("read the sign-in code: %w", l.err)
		}
		code := strings.TrimSpace(l.s)
		if code == "" {
			return "", errors.New("no sign-in code entered")
		}
		return code, nil
	case <-ctx.Done():
		return "", fmt.Errorf("sign-in interrupted: %w", ctx.Err())
	}
}

func humanDuration(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return "1 " + unit
		}
		return strconv.FormatInt(n, 10) + " " + unit + "s"
	}
	switch {
	case d >= time.Minute && d%time.Minute == 0:
		return plural(int64(d/time.Minute), "minute")
	case d >= time.Second && d%time.Second == 0:
		return plural(int64(d/time.Second), "second")
	default:
		return d.String()
	}
}
