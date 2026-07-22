package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExpandGlobs (BUG-28) covers the previously-nonexistent glob support: a
// pattern expands to its matches, a literal passes through, and a pattern that
// matches nothing is a loud error (not a silent empty upload).
func TestExpandGlobs(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.xml", "b.xml"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Literal args pass through untouched.
	out, err := expandGlobs([]string{"literal.xml", "other.json"})
	if err != nil || len(out) != 2 || out[0] != "literal.xml" {
		t.Fatalf("literal passthrough failed: %v %v", out, err)
	}

	// A glob expands to its matches.
	out, err = expandGlobs([]string{filepath.Join(dir, "*.xml")})
	if err != nil {
		t.Fatalf("glob expand errored: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("glob matched %d files, want 2: %v", len(out), out)
	}

	// A pattern that matches nothing is an error.
	if _, err := expandGlobs([]string{filepath.Join(dir, "*.json")}); err == nil {
		t.Fatal("a glob matching nothing must error")
	}
}
