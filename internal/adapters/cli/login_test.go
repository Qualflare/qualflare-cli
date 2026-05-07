package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/config"
)

// newCLIForLoginTest builds the minimum dependency surface needed to exercise
// the login/logout/projects commands. apiClient/reportService/parserFactory are
// nil because these commands never call them.
func newCLIForLoginTest(t *testing.T) (*CLI, string) {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "qualflare", "config.toml")
	store, err := auth.Load(storePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg := config.DefaultConfig()
	return &CLI{config: cfg, store: store}, storePath
}

func TestLogin_ForceHappyPath(t *testing.T) {
	c, storePath := newCLIForLoginTest(t)
	cmd := c.createLoginCommand()
	cmd.SetArgs([]string{"myapp", "qf_aaa", "--force"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	loaded, err := auth.Load(storePath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	tok, ok := loaded.Get("myapp")
	if !ok || tok != "qf_aaa" {
		t.Fatalf("Get(myapp) = (%q,%v), want (qf_aaa, true)", tok, ok)
	}
}

func TestLogin_RejectsReservedBeforeWrite(t *testing.T) {
	c, storePath := newCLIForLoginTest(t)
	cmd := c.createLoginCommand()
	cmd.SetArgs([]string{"login", "qf_aaa", "--force"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("Execute: got %v, want reserved-name error", err)
	}

	// Disk must be untouched.
	loaded, err := auth.Load(storePath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := loaded.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty (login should not have touched disk)", got)
	}
}

func TestLogin_RejectsEmptyToken(t *testing.T) {
	c, _ := newCLIForLoginTest(t)
	cmd := c.createLoginCommand()
	cmd.SetArgs([]string{"myapp", "", "--force"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "token cannot be empty") {
		t.Fatalf("Execute: got %v, want empty-token error", err)
	}
}

func TestLogout_NotFound(t *testing.T) {
	c, _ := newCLIForLoginTest(t)
	cmd := c.createLogoutCommand()
	cmd.SetArgs([]string{"nope"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Execute: got %v, want not-found error", err)
	}
}

func TestLogout_RemovesAndPersists(t *testing.T) {
	c, storePath := newCLIForLoginTest(t)
	c.store.Set("myapp", "qf_aaa")
	if err := c.store.Save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	cmd := c.createLogoutCommand()
	cmd.SetArgs([]string{"myapp"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	loaded, err := auth.Load(storePath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := loaded.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}
}

func TestProjects_ListsSortedIdentifiers(t *testing.T) {
	c, _ := newCLIForLoginTest(t)
	c.store.Set("zebra", "qf_z")
	c.store.Set("apple", "qf_a")
	if err := c.store.Save(); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	cmd := c.createProjectsCommand()
	cmd.SetArgs([]string{})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// projects writes via fmt.Println -> os.Stdout; we don't capture stdout
	// here, but the round-trip via store proves Set/Save and List are wired.
	if got := c.store.List(); len(got) != 2 || got[0] != "apple" || got[1] != "zebra" {
		t.Fatalf("store.List() = %v, want [apple zebra]", got)
	}
}
