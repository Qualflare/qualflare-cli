package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newTempStorePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "qualflare", "config.toml")
}

func TestLoad_Missing_ReturnsEmptyStore(t *testing.T) {
	path := newTempStorePath(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}
	if s.Path() != path {
		t.Fatalf("Path() = %q, want %q", s.Path(), path)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := newTempStorePath(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Set("myapp", "qf_aaa")
	s.Set("prod-app", "qf_bbb")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	tok, ok := loaded.Get("myapp")
	if !ok || tok != "qf_aaa" {
		t.Fatalf("Get(myapp) = (%q,%v), want (qf_aaa,true)", tok, ok)
	}
	tok, ok = loaded.Get("prod-app")
	if !ok || tok != "qf_bbb" {
		t.Fatalf("Get(prod-app) = (%q,%v), want (qf_bbb,true)", tok, ok)
	}
	got := loaded.List()
	if len(got) != 2 || got[0] != "myapp" || got[1] != "prod-app" {
		t.Fatalf("List() = %v, want [myapp prod-app]", got)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits not meaningful on Windows")
	}
	path := newTempStorePath(t)
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Set("myapp", "qf_aaa")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != fileMode {
		t.Fatalf("file mode = %o, want %o", mode, fileMode)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if mode := di.Mode().Perm(); mode != dirMode {
		t.Fatalf("dir mode = %o, want %o", mode, dirMode)
	}
}

func TestDelete_NotFound(t *testing.T) {
	s, err := Load(newTempStorePath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := s.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete: got %v, want ErrNotFound", err)
	}
}

func TestDelete_Removes(t *testing.T) {
	s, err := Load(newTempStorePath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Set("myapp", "qf_aaa")
	if err := s.Delete("myapp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get("myapp"); ok {
		t.Fatalf("Get after delete: still present")
	}
}

func TestSave_AtomicCleansTempOnRenameFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-onto-directory semantics differ on Windows")
	}
	dir := t.TempDir()
	configDir := filepath.Join(dir, "qualflare")
	if err := os.MkdirAll(configDir, dirMode); err != nil {
		t.Fatalf("setup dir: %v", err)
	}
	// Force rename failure by making the destination a non-empty directory.
	target := filepath.Join(configDir, "config.toml")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	_, err := Load(target) // Load on a directory should error
	if err == nil {
		t.Logf("Load on directory unexpectedly succeeded; constructing store manually")
	}
	// We deliberately construct the store directly because Load wraps the read error.
	s := &Store{path: target, data: fileFormat{SchemaVersion: schemaVersion, Identifiers: map[string]Identifier{}}}
	s.Set("myapp", "qf_aaa")
	if err := s.Save(); err == nil {
		t.Fatal("Save: expected error when target is a non-empty directory")
	}

	// No leftover .config-*.toml in configDir.
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "config.toml" && (len(e.Name()) >= 8 && e.Name()[:8] == ".config-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func TestLoad_CorruptToml_Errors(t *testing.T) {
	path := newTempStorePath(t)
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("this is = not valid TOML ===\n[\n"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load: expected error on corrupt toml")
	}
}
