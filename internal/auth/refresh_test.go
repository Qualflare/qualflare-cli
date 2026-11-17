package auth

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// writeOAuthEntry saves an OAuth entry for "myapp" and returns the store path.
func writeOAuthEntry(t *testing.T, access, refresh string, expires time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qualflare", "config.toml")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.SetOAuth("myapp", Identifier{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    expires,
		Issuer:       "https://api.example.test",
	})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustLoad(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// rotatingServer plays the token endpoint: each use of the current refresh
// token issues the next pair, and a superseded one is refused as invalid_grant.
type rotatingServer struct {
	mu      sync.Mutex
	gen     int
	calls   atomic.Int32
	delay   time.Duration
	refused error
}

func (r *rotatingServer) refresh(_ context.Context, rt string) (Identifier, error) {
	r.calls.Add(1)
	time.Sleep(r.delay)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refused != nil {
		return Identifier{}, r.refused
	}
	if rt != fmt.Sprintf("rt-%d", r.gen) {
		return Identifier{}, fmt.Errorf("token endpoint: %w", ErrInvalidGrant)
	}
	r.gen++
	return Identifier{
		AccessToken:  fmt.Sprintf("at-%d", r.gen),
		RefreshToken: fmt.Sprintf("rt-%d", r.gen),
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
	}, nil
}

func TestFreshToken_NotExpiring(t *testing.T) {
	path := writeOAuthEntry(t, "at-0", "rt-0", time.Now().Add(10*time.Minute))
	srv := &rotatingServer{}
	got, err := FreshToken(t.Context(), mustLoad(t, path), "myapp", srv.refresh, false, "")
	if err != nil {
		t.Fatalf("FreshToken: %v", err)
	}
	if got != "at-0" {
		t.Errorf("token = %q, want at-0", got)
	}
	if n := srv.calls.Load(); n != 0 {
		t.Errorf("refresh calls = %d, want 0", n)
	}
}

func TestFreshToken_Within60s(t *testing.T) {
	path := writeOAuthEntry(t, "at-0", "rt-0", time.Now().Add(30*time.Second))
	srv := &rotatingServer{}
	got, err := FreshToken(t.Context(), mustLoad(t, path), "myapp", srv.refresh, false, "")
	if err != nil {
		t.Fatalf("FreshToken: %v", err)
	}
	if got != "at-1" {
		t.Errorf("token = %q, want at-1", got)
	}
	if n := srv.calls.Load(); n != 1 {
		t.Errorf("refresh calls = %d, want 1", n)
	}
	e, ok := mustLoad(t, path).Entry("myapp")
	if !ok {
		t.Fatal("entry gone after refresh")
	}
	if e.Kind != KindOAuth || e.AccessToken != "at-1" || e.RefreshToken != "rt-1" {
		t.Errorf("saved entry = %+v, want the new pair", e)
	}
	if e.Issuer != "https://api.example.test" {
		t.Errorf("issuer = %q, want it kept", e.Issuer)
	}
	if time.Until(e.ExpiresAt) < 50*time.Minute {
		t.Errorf("expires_at = %v, want about an hour ahead", e.ExpiresAt)
	}
}

// Two processes with their own view of one file, both holding a token about to
// expire: the lock and the re-read under it mean exactly one refresh, and the
// second process uses the pair the first saved (Review Focus 2).
func TestFreshToken_ConcurrentProcesses(t *testing.T) {
	path := writeOAuthEntry(t, "at-0", "rt-0", time.Now().Add(20*time.Second))
	srv := &rotatingServer{delay: 100 * time.Millisecond}
	stores := []*Store{mustLoad(t, path), mustLoad(t, path)}

	var wg sync.WaitGroup
	tokens := make([]string, len(stores))
	errs := make([]error, len(stores))
	for i, s := range stores {
		wg.Go(func() {
			tokens[i], errs[i] = FreshToken(t.Context(), s, "myapp", srv.refresh, false, "")
		})
	}
	wg.Wait()

	for i := range stores {
		if errs[i] != nil {
			t.Fatalf("process %d: %v", i, errs[i])
		}
		if tokens[i] != "at-1" {
			t.Errorf("process %d token = %q, want at-1", i, tokens[i])
		}
	}
	if n := srv.calls.Load(); n != 1 {
		t.Errorf("refresh calls = %d, want exactly 1", n)
	}
	e, _ := mustLoad(t, path).Entry("myapp")
	if e.AccessToken != "at-1" || e.RefreshToken != "rt-1" {
		t.Errorf("file holds %q/%q, want at-1/rt-1", e.AccessToken, e.RefreshToken)
	}
}

func TestFreshToken_InvalidGrant(t *testing.T) {
	path := writeOAuthEntry(t, "at-0", "rt-0", time.Now().Add(-time.Minute))
	srv := &rotatingServer{refused: fmt.Errorf("oauth2: %w", ErrInvalidGrant)}
	_, err := FreshToken(t.Context(), mustLoad(t, path), "myapp", srv.refresh, false, "")
	if !errors.Is(err, ErrSignInAgain) {
		t.Fatalf("err = %v, want ErrSignInAgain", err)
	}
	if !errors.Is(err, ErrInvalidGrant) {
		t.Errorf("err = %v, want it to wrap ErrInvalidGrant", err)
	}
	const want = `Your sign-in for "myapp" has ended. Run qf login myapp again.`
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestFreshToken_OtherRefreshErrorIsNotSignInAgain(t *testing.T) {
	path := writeOAuthEntry(t, "at-0", "rt-0", time.Now().Add(-time.Minute))
	srv := &rotatingServer{refused: errors.New("connection refused")}
	_, err := FreshToken(t.Context(), mustLoad(t, path), "myapp", srv.refresh, false, "")
	if err == nil || errors.Is(err, ErrSignInAgain) {
		t.Fatalf("err = %v, want a plain refresh error", err)
	}
}

func TestFreshToken_NoRefreshTokenIsSignInAgain(t *testing.T) {
	path := writeOAuthEntry(t, "at-0", "", time.Now().Add(-time.Minute))
	srv := &rotatingServer{}
	_, err := FreshToken(t.Context(), mustLoad(t, path), "myapp", srv.refresh, false, "")
	if !errors.Is(err, ErrSignInAgain) {
		t.Fatalf("err = %v, want ErrSignInAgain", err)
	}
	if n := srv.calls.Load(); n != 0 {
		t.Errorf("refresh calls = %d, want 0", n)
	}
}

// After a 401 the token is refreshed even though it has not expired yet.
func TestFreshToken_ForceRefreshesTheRejectedToken(t *testing.T) {
	path := writeOAuthEntry(t, "at-0", "rt-0", time.Now().Add(time.Hour))
	srv := &rotatingServer{}
	got, err := FreshToken(t.Context(), mustLoad(t, path), "myapp", srv.refresh, true, "at-0")
	if err != nil {
		t.Fatalf("FreshToken: %v", err)
	}
	if got != "at-1" || srv.calls.Load() != 1 {
		t.Errorf("token = %q after %d calls, want at-1 after 1", got, srv.calls.Load())
	}
}

// After a 401, a token someone else already replaced is not refreshed again:
// the file's newer token is returned as it is.
func TestFreshToken_ForceSkipsWhenAlreadyReplaced(t *testing.T) {
	path := writeOAuthEntry(t, "at-1", "rt-1", time.Now().Add(time.Hour))
	srv := &rotatingServer{gen: 1}
	got, err := FreshToken(t.Context(), mustLoad(t, path), "myapp", srv.refresh, true, "at-0")
	if err != nil {
		t.Fatalf("FreshToken: %v", err)
	}
	if got != "at-1" {
		t.Errorf("token = %q, want at-1", got)
	}
	if n := srv.calls.Load(); n != 0 {
		t.Errorf("refresh calls = %d, want 0", n)
	}
}

// The in-memory view may be stale: a process that loaded an expiring token
// re-reads the file under the lock and uses the token another process saved.
func TestFreshToken_StaleViewUsesTheFile(t *testing.T) {
	path := writeOAuthEntry(t, "at-0", "rt-0", time.Now().Add(10*time.Second))
	stale := mustLoad(t, path)
	fresh := mustLoad(t, path)
	fresh.SetOAuth("myapp", Identifier{AccessToken: "at-1", RefreshToken: "rt-1",
		ExpiresAt: time.Now().Add(time.Hour), Issuer: "https://api.example.test"})
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}
	srv := &rotatingServer{gen: 1}
	got, err := FreshToken(t.Context(), stale, "myapp", srv.refresh, false, "")
	if err != nil {
		t.Fatalf("FreshToken: %v", err)
	}
	if got != "at-1" || srv.calls.Load() != 0 {
		t.Errorf("token = %q after %d calls, want at-1 after 0", got, srv.calls.Load())
	}
}

func TestFreshToken_PastedTokenUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	s := mustLoad(t, path)
	s.Set("myapp", "qf_pasted")
	srv := &rotatingServer{}
	got, err := FreshToken(t.Context(), s, "myapp", srv.refresh, true, "qf_pasted")
	if err != nil {
		t.Fatalf("FreshToken: %v", err)
	}
	if got != "qf_pasted" || srv.calls.Load() != 0 {
		t.Errorf("token = %q after %d calls, want qf_pasted after 0", got, srv.calls.Load())
	}
}

func TestFreshToken_UnknownAlias(t *testing.T) {
	s := mustLoad(t, filepath.Join(t.TempDir(), "config.toml"))
	if _, err := FreshToken(t.Context(), s, "nope", (&rotatingServer{}).refresh, false, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
