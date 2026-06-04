package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetect_insideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "main", dir)
	run("-C", dir, "commit", "--allow-empty", "-m", "init")

	// Change working dir to the temp repo so the helpers pick it up.
	orig, _ := os.Getwd()
	defer os.Chdir(orig) //nolint:errcheck
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	branch := DetectBranch(ctx)
	if branch == "" {
		t.Error("DetectBranch returned empty string inside a repo")
	}

	commit := DetectCommit(ctx)
	if commit == "" {
		t.Error("DetectCommit returned empty string inside a repo")
	}
	if len(commit) != 40 {
		t.Errorf("DetectCommit = %q; want 40-char SHA", commit)
	}
}

func TestDetect_outsideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Use a temp dir that is not a git repo.
	dir := t.TempDir()
	// Write a sentinel file so dir is definitely not inside any parent repo.
	os.WriteFile(filepath.Join(dir, ".git-no"), nil, 0o644) //nolint:errcheck

	orig, _ := os.Getwd()
	defer os.Chdir(orig) //nolint:errcheck
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if got := DetectBranch(ctx); got != "" {
		t.Errorf("DetectBranch = %q outside repo; want empty", got)
	}
	if got := DetectCommit(ctx); got != "" {
		t.Errorf("DetectCommit = %q outside repo; want empty", got)
	}
}
