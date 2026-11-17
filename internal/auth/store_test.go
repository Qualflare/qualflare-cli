package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

// TestLoad_LoosePermsStillLoads (SEC-05) confirms Load warns but does NOT fail on
// a group/world-accessible credential file — failing would lock the user out.
func TestLoad_LoosePermsStillLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("schema_version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load must not fail on a 0644 file (warn only): %v", err)
	}
}

const pastedFixture = `schema_version = 1

[identifiers]
  [identifiers.alpha]
    token = "qf_alpha_token"
  [identifiers.beta]
    token = "qf_beta_token"
`

func TestStore_PastedTokensRoundTripUnchanged(t *testing.T) {
	path := newTempStorePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(pastedFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Re-saving with no changes must reproduce the file byte for byte.
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != pastedFixture {
		t.Fatalf("unchanged save differs:\n%s\nwant:\n%s", got, pastedFixture)
	}

	s.SetOAuth("gamma", Identifier{
		Kind: KindOAuth, AccessToken: "at", RefreshToken: "rt",
		ExpiresAt: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC), Issuer: "https://api.qualflare.com",
	})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ = os.ReadFile(path)
	for _, block := range []string{
		"  [identifiers.alpha]\n    token = \"qf_alpha_token\"\n",
		"  [identifiers.beta]\n    token = \"qf_beta_token\"\n",
	} {
		if !strings.Contains(string(got), block) {
			t.Errorf("pasted entry changed; missing block %q in:\n%s", block, got)
		}
	}
	if n := strings.Count(string(got), "expires_at"); n != 1 {
		t.Errorf("expires_at appears %d times, want 1 (only the OAuth entry):\n%s", n, got)
	}
	if !strings.Contains(string(got), "schema_version = 1") {
		t.Errorf("schema_version changed:\n%s", got)
	}
}

func TestStore_OAuthEntryRoundTrip(t *testing.T) {
	path := newTempStorePath(t)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Identifier{
		Kind: KindOAuth, AccessToken: "at", RefreshToken: "rt",
		ExpiresAt: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC), Issuer: "https://api.qualflare.com",
	}
	s.SetOAuth("proj", want)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Entry("proj")
	if !ok {
		t.Fatal("Entry missing")
	}
	if got.Kind != want.Kind || got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken ||
		got.Issuer != want.Issuer || !got.ExpiresAt.Equal(want.ExpiresAt) || got.Token != "" {
		t.Fatalf("Entry = %+v, want %+v", got, want)
	}
	if tok, _ := s2.Get("proj"); tok != "at" {
		t.Fatalf("Get = %q, want access token", tok)
	}
	if _, ok := s2.Entry("nope"); ok {
		t.Fatal("Entry for unknown id reported ok")
	}
}

// SetOAuth marks the entry as OAuth itself, so a caller that forgets Kind still
// gets an entry whose Get returns the access token.
func TestStore_SetOAuthSetsKind(t *testing.T) {
	s, err := Load(newTempStorePath(t))
	if err != nil {
		t.Fatal(err)
	}
	s.SetOAuth("proj", Identifier{AccessToken: "at", RefreshToken: "rt", Token: "stale"})
	got, _ := s.Entry("proj")
	if got.Kind != KindOAuth {
		t.Fatalf("Kind = %q, want %q", got.Kind, KindOAuth)
	}
	if tok, _ := s.Get("proj"); tok != "at" {
		t.Fatalf("Get = %q, want the access token", tok)
	}
}
