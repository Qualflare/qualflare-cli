package detox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

func TestResolveRunDir(t *testing.T) {
	t.Run("a directory holding run directories resolves to the newest", func(t *testing.T) {
		root := t.TempDir()
		older := filepath.Join(root, "ios.sim.debug.2026-09-01 10-00-00Z")
		newer := filepath.Join(root, "ios.sim.debug.2026-09-27 10-00-00Z")
		for _, d := range []string{older, newer} {
			if err := os.MkdirAll(filepath.Join(d, "✓ a test"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		// Make the intended winner unambiguously newer, since a temp dir's
		// entries can share a timestamp to the second.
		future := time.Now().Add(time.Hour)
		if err := os.Chtimes(newer, future, future); err != nil {
			t.Fatal(err)
		}

		got, err := ResolveRunDir(root)
		if err != nil {
			t.Fatalf("ResolveRunDir: %v", err)
		}
		if got != newer {
			t.Errorf("got %q, want the newest run %q", got, newer)
		}
	})

	t.Run("a run directory is used as given", func(t *testing.T) {
		run := t.TempDir()
		if err := os.MkdirAll(filepath.Join(run, "✗ a failing test"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveRunDir(run)
		if err != nil {
			t.Fatalf("ResolveRunDir: %v", err)
		}
		if got != run {
			t.Errorf("got %q, want the directory as given %q", got, run)
		}
	})

	t.Run("a missing directory is an error the user can act on", func(t *testing.T) {
		_, err := ResolveRunDir(filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Fatal("want an error for a directory that does not exist")
		}
	})

	t.Run("an empty directory is an error, not an empty success", func(t *testing.T) {
		if _, err := ResolveRunDir(t.TempDir()); err == nil {
			t.Fatal("want an error: an empty directory means the path is wrong, and silently attaching nothing hides that")
		}
	})

	t.Run("a run directory with only unglyphed (skipped) tests resolves correctly", func(t *testing.T) {
		run := t.TempDir()
		if err := os.MkdirAll(filepath.Join(run, "a skipped test"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveRunDir(run)
		if err != nil {
			t.Fatalf("ResolveRunDir: %v", err)
		}
		if got != run {
			t.Errorf("got %q, want the directory as given %q", got, run)
		}
	})

	t.Run("a root directory with a symlink to a run directory finds the run", func(t *testing.T) {
		root := t.TempDir()
		actualRun := t.TempDir()
		if err := os.MkdirAll(filepath.Join(actualRun, "✓ a test"), 0o755); err != nil {
			t.Fatal(err)
		}
		// Create a symlink in root pointing to the actual run
		symlinkPath := filepath.Join(root, "ios.sim.debug.2026-09-27 10-00-00Z")
		if err := os.Symlink(actualRun, symlinkPath); err != nil {
			t.Fatal(err)
		}

		got, err := ResolveRunDir(root)
		if err != nil {
			t.Fatalf("ResolveRunDir: %v", err)
		}
		if got != symlinkPath {
			t.Errorf("got %q, want the symlink path %q", got, symlinkPath)
		}
	})
}

func TestAttach(t *testing.T) {
	run := t.TempDir()

	// Two invocations of one flaky test, plus a passing test, plus a directory
	// belonging to no case in the report.
	write := func(dir, file string, content string) {
		if err := os.MkdirAll(filepath.Join(run, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(run, dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("✗ Login should sign in", "testFnFailure.png", "png-bytes")
	write("✗ Login should sign in", "device.log", "log-bytes")
	write("✓ Login should sign in (2)", "afterEach.png", "png-bytes")
	write("✓ Checkout should charge", "test.mp4", "mp4-bytes")
	write("✓ A test nobody reported", "orphan.png", "png-bytes")

	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Name: "Login should sign in", Status: domain.StatusPassed},
		{Name: "Checkout should charge", Status: domain.StatusPassed},
	}}}}

	matched, unmatched, unreadable, err := Attach(launch, run)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(unreadable) != 0 {
		t.Errorf("unreadable = %v, want none", unreadable)
	}
	if matched != 2 {
		t.Errorf("matched %d cases, want 2", matched)
	}
	if len(unmatched) != 1 || unmatched[0] != "✓ A test nobody reported" {
		t.Errorf("unmatched = %v, want the one orphan directory", unmatched)
	}

	login := launch.Suites[0].Cases[0]
	if len(login.Attachments) != 3 {
		t.Fatalf("the flaky test should carry all three files across both invocations, got %d", len(login.Attachments))
	}
	// A retried test's later invocations are distinguishable, since they attach
	// to the case rather than to an attempt.
	names := make([]string, 0, len(login.Attachments))
	for _, a := range login.Attachments {
		names = append(names, a.Name)
	}
	if !slices.Contains(names, "afterEach.png (attempt 2)") {
		t.Errorf("invocation 2's file should name its attempt, got %v", names)
	}

	// Kinds decide the upload route AND whether it uploads by default.
	byName := map[string]domain.Attachment{}
	for _, a := range login.Attachments {
		byName[a.Name] = a
	}
	if got := byName["testFnFailure.png"]; got.ArtifactKind != domain.ArtifactKindImage {
		t.Errorf("a screenshot should be an image (uploads by default), got %q", got.ArtifactKind)
	}
	if got := byName["device.log"]; got.ArtifactKind != domain.ArtifactKindTrace {
		t.Errorf("a device log should be a trace (opt-in: it may carry customer data), got %q", got.ArtifactKind)
	}
	checkout := launch.Suites[0].Cases[1]
	if checkout.Attachments[0].ArtifactKind != domain.ArtifactKindVideo {
		t.Errorf("a video should be a video (opt-in: it is the largest thing in a report), got %q", checkout.Attachments[0].ArtifactKind)
	}
	if !filepath.IsAbs(checkout.Attachments[0].LocalPath) {
		t.Errorf("LocalPath must be absolute for report_service to resolve it, got %q", checkout.Attachments[0].LocalPath)
	}
}

func TestAttachMatchesRegardlessOfReportedStatus(t *testing.T) {
	// The report says passed; Detox wrote the directory when the test failed on
	// its first invocation. Matching must try every status glyph rather than
	// only the one the case ended on.
	run := t.TempDir()
	if err := os.MkdirAll(filepath.Join(run, "✗ Flaky test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "✗ Flaky test", "shot.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Name: "Flaky test", Status: domain.StatusPassed},
	}}}}
	if _, _, _, err := Attach(launch, run); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(launch.Suites[0].Cases[0].Attachments) != 1 {
		t.Error("a case that ended green must still pick up the directory from its failed invocation")
	}
}

func TestAttachToleratesARunWithNoArtifacts(t *testing.T) {
	// A run where every test passed and screenshots/video were both off
	// writes no per-test directories at all. ResolveRunDir treats that as an
	// error (Task 3's rule: a wrong path looks the same), but Attach must
	// tell the two apart and no-op instead of failing the whole upload.
	run := t.TempDir()
	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Name: "A passing test", Status: domain.StatusPassed},
	}}}}

	matched, unmatched, unreadable, err := Attach(launch, run)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if matched != 0 || len(unmatched) != 0 || len(unreadable) != 0 {
		t.Errorf("matched=%d unmatched=%v unreadable=%v, want 0 and none: an artifact-free run is a no-op, not a partial failure", matched, unmatched, unreadable)
	}
}

func TestAttachReportsAMatchedButUnreadableDirectory(t *testing.T) {
	// A directory that matches a case by name but can't be read (permission
	// denied, say) must not vanish from both matched and unreadable counts —
	// and must be reported separately from unmatched, since the two are
	// different problems (a naming bug vs. a mode bit) with different fixes.
	run := t.TempDir()
	caseDir := filepath.Join(run, "✓ Login should sign in")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(caseDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(caseDir, 0o755)
	})

	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Name: "Login should sign in", Status: domain.StatusPassed},
	}}}}

	matched, unmatched, unreadable, err := Attach(launch, run)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if matched != 0 {
		t.Errorf("matched = %d, want 0: the directory couldn't be read, so it didn't attach anything", matched)
	}
	if len(unmatched) != 0 {
		t.Errorf("unmatched = %v, want none: this directory matched a case by name, it just couldn't be read", unmatched)
	}
	if len(unreadable) != 1 || unreadable[0] != "✓ Login should sign in" {
		t.Errorf("unreadable = %v, want the unreadable directory reported there, not silently dropped", unreadable)
	}
}
