package oauth

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// tokenServer records the form of each POST and answers with status and body.
type tokenServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	forms  []url.Values
	status int
	body   string
}

func newTokenServer(t *testing.T, status int, body string) *tokenServer {
	t.Helper()
	ts := &tokenServer{status: status, body: body}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("public client sent an Authorization header")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		ts.mu.Lock()
		ts.forms = append(ts.forms, r.PostForm)
		ts.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ts.status)
		_, _ = io.WriteString(w, ts.body)
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *tokenServer) metadata() Metadata {
	return Metadata{
		Issuer:             ts.srv.URL,
		TokenEndpoint:      ts.srv.URL + "/oauth/token",
		RevocationEndpoint: ts.srv.URL + "/oauth/revoke",
	}
}

func TestRefresh_Success(t *testing.T) {
	ts := newTokenServer(t, http.StatusOK,
		`{"access_token":"at-2","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-2"}`)
	cred, err := Refresh(t.Context(), ts.metadata(), "rt-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if cred.AccessToken != "at-2" || cred.RefreshToken != "rt-2" {
		t.Errorf("credential = %+v, want at-2/rt-2", cred)
	}
	if d := time.Until(cred.ExpiresAt); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("expires in %v, want about an hour", d)
	}
	if len(ts.forms) != 1 {
		t.Fatalf("requests = %d, want 1", len(ts.forms))
	}
	f := ts.forms[0]
	for k, want := range map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": "rt-1",
		"client_id":     ClientID,
	} {
		if f.Get(k) != want {
			t.Errorf("form %s = %q, want %q", k, f.Get(k), want)
		}
	}
}

func TestRefresh_InvalidGrant(t *testing.T) {
	ts := newTokenServer(t, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"grant revoked"}`)
	_, err := Refresh(t.Context(), ts.metadata(), "rt-1")
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("err = %v, want ErrInvalidGrant", err)
	}
}

func TestRefresh_OtherErrorIsNotInvalidGrant(t *testing.T) {
	ts := newTokenServer(t, http.StatusInternalServerError, `{"error":"server_error"}`)
	_, err := Refresh(t.Context(), ts.metadata(), "rt-1")
	if err == nil || errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("err = %v, want a plain error", err)
	}
	if strings.Contains(err.Error(), "rt-1") {
		t.Errorf("error %q quotes the refresh token", err)
	}
}

func TestRevoke_PostsTokenAndClientID(t *testing.T) {
	ts := newTokenServer(t, http.StatusOK, `{}`)
	if err := Revoke(t.Context(), ts.metadata(), "rt-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if len(ts.forms) != 1 {
		t.Fatalf("requests = %d, want 1", len(ts.forms))
	}
	f := ts.forms[0]
	if f.Get("token") != "rt-1" || f.Get("client_id") != ClientID {
		t.Errorf("form = %v, want token=rt-1 and client_id=%s", f, ClientID)
	}
}

func TestRevoke_ErrorStatus(t *testing.T) {
	ts := newTokenServer(t, http.StatusServiceUnavailable, `{}`)
	err := Revoke(t.Context(), ts.metadata(), "rt-1")
	if err == nil {
		t.Fatal("Revoke succeeded on a 503")
	}
	if strings.Contains(err.Error(), "rt-1") {
		t.Errorf("error %q quotes the refresh token", err)
	}
}

func TestRevoke_NoEndpoint(t *testing.T) {
	if err := Revoke(t.Context(), Metadata{Issuer: "https://api.example.test"}, "rt-1"); err == nil {
		t.Fatal("Revoke succeeded without a revocation endpoint")
	}
}
