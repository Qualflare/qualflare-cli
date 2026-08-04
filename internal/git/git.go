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
	// Launch via the absolute path LookPath resolved rather than the bare name, so the
	// binary is located once instead of being re-resolved against $PATH by exec. A CLI
	// must still honour the user's PATH here — hardcoding /usr/bin/git would break
	// Homebrew, asdf, nix, and Windows.
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, gitPath, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
