package http

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"qualflare-cli/internal/core/domain"
)

// tokenGate answers 401 to any bearer token other than valid and records every
// request's token and body.
type tokenGate struct {
	mu     sync.Mutex
	valid  string
	tokens []string
	bodies []string
}

func (g *tokenGate) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	g.mu.Lock()
	g.tokens = append(g.tokens, tok)
	g.bodies = append(g.bodies, string(b))
	valid := g.valid
	g.mu.Unlock()
	if tok != valid {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"code":"auth.invalid_token","message":"token expired"}`)
		return
	}
	_, _ = io.WriteString(w, `{"data":[]}`)
}

// retryRecorder is an UnauthorizedRetry that hands out token and records what
// it was called with.
type retryRecorder struct {
	calls    atomic.Int32
	rejected []string
	mu       sync.Mutex
	token    string
	err      error
}

func (r *retryRecorder) fn(_ context.Context, rejected string) (string, error) {
	r.calls.Add(1)
	r.mu.Lock()
	r.rejected = append(r.rejected, rejected)
	r.mu.Unlock()
	return r.token, r.err
}

func newGatedClient(t *testing.T, g *tokenGate, r *retryRecorder, debug bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(g.handler))
	t.Cleanup(srv.Close)
	c := NewHTTPClient(&stubConfig{endpoint: srv.URL, apiKey: "at-old", debug: debug}, WithUnauthorizedRetry(r.fn))
	t.Cleanup(c.Close)
	return c
}

func TestClient_UnauthorizedRetryOnce(t *testing.T) {
	g := &tokenGate{valid: "at-new"}
	r := &retryRecorder{token: "at-new"}
	c := newGatedClient(t, g, r, false)

	// A POST with a body: the retry must send the same body again.
	launch := &domain.Launch{Framework: "nightly run"}
	if err := c.SendReport(t.Context(), launch); err != nil {
		t.Fatalf("SendReport: %v", err)
	}
	if got := r.calls.Load(); got != 1 {
		t.Fatalf("retry calls = %d, want 1", got)
	}
	if r.rejected[0] != "at-old" {
		t.Errorf("retry got rejected token %q, want at-old", r.rejected[0])
	}
	if len(g.tokens) != 2 || g.tokens[0] != "at-old" || g.tokens[1] != "at-new" {
		t.Fatalf("tokens sent = %v, want [at-old at-new]", g.tokens)
	}
	if g.bodies[1] == "" || g.bodies[1] != g.bodies[0] {
		t.Errorf("retry body = %q, want the first body %q", g.bodies[1], g.bodies[0])
	}
	if !strings.Contains(g.bodies[1], "nightly run") {
		t.Errorf("retry body %q lacks the launch", g.bodies[1])
	}

	// The new token sticks: the next request carries it and needs no retry.
	if _, err := c.Get(t.Context(), apiBasePath+"/suites", nil); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := g.tokens[len(g.tokens)-1]; got != "at-new" {
		t.Errorf("next request token = %q, want at-new", got)
	}
	if got := r.calls.Load(); got != 1 {
		t.Errorf("retry calls after the next request = %d, want still 1", got)
	}
}

func TestClient_UnauthorizedSecond401IsTheError(t *testing.T) {
	g := &tokenGate{valid: "never"}
	r := &retryRecorder{token: "at-new"}
	c := newGatedClient(t, g, r, false)

	_, err := c.Get(t.Context(), apiBasePath+"/suites", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a 401 APIError", err)
	}
	if got := r.calls.Load(); got != 1 {
		t.Errorf("retry calls = %d, want 1 (no loop)", got)
	}
	if len(g.tokens) != 2 {
		t.Errorf("requests = %d, want 2", len(g.tokens))
	}
}

// A pasted-token alias has nothing to refresh: the hook returns "" and the
// first 401 is the answer.
func TestClient_UnauthorizedNoTokenMeansNoRetry(t *testing.T) {
	g := &tokenGate{valid: "never"}
	r := &retryRecorder{}
	c := newGatedClient(t, g, r, false)

	err := c.SendReport(t.Context(), &domain.Launch{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a 401 APIError", err)
	}
	if len(g.tokens) != 1 {
		t.Errorf("requests = %d, want 1", len(g.tokens))
	}
}

func TestClient_UnauthorizedRetryErrorIsReturned(t *testing.T) {
	g := &tokenGate{valid: "never"}
	signInAgain := errors.New(`Your sign-in for "myapp" has ended. Run qf login myapp again.`)
	r := &retryRecorder{err: signInAgain}
	c := newGatedClient(t, g, r, false)

	_, err := c.Get(t.Context(), apiBasePath+"/suites", nil)
	if !errors.Is(err, signInAgain) {
		t.Fatalf("err = %v, want the refresh error", err)
	}
	if err.Error() != signInAgain.Error() {
		t.Errorf("message = %q, want it as the refresh said it", err.Error())
	}
	if len(g.tokens) != 1 {
		t.Errorf("requests = %d, want 1", len(g.tokens))
	}
}

func TestClient_NoRetryHookKeeps401(t *testing.T) {
	g := &tokenGate{valid: "never"}
	srv := httptest.NewServer(http.HandlerFunc(g.handler))
	t.Cleanup(srv.Close)
	c := NewHTTPClient(&stubConfig{endpoint: srv.URL, apiKey: "at-old"})
	t.Cleanup(c.Close)
	_, err := c.Get(t.Context(), apiBasePath+"/suites", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a 401 APIError", err)
	}
	if len(g.tokens) != 1 {
		t.Errorf("requests = %d, want 1", len(g.tokens))
	}
}

// The presign POST goes through the same retry.
func TestClient_UnauthorizedRetryOnPresign(t *testing.T) {
	var puts atomic.Int32
	put := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("PUT to storage carried the Authorization header")
		}
		puts.Add(1)
	}))
	t.Cleanup(put.Close)
	g := &tokenGate{valid: "at-new"}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-new" {
			g.handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"uploadUrl":%q,"storageKey":"k1"}`, put.URL+"/obj")
	}))
	t.Cleanup(api.Close)
	r := &retryRecorder{token: "at-new"}
	c := NewHTTPClient(&stubConfig{endpoint: api.URL, apiKey: "at-old"}, WithUnauthorizedRetry(r.fn))
	t.Cleanup(c.Close)

	key, _, err := c.UploadAttachment(t.Context(), []byte("png"), "image/png", "a.png")
	if err != nil {
		t.Fatalf("UploadAttachment: %v", err)
	}
	if key != "k1" || puts.Load() != 1 || r.calls.Load() != 1 {
		t.Errorf("key=%q puts=%d retries=%d, want k1, 1, 1", key, puts.Load(), r.calls.Load())
	}
}

// Concurrent requests rejected with the same token each ask the hook, which is
// given the token their own request carried.
func TestClient_UnauthorizedConcurrentRejectedToken(t *testing.T) {
	g := &tokenGate{valid: "at-new"}
	r := &retryRecorder{token: "at-new"}
	c := newGatedClient(t, g, r, false)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := c.Get(t.Context(), apiBasePath+"/suites", nil); err != nil {
				t.Errorf("Get: %v", err)
			}
		})
	}
	wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rej := range r.rejected {
		if rej != "at-old" {
			t.Errorf("hook got rejected token %q, want at-old", rej)
		}
	}
}

type captureLogger struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *captureLogger) Errorf(f string, v ...any) { l.write(f, v...) }
func (l *captureLogger) Warnf(f string, v ...any)  { l.write(f, v...) }
func (l *captureLogger) Debugf(f string, v ...any) { l.write(f, v...) }
func (l *captureLogger) write(f string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.buf, f, v...)
}

// --debug logs both attempts; neither token may appear.
func TestClient_UnauthorizedRetryDebugRedacted(t *testing.T) {
	g := &tokenGate{valid: "at-new"}
	r := &retryRecorder{token: "at-new"}
	c := newGatedClient(t, g, r, true)
	log := &captureLogger{}
	c.resty.SetLogger(log)

	if err := c.SendReport(t.Context(), &domain.Launch{}); err != nil {
		t.Fatalf("SendReport: %v", err)
	}
	out := log.buf.String()
	if !strings.Contains(out, "REDACTED") {
		t.Fatalf("debug output has no redacted header; got:\n%s", out)
	}
	for _, tok := range []string{"at-old", "at-new"} {
		if strings.Contains(out, tok) {
			t.Errorf("debug output contains token %q", tok)
		}
	}
}
