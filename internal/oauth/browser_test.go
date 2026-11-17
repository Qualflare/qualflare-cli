package oauth

import (
	"slices"
	"testing"
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
