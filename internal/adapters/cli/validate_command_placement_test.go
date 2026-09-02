package cli

import (
	"path/filepath"
	"testing"

	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/config"
)

// newRootCLI builds a root command over an empty, throwaway credential store.
// CreateRootCommand calls store.List(), so the store cannot be nil here the way
// it can in the other tests in this package.
func newRootCLI(t *testing.T, identifiers map[string]string) *CLI {
	t.Helper()

	store, err := auth.Load(filepath.Join(t.TempDir(), "credentials.json"))
	if err != nil {
		t.Fatalf("auth.Load: %v", err)
	}
	for id, token := range identifiers {
		store.Set(id, token)
	}

	return NewCLI(nil, config.DefaultConfig(), nil, nil, store)
}

// The regression test for `qf validate results.xml` failing with
//
//	Error: no identifier "validate" configured. Run 'qf login validate <token>'
//
// validate was registered only inside the identifier-scoped subtree, so cobra
// read the subcommand name as a project name. It parses files locally and never
// calls the API, so there was never anything for credentials to authorise — and
// all ten `make validate-*` targets, which invoke the flat form, had been broken
// for as long as that was true.
//
// Asserted through cobra's own resolution rather than by inspecting the command
// list, because resolution is what actually failed: the command existed, it was
// just unreachable by that path.
func TestValidateResolvesWithoutAProject(t *testing.T) {
	root := newRootCLI(t, nil).CreateRootCommand()

	cmd, _, err := root.Find([]string{"validate", "results.xml"})
	if err != nil {
		t.Fatalf("root.Find(validate): %v", err)
	}
	if cmd.Name() != "validate" {
		t.Fatalf("`qf validate results.xml` resolved to %q, not the validate command — "+
			"cobra is reading the subcommand name as a project identifier again", cmd.Name())
	}
}

// A saved project must not shadow it either: with identifiers present, the flat
// form still has to resolve. This is the shape the bug actually appeared in,
// since anyone hitting it had projects configured.
func TestValidateResolvesWithSavedProjects(t *testing.T) {
	root := newRootCLI(t, map[string]string{"acme": "qf_token"}).CreateRootCommand()

	cmd, _, err := root.Find([]string{"validate", "results.xml"})
	if err != nil {
		t.Fatalf("root.Find(validate): %v", err)
	}
	if cmd.Name() != "validate" {
		t.Errorf("`qf validate` resolved to %q with a project saved", cmd.Name())
	}
}

// The documented `qf <id> validate` form must keep working — the fix registers
// the command in both places rather than moving it, and this is what stops a
// later tidy-up from "de-duplicating" it back into one.
//
// Registering it twice is safe only because createValidateCommand returns a
// fresh *cobra.Command per call; cobra mutates the parent pointer on AddCommand,
// so a shared instance would silently reparent. That is the same hazard
// buildAuthedSubtree's own comment warns about.
func TestValidateStillResolvesUnderAProject(t *testing.T) {
	root := newRootCLI(t, map[string]string{"acme": "qf_token"}).CreateRootCommand()

	cmd, _, err := root.Find([]string{"acme", "validate", "results.xml"})
	if err != nil {
		t.Fatalf("root.Find(acme validate): %v", err)
	}
	if cmd.Name() != "validate" {
		t.Errorf("`qf acme validate` resolved to %q", cmd.Name())
	}
	if parent := cmd.Parent(); parent == nil || parent.Name() != "acme" {
		t.Errorf("the project-scoped validate has parent %v, want acme — "+
			"a shared *cobra.Command instance would reparent like this", parent)
	}
}

// The two registrations must be distinct instances, which is the invariant the
// whole double-registration rests on.
func TestValidateInstancesAreDistinct(t *testing.T) {
	root := newRootCLI(t, map[string]string{"acme": "qf_token"}).CreateRootCommand()

	flat, _, err := root.Find([]string{"validate"})
	if err != nil {
		t.Fatalf("find flat validate: %v", err)
	}
	scoped, _, err := root.Find([]string{"acme", "validate"})
	if err != nil {
		t.Fatalf("find scoped validate: %v", err)
	}
	if flat == scoped {
		t.Error("both registrations share one *cobra.Command; cobra mutates the " +
			"parent pointer on AddCommand, so one of the two paths will break")
	}
}
