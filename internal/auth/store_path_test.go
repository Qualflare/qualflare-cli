package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DefaultPath is what main() uses to find the credential store, so the layout it
// produces is a compatibility surface: qualflare/config.toml under the platform's
// user-config directory.
func TestDefaultPath(t *testing.T) {
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() = %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("DefaultPath() = %q, want an absolute path", got)
	}
	wantSuffix := filepath.Join(configDirName, configFileName)
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("DefaultPath() = %q, want it to end with %q", got, wantSuffix)
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user config dir on this platform")
	}
	if want := filepath.Join(dir, configDirName, configFileName); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestHas(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}

	if s.Has("nope") {
		t.Error("Has() on an empty store = true, want false")
	}
	s.Set("myapp", "qf_token")
	if !s.Has("myapp") {
		t.Error("Has(\"myapp\") after Set = false, want true")
	}
	// An empty token still counts as present — Has reports membership, not validity.
	s.Set("blank", "")
	if !s.Has("blank") {
		t.Error("Has() must report an identifier with an empty token as present")
	}
	if err := s.Delete("myapp"); err != nil {
		t.Fatal(err)
	}
	if s.Has("myapp") {
		t.Error("Has() after Delete = true, want false")
	}
}

// A store built by hand (rather than via Load) has no path; Save must say so instead of
// writing somewhere unexpected.
func TestSave_WithoutPath(t *testing.T) {
	s := &Store{}
	err := s.Save()
	if err == nil {
		t.Fatal("Save() with no path = nil, want an error")
	}
	if !strings.Contains(err.Error(), "no path") {
		t.Errorf("Save() = %q, want it to explain the missing path", err)
	}
}
