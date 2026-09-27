package artifacts

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestAttachFromDirectoryRejectsUnsupportedFormats(t *testing.T) {
	// Silently ignoring the flag would be indistinguishable from a matching
	// bug, so an unsupported format is a hard error that names the alternatives.
	err := AttachFromDirectory(&domain.Launch{}, domain.FrameworkJUnit, t.TempDir(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("want an error for a format with no artifact-directory support")
	}
	if !strings.Contains(err.Error(), "detox") {
		t.Errorf("the error should name the formats that DO support it, got %q", err)
	}
}

// The case that would otherwise reject every real Detox upload: Detox writes a
// Jest report, so detection calls it jest unless --format detox is passed.
func TestAttachFromDirectoryAcceptsJest(t *testing.T) {
	run := t.TempDir()
	if err := os.MkdirAll(filepath.Join(run, "✓ a test"), 0o755); err != nil {
		t.Fatal(err)
	}
	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{Name: "a test"}}}}}
	if err := AttachFromDirectory(launch, domain.FrameworkJest, run, &bytes.Buffer{}); err != nil {
		t.Fatalf("a Detox report detected as jest must be accepted: %v", err)
	}
}

func TestAttachFromDirectoryWarnsAboutUnmatchedDirectories(t *testing.T) {
	run := t.TempDir()
	if err := os.MkdirAll(filepath.Join(run, "✓ nobody reported this"), 0o755); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{Name: "a different test"}}}}}

	if err := AttachFromDirectory(launch, domain.FrameworkDetox, run, &warn); err != nil {
		t.Fatalf("AttachFromDirectory: %v", err)
	}
	if !strings.Contains(warn.String(), "nobody reported this") {
		t.Errorf("an unmatched directory must be reported, got %q", warn.String())
	}
	if !strings.Contains(warn.String(), "matched no cases") {
		t.Errorf("a scan that matched nothing deserves its own warning, got %q", warn.String())
	}
}
