package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func metadataServer(t *testing.T, doc func(base string) map[string]any) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-authorization-server" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc(srv.URL))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func goodDoc(base string) map[string]any {
	return map[string]any{
		"issuer":                 base,
		"authorization_endpoint": "https://app.example.test/oauth/authorize",
		"token_endpoint":         base + "/oauth/token",
		"revocation_endpoint":    base + "/oauth/revoke",
		"authorization_response_iss_parameter_supported": true,
	}
}

func TestDiscover_Success(t *testing.T) {
	srv := metadataServer(t, goodDoc)
	for _, endpoint := range []string{srv.URL, srv.URL + "/"} {
		md, err := Discover(t.Context(), endpoint)
		if err != nil {
			t.Fatalf("Discover(%q): %v", endpoint, err)
		}
		want := Metadata{
			Issuer:                srv.URL,
			AuthorizationEndpoint: "https://app.example.test/oauth/authorize",
			TokenEndpoint:         srv.URL + "/oauth/token",
			RevocationEndpoint:    srv.URL + "/oauth/revoke",
		}
		if md != want {
			t.Fatalf("Discover = %+v, want %+v", md, want)
		}
	}
}

// A QF_API_URL typed in upper case still matches the issuer the server prints.
func TestDiscover_HostCaseInsensitive(t *testing.T) {
	srv := metadataServer(t, func(base string) map[string]any {
		return goodDoc(strings.Replace(base, "127.0.0.1", "localhost", 1))
	})
	endpoint := strings.Replace(srv.URL, "127.0.0.1", "LOCALHOST", 1)
	md, err := Discover(t.Context(), endpoint)
	if err != nil {
		t.Fatalf("Discover(%q): %v", endpoint, err)
	}
	if !strings.HasPrefix(md.Issuer, "http://localhost:") {
		t.Errorf("Issuer = %q", md.Issuer)
	}
}

func TestDiscover_NotFoundIsNoAuthServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	if _, err := Discover(t.Context(), srv.URL); !errors.Is(err, ErrNoAuthServer) {
		t.Fatalf("Discover = %v, want ErrNoAuthServer", err)
	}
}

func TestDiscover_MissingEndpointsIsNoAuthServer(t *testing.T) {
	for _, field := range []string{"authorization_endpoint", "token_endpoint"} {
		srv := metadataServer(t, func(base string) map[string]any {
			d := goodDoc(base)
			delete(d, field)
			return d
		})
		if _, err := Discover(t.Context(), srv.URL); !errors.Is(err, ErrNoAuthServer) {
			t.Errorf("without %s: Discover = %v, want ErrNoAuthServer", field, err)
		}
	}
}

func TestDiscover_Refusals(t *testing.T) {
	cases := map[string]func(base string) map[string]any{
		"issuer on another host": func(base string) map[string]any {
			d := goodDoc(base)
			d["issuer"] = "https://evil.example"
			return d
		},
		"missing issuer": func(base string) map[string]any {
			d := goodDoc(base)
			delete(d, "issuer")
			return d
		},
		"plaintext token endpoint": func(base string) map[string]any {
			d := goodDoc(base)
			d["token_endpoint"] = "http://evil.example/oauth/token"
			return d
		},
		"relative authorize endpoint": func(base string) map[string]any {
			d := goodDoc(base)
			d["authorization_endpoint"] = "/oauth/authorize"
			return d
		},
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := metadataServer(t, doc)
			_, err := Discover(t.Context(), srv.URL)
			if err == nil {
				t.Fatal("Discover = nil error, want a refusal")
			}
			if errors.Is(err, ErrNoAuthServer) {
				t.Fatalf("Discover = %v; a bad document must not fall back to token paste silently", err)
			}
		})
	}
}

func TestDiscover_DoesNotFollowRedirects(t *testing.T) {
	target := metadataServer(t, goodDoc)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	_, err := Discover(t.Context(), srv.URL)
	if err == nil {
		t.Fatal("Discover followed a redirect")
	}
}

func TestDiscover_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	_, err := Discover(t.Context(), srv.URL)
	if err == nil || errors.Is(err, ErrNoAuthServer) {
		t.Fatalf("Discover = %v, want a plain error", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q should name the status", err)
	}
}
