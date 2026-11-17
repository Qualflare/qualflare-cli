package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

// The qf-init skill in the Claude Code plugin matches this hint by its prefix.
const noProjectsHint = "No projects configured. Run 'qf login <identifier>' to sign in."

func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	f()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestNoProjectsHint(t *testing.T) {
	c, _ := newCLIForLoginTest(t)

	t.Run("root command", func(t *testing.T) {
		root := c.CreateRootCommand()
		root.SetArgs(nil)
		out := captureStdout(t, func() {
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
		})
		if strings.TrimSpace(out) != noProjectsHint {
			t.Errorf("got %q, want %q", out, noProjectsHint)
		}
	})

	t.Run("projects command", func(t *testing.T) {
		cmd := c.createProjectsCommand()
		cmd.SetArgs(nil)
		out := captureStdout(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
		})
		if strings.TrimSpace(out) != noProjectsHint {
			t.Errorf("got %q, want %q", out, noProjectsHint)
		}
	})
}

func TestRootHelpDescribesBrowserLogin(t *testing.T) {
	c, _ := newCLIForLoginTest(t)
	long := c.CreateRootCommand().Long
	if !strings.Contains(long, "Sign in to a project (qf login <id>)") {
		t.Errorf("root help does not describe browser sign-in:\n%s", long)
	}
	if strings.Contains(long, "<id> <token>") {
		t.Errorf("root help still shows the token argument:\n%s", long)
	}
}
