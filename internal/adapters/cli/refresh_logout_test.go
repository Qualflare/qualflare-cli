package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/oauth"
)

// fakeIssuer serves authorization server metadata and a revocation endpoint
// that records each form it receives.
type fakeIssuer struct {
	srv          *httptest.Server
	mu           sync.Mutex
	revokes      []url.Values
	revokeStatus int
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	f := &fakeIssuer{revokeStatus: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 f.srv.URL,
			"authorization_endpoint": f.srv.URL + "/oauth/authorize",
			"token_endpoint":         f.srv.URL + "/oauth/token",
			"revocation_endpoint":    f.srv.URL + "/oauth/revoke",
		})
	})
	mux.HandleFunc("/oauth/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.revokes = append(f.revokes, r.PostForm)
		status := f.revokeStatus
		f.mu.Unlock()
		w.WriteHeader(status)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// saveOAuthAlias saves an OAuth "myapp" signed in at issuer and points the API
// endpoint at that issuer, as a sign-in leaves it.
func saveOAuthAlias(t *testing.T, c *CLI, issuer string, expires time.Time) {
	t.Helper()
	c.config.APIEndpoint = issuer
	err := c.saveLocked(func(s *auth.Store) {
		s.SetOAuth("myapp", auth.Identifier{
			AccessToken: "at-0", RefreshToken: "rt-0", ExpiresAt: expires, Issuer: issuer,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func runLogoutCmd(t *testing.T, c *CLI) (string, error) {
	t.Helper()
	cmd := c.createLogoutCommand()
	cmd.SetArgs([]string{"myapp"})
	var stderr bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	err := cmd.Execute()
	return stderr.String(), err
}

func TestLogout_OAuthRevokes(t *testing.T) {
	f := newFakeIssuer(t)
	c, storePath := newCLIForLoginTest(t)
	saveOAuthAlias(t, c, f.srv.URL, time.Now().Add(time.Hour))

	stderr, err := runLogoutCmd(t, c)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if len(f.revokes) != 1 {
		t.Fatalf("revocation requests = %d, want 1", len(f.revokes))
	}
	if got := f.revokes[0]; got.Get("token") != "rt-0" || got.Get("client_id") != oauth.ClientID {
		t.Errorf("revoke form = %v, want token=rt-0 client_id=%s", got, oauth.ClientID)
	}
	if _, ok := reloadEntry(t, storePath); ok {
		t.Error("alias still saved after logout")
	}
	if strings.Contains(stderr, "rt-0") || strings.Contains(stderr, "at-0") {
		t.Errorf("output quotes a token: %q", stderr)
	}
}

func TestLogout_OAuthRevokeFailureWarnsAndDeletes(t *testing.T) {
	f := newFakeIssuer(t)
	f.revokeStatus = http.StatusServiceUnavailable
	c, storePath := newCLIForLoginTest(t)
	saveOAuthAlias(t, c, f.srv.URL, time.Now().Add(time.Hour))

	stderr, err := runLogoutCmd(t, c)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if !strings.Contains(stderr, "warning:") {
		t.Errorf("stderr = %q, want a warning", stderr)
	}
	if strings.Contains(stderr, "rt-0") {
		t.Errorf("warning quotes the refresh token: %q", stderr)
	}
	if _, ok := reloadEntry(t, storePath); ok {
		t.Error("alias still saved after a failed revoke")
	}
}

func TestLogout_PastedAliasMakesNoHTTPCall(t *testing.T) {
	orig := discoverOAuth
	t.Cleanup(func() { discoverOAuth = orig })
	discoverOAuth = func(context.Context, string) (oauth.Metadata, error) {
		t.Error("discovery called for a pasted alias")
		return oauth.Metadata{}, errors.New("unexpected")
	}
	c, storePath := newCLIForLoginTest(t)
	if err := c.saveLocked(func(s *auth.Store) { s.Set("myapp", "qf_pasted") }); err != nil {
		t.Fatal(err)
	}
	if _, err := runLogoutCmd(t, c); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, ok := reloadEntry(t, storePath); ok {
		t.Error("alias still saved after logout")
	}
}

func TestLogout_UnknownAlias(t *testing.T) {
	c, _ := newCLIForLoginTest(t)
	_, err := runLogoutCmd(t, c)
	if err == nil || !strings.Contains(err.Error(), `identifier "myapp" not found`) {
		t.Fatalf("err = %v, want not found", err)
	}
}

// refreshHooks stubs discovery and the token refresh.
type refreshHooks struct {
	discovered []string
	refreshed  []string
	err        error
}

func stubRefreshHooks(t *testing.T) *refreshHooks {
	t.Helper()
	h := &refreshHooks{}
	origDiscover, origRefresh := discoverOAuth, refreshOAuth
	t.Cleanup(func() { discoverOAuth, refreshOAuth = origDiscover, origRefresh })
	discoverOAuth = func(_ context.Context, endpoint string) (oauth.Metadata, error) {
		h.discovered = append(h.discovered, endpoint)
		return testMetadata, nil
	}
	refreshOAuth = func(_ context.Context, md oauth.Metadata, rt string) (oauth.Credential, error) {
		if md != testMetadata {
			t.Errorf("refresh got metadata %+v", md)
		}
		h.refreshed = append(h.refreshed, rt)
		if h.err != nil {
			return oauth.Credential{}, h.err
		}
		return oauth.Credential{AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	return h
}

func runPreRun(t *testing.T, c *CLI, sub string) error {
	t.Helper()
	parent := c.createIdentifierCommand("myapp", "")
	child := &cobra.Command{Use: sub}
	parent.AddCommand(child)
	return parent.PersistentPreRunE(child, nil)
}

func TestPreRun_RefreshesAnExpiringOAuthToken(t *testing.T) {
	h := stubRefreshHooks(t)
	c, storePath := newCLIForLoginTest(t)
	saveOAuthAlias(t, c, testMetadata.Issuer, time.Now().Add(20*time.Second))
	// The same origin, written differently: still the issuer's server.
	c.config.APIEndpoint = "https://API.example.test:443/"

	if err := runPreRun(t, c, "suites"); err != nil {
		t.Fatalf("pre-run: %v", err)
	}
	if got := c.config.GetAPIKey(); got != "at-1" {
		t.Errorf("API key = %q, want at-1", got)
	}
	if len(h.discovered) != 1 || h.discovered[0] != testMetadata.Issuer {
		t.Errorf("discovered %v, want the entry's issuer %s", h.discovered, testMetadata.Issuer)
	}
	if len(h.refreshed) != 1 || h.refreshed[0] != "rt-0" {
		t.Errorf("refreshed %v, want [rt-0]", h.refreshed)
	}
	e, _ := reloadEntry(t, storePath)
	if e.AccessToken != "at-1" || e.RefreshToken != "rt-1" || e.Issuer != testMetadata.Issuer {
		t.Errorf("saved entry = %+v", e)
	}
}

func TestPreRun_UsesAValidOAuthTokenAsIs(t *testing.T) {
	h := stubRefreshHooks(t)
	c, _ := newCLIForLoginTest(t)
	saveOAuthAlias(t, c, testMetadata.Issuer, time.Now().Add(time.Hour))
	if err := runPreRun(t, c, "suites"); err != nil {
		t.Fatalf("pre-run: %v", err)
	}
	if got := c.config.GetAPIKey(); got != "at-0" {
		t.Errorf("API key = %q, want at-0", got)
	}
	if len(h.refreshed) != 0 {
		t.Errorf("refreshed %v, want none", h.refreshed)
	}
}

func TestPreRun_InvalidGrantSaysSignInAgain(t *testing.T) {
	h := stubRefreshHooks(t)
	h.err = oauth.ErrInvalidGrant
	c, _ := newCLIForLoginTest(t)
	saveOAuthAlias(t, c, testMetadata.Issuer, time.Now().Add(-time.Minute))
	err := runPreRun(t, c, "suites")
	if !errors.Is(err, auth.ErrSignInAgain) {
		t.Fatalf("err = %v, want ErrSignInAgain", err)
	}
	if err.Error() != `Your sign-in for "myapp" has ended. Run qf login myapp again.` {
		t.Errorf("message = %q", err.Error())
	}
}

// validate never calls the API, so an expired sign-in must not stop it.
func TestPreRun_ValidateSkipsTheRefresh(t *testing.T) {
	h := stubRefreshHooks(t)
	c, _ := newCLIForLoginTest(t)
	saveOAuthAlias(t, c, testMetadata.Issuer, time.Now().Add(-time.Minute))
	if err := runPreRun(t, c, "validate"); err != nil {
		t.Fatalf("pre-run: %v", err)
	}
	if len(h.refreshed) != 0 {
		t.Errorf("refreshed %v, want none", h.refreshed)
	}
}

func TestPreRun_PastedTokenUnchanged(t *testing.T) {
	stubRefreshHooks(t)
	c, _ := newCLIForLoginTest(t)
	if err := c.saveLocked(func(s *auth.Store) { s.Set("myapp", "qf_pasted") }); err != nil {
		t.Fatal(err)
	}
	parent := c.createIdentifierCommand("myapp", "qf_pasted")
	child := &cobra.Command{Use: "suites"}
	parent.AddCommand(child)
	if err := parent.PersistentPreRunE(child, nil); err != nil {
		t.Fatalf("pre-run: %v", err)
	}
	if got := c.config.GetAPIKey(); got != "qf_pasted" {
		t.Errorf("API key = %q, want qf_pasted", got)
	}
}

func TestRetryUnauthorized_OAuthRefreshes(t *testing.T) {
	h := stubRefreshHooks(t)
	c, _ := newCLIForLoginTest(t)
	saveOAuthAlias(t, c, testMetadata.Issuer, time.Now().Add(time.Hour))
	if err := runPreRun(t, c, "suites"); err != nil {
		t.Fatal(err)
	}
	tok, err := c.RetryUnauthorized(t.Context(), "at-0")
	if err != nil {
		t.Fatalf("RetryUnauthorized: %v", err)
	}
	if tok != "at-1" || len(h.refreshed) != 1 {
		t.Errorf("token = %q after %d refreshes, want at-1 after 1", tok, len(h.refreshed))
	}
}

func TestRetryUnauthorized_PastedAliasNoRetry(t *testing.T) {
	h := stubRefreshHooks(t)
	c, _ := newCLIForLoginTest(t)
	if err := c.saveLocked(func(s *auth.Store) { s.Set("myapp", "qf_pasted") }); err != nil {
		t.Fatal(err)
	}
	parent := c.createIdentifierCommand("myapp", "qf_pasted")
	child := &cobra.Command{Use: "suites"}
	parent.AddCommand(child)
	if err := parent.PersistentPreRunE(child, nil); err != nil {
		t.Fatal(err)
	}
	tok, err := c.RetryUnauthorized(t.Context(), "qf_pasted")
	if err != nil || tok != "" {
		t.Errorf("RetryUnauthorized = %q, %v; want no retry", tok, err)
	}
	if len(h.refreshed) != 0 {
		t.Errorf("refreshed %v, want none", h.refreshed)
	}
}

// No alias was selected (a flat command): nothing to refresh.
func TestRetryUnauthorized_NoActiveAlias(t *testing.T) {
	c, _ := newCLIForLoginTest(t)
	tok, err := c.RetryUnauthorized(t.Context(), "x")
	if err != nil || tok != "" {
		t.Errorf("RetryUnauthorized = %q, %v; want no retry", tok, err)
	}
}

// An OAuth alias belongs to the server that issued it. With the API endpoint
// pointing elsewhere, the command is refused before any refresh or request, so
// its tokens never reach the other server.
func TestPreRun_RefusesAnEndpointOtherThanTheIssuer(t *testing.T) {
	cases := []struct {
		name, endpoint, shown string
	}{
		{"QF_API_URL override", "http://127.0.0.1:8003", "http://127.0.0.1:8003"},
		{"default endpoint", "", "https://api.qualflare.com"},
		{"other port", "https://api.example.test:8443", "https://api.example.test:8443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := stubRefreshHooks(t)
			c, storePath := newCLIForLoginTest(t)
			// Expired, so a pre-run that went ahead would refresh.
			saveOAuthAlias(t, c, testMetadata.Issuer, time.Now().Add(-time.Minute))
			c.config.APIEndpoint = tc.endpoint

			err := runPreRun(t, c, "suites")
			want := `"myapp" was signed in at https://api.example.test, but the API endpoint is ` + tc.shown +
				`. Unset QF_API_URL, or run qf login myapp again.`
			if err == nil || err.Error() != want {
				t.Fatalf("pre-run = %v, want %q", err, want)
			}
			if len(h.discovered) != 0 || len(h.refreshed) != 0 {
				t.Errorf("discovered %v, refreshed %v; want neither", h.discovered, h.refreshed)
			}
			if got := c.config.GetAPIKey(); got != "" {
				t.Errorf("API key = %q, want none set", got)
			}
			if e, _ := reloadEntry(t, storePath); e.RefreshToken != "rt-0" {
				t.Errorf("entry changed: %+v", e)
			}
		})
	}
}

// Logging out of an alias whose issuer is not the configured endpoint still
// revokes at the issuer and removes the alias.
func TestLogout_OAuthAliasOfAnotherServer(t *testing.T) {
	f := newFakeIssuer(t)
	c, storePath := newCLIForLoginTest(t)
	saveOAuthAlias(t, c, f.srv.URL, time.Now().Add(time.Hour))
	c.config.APIEndpoint = "http://127.0.0.1:1"

	if _, err := runLogoutCmd(t, c); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if len(f.revokes) != 1 || f.revokes[0].Get("token") != "rt-0" {
		t.Errorf("revocations = %v, want one of rt-0 at the issuer", f.revokes)
	}
	if _, ok := reloadEntry(t, storePath); ok {
		t.Error("alias still saved after logout")
	}
}

// A refresh that fails on the network or a 5xx while the token is still valid
// lets the command go on with that token; the warning goes to stderr.
func TestPreRun_TransientRefreshFailureUsesTheValidToken(t *testing.T) {
	h := stubRefreshHooks(t)
	h.err = errors.New("refresh the sign-in: unexpected status 503")
	c, _ := newCLIForLoginTest(t)
	var stderr bytes.Buffer
	c.errOut = &stderr
	saveOAuthAlias(t, c, testMetadata.Issuer, time.Now().Add(30*time.Second))

	if err := runPreRun(t, c, "suites"); err != nil {
		t.Fatalf("pre-run: %v", err)
	}
	if got := c.config.GetAPIKey(); got != "at-0" {
		t.Errorf("API key = %q, want the current at-0", got)
	}
	if !strings.HasPrefix(stderr.String(), "warning: could not refresh the sign-in for \"myapp\"") ||
		strings.Contains(stderr.String(), "at-0") || strings.Contains(stderr.String(), "rt-0") {
		t.Errorf("stderr = %q, want a warning that names no token", stderr.String())
	}
}
