package oauth

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestBrowserCommand(t *testing.T) {
	const u = "https://app.example.test/oauth/authorize?a=1&b=2"
	cases := []struct {
		goos, name string
		args       []string
	}{
		{"darwin", "open", []string{u}},
		{"linux", "xdg-open", []string{u}},
		{"freebsd", "xdg-open", []string{u}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", u}},
	}
	for _, tc := range cases {
		name, args, err := browserCommand(tc.goos, u)
		if err != nil {
			t.Fatalf("%s: %v", tc.goos, err)
		}
		if name != tc.name || !slices.Equal(args, tc.args) {
			t.Errorf("%s: got %s %q, want %s %q", tc.goos, name, args, tc.name, tc.args)
		}
	}
}

func TestBrowserCommand_RejectsNonWebURL(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "-a Calculator", ""} {
		if _, _, err := browserCommand("darwin", u); err == nil {
			t.Errorf("browserCommand(%q) = nil error, want refusal", u)
		}
	}
}

// The opener outlives the sign-in that started it: xdg-open, for one, hands
// the URL on and exits in its own time, and killing it when qf login returns
// could leave no browser open. The helper process below plays the opener.
func TestOpenBrowser_OpenerOutlivesTheSignIn(t *testing.T) {
	done := filepath.Join(t.TempDir(), "opened")
	t.Setenv("QF_TEST_OPENER_DONE", done)
	orig := browserCommandFor
	t.Cleanup(func() { browserCommandFor = orig })
	browserCommandFor = func(_, rawURL string) (string, []string, error) {
		return os.Args[0], []string{"-test.run=^TestOpenerHelperProcess$", "--", rawURL}, nil
	}

	if err := OpenBrowser("https://app.example.test/oauth/authorize"); err != nil {
		t.Fatalf("OpenBrowser: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(done); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the opener was stopped before it finished")
}

// TestOpenerHelperProcess is the opener TestOpenBrowser_OpenerOutlivesTheSignIn
// starts; run directly, it does nothing.
func TestOpenerHelperProcess(t *testing.T) {
	done := os.Getenv("QF_TEST_OPENER_DONE")
	if done == "" {
		t.Skip("helper process")
	}
	time.Sleep(300 * time.Millisecond)
	if err := os.WriteFile(done, nil, 0o600); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
