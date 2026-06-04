package git

import (
	"context"
	"os/exec"
	"strings"
)

// DetectBranch returns the current git branch name, or "" when the working
// directory is not inside a git repo, HEAD is detached, or git is unavailable.
func DetectBranch(ctx context.Context) string {
	out := run(ctx, "symbolic-ref", "--short", "-q", "HEAD")
	if out == "HEAD" {
		return ""
	}
	return out
}

// DetectCommit returns the full SHA of the HEAD commit, or "" on any error.
func DetectCommit(ctx context.Context) string {
	return run(ctx, "rev-parse", "HEAD")
}

func run(ctx context.Context, args ...string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
