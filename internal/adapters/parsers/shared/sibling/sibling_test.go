package sibling

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead_ReturnsContentWhenSiblingExists(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "report.xml")
	if err := os.WriteFile(primary, []byte("<x/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	sib := filepath.Join(dir, "commands.json")
	if err := os.WriteFile(sib, []byte(`[{"command":"tapOn"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	content, ok, err := Read(primary, "commands.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if string(content) != `[{"command":"tapOn"}]` {
		t.Errorf("content = %q, want the sibling file's bytes", content)
	}
}

func TestRead_MissingSiblingIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "report.xml")
	if err := os.WriteFile(primary, []byte("<x/>"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, ok, err := Read(primary, "commands.json")
	if err != nil {
		t.Fatalf("unexpected error: %v, want nil (missing sibling is the expected common case)", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
	if content != nil {
		t.Errorf("content = %q, want nil", content)
	}
}

func TestRead_OtherErrorsAreReturned(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "report.xml")
	if err := os.WriteFile(primary, []byte("<x/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A sibling name that is itself a directory triggers a real (non-NotExist) error.
	if err := os.Mkdir(filepath.Join(dir, "commands.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, ok, err := Read(primary, "commands.json")
	if err == nil {
		t.Fatal("expected a real error for a directory sibling, got nil")
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

func TestRead_LooksInThePrimaryFilesDirectoryNotTheCallersCWD(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(sub, "report.xml")
	if err := os.WriteFile(primary, []byte("<x/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	sib := filepath.Join(sub, "commands.json")
	if err := os.WriteFile(sib, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	// A same-named file in dir (not sub) must NOT be picked up.
	if err := os.WriteFile(filepath.Join(dir, "commands.json"), []byte(`"wrong"`), 0o600); err != nil {
		t.Fatal(err)
	}

	content, ok, err := Read(primary, "commands.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if string(content) != "[]" {
		t.Errorf("content = %q, want the sibling from primary's own directory, not the parent's", content)
	}
}
