package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeAppAuthorize is the authorization endpoint the fake metadata advertises. It
// is on a different host from the issuer, as in production, and is never fetched:
// the tests play the browser themselves.
const fakeAppAuthorize = "https://app.example.test/oauth/authorize"

type grant struct {
	challenge   string
	redirectURI string
}

// fakeAS is a fake authorization server: it serves metadata and a token endpoint
// that checks the PKCE verifier, client_id, redirect_uri and resource.
type fakeAS struct {
	t         *testing.T
	srv       *httptest.Server
	exchanges atomic.Int32

	mu     sync.Mutex
	grants map[string]grant
}

func newFakeAS(t *testing.T) *fakeAS {
	t.Helper()
	f := &fakeAS{t: t, grants: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 f.srv.URL,
			"authorization_endpoint": fakeAppAuthorize,
			"token_endpoint":         f.srv.URL + "/oauth/token",
			"revocation_endpoint":    f.srv.URL + "/oauth/revoke",
			"authorization_response_iss_parameter_supported": true,
		})
	})
	mux.HandleFunc("/oauth/token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAS) metadata() Metadata {
	return Metadata{
		Issuer:                f.srv.URL,
		AuthorizationEndpoint: fakeAppAuthorize,
		TokenEndpoint:         f.srv.URL + "/oauth/token",
		RevocationEndpoint:    f.srv.URL + "/oauth/revoke",
	}
}

// authorize plays the authorization endpoint: it checks the authorize URL the
// CLI built and records a code bound to its PKCE challenge and redirect URI.
func (f *fakeAS) authorize(authURL, code string) url.Values {
	f.t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		f.t.Errorf("authorize URL %q: %v", authURL, err)
		return nil
	}
	q := u.Query()
	if got := u.Scheme + "://" + u.Host + u.Path; got != fakeAppAuthorize {
		f.t.Errorf("authorize URL base = %q, want %q", got, fakeAppAuthorize)
	}
	for k, want := range map[string]string{
		"response_type":         "code",
		"client_id":             ClientID,
		"scope":                 Scope,
		"code_challenge_method": "S256",
		"resource":              f.srv.URL,
	} {
		if q.Get(k) != want {
			f.t.Errorf("authorize %s = %q, want %q", k, q.Get(k), want)
		}
	}
	if q.Get("state") == "" || q.Get("code_challenge") == "" {
		f.t.Errorf("authorize URL lacks state or code_challenge: %s", authURL)
	}
	f.mu.Lock()
	f.grants[code] = grant{challenge: q.Get("code_challenge"), redirectURI: q.Get("redirect_uri")}
	f.mu.Unlock()
	return q
}

func (f *fakeAS) token(w http.ResponseWriter, r *http.Request) {
	f.exchanges.Add(1)
	fail := func(msg string) {
		f.t.Errorf("token request: %s", msg)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
	}
	if r.Method != http.MethodPost {
		fail("method " + r.Method)
		return
	}
	if r.Header.Get("Authorization") != "" {
		fail("public client sent an Authorization header")
		return
	}
	if err := r.ParseForm(); err != nil {
		fail(err.Error())
		return
	}
	f.mu.Lock()
	g, ok := f.grants[r.PostForm.Get("code")]
	f.mu.Unlock()
	if !ok {
		fail("unknown code " + r.PostForm.Get("code"))
		return
	}
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	switch {
	case r.PostForm.Get("grant_type") != "authorization_code":
		fail("grant_type " + r.PostForm.Get("grant_type"))
		return
	case base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge:
		fail("code_verifier does not match the S256 challenge")
		return
	case r.PostForm.Get("client_id") != ClientID:
		fail("client_id " + r.PostForm.Get("client_id"))
		return
	case r.PostForm.Get("redirect_uri") != g.redirectURI:
		fail("redirect_uri " + r.PostForm.Get("redirect_uri") + " != " + g.redirectURI)
		return
	case r.PostForm.Get("resource") != f.srv.URL:
		fail("resource " + r.PostForm.Get("resource"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"access_token":"at-secret","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-secret","scope":"qualflare:read qualflare:write"}`)
}

var authURLPattern = regexp.MustCompile(`https://app\.example\.test/oauth/authorize\?\S+`)

// browserGet plays the browser following the redirect back to the CLI.
func browserGet(t *testing.T, rawURL string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("GET %s: %v", rawURL, err)
		return 0, ""
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func callbackURL(redirectURI string, params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	return redirectURI + "?" + q.Encode()
}
