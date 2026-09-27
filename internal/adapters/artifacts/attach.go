// Package artifacts attaches files a framework left on disk to the cases they
// belong to.
//
// It runs after parsing and before the dry-run return in ProcessTestResults,
// so `--dry-run` shows what would be attached. It cannot run in a reporter:
// Detox finalises video in detox.cleanup(), which its Jest integration calls
// from Jest's globalTeardown — and globalTeardown runs AFTER every reporter's
// onRunComplete, so a reporter-side scan would systematically miss videos.
package artifacts

import (
	"fmt"
	"io"

	"qualflare-cli/internal/adapters/artifacts/detox"
	"qualflare-cli/internal/core/domain"
)

// detoxLayout lists the formats whose artifact layout is known. The layout is
// inferred from the REPORT's format, never sniffed from the directory: guessing
// what wrote a directory fails in exactly the confusing cases.
// Both jest and detox map to the Detox layout. A Detox report IS a Jest
// report — Detox drives Jest — so unless the user passes --format detox,
// content detection identifies a real Detox upload as jest. Accepting only
// detox here would reject exactly the input this flag exists for.
var detoxLayout = map[domain.Framework]bool{
	domain.FrameworkJest:  true,
	domain.FrameworkDetox: true,
}

// AttachIfRequested attaches on-disk artifacts when a directory was configured,
// and does nothing when it was not. Both collect paths call this rather than
// repeating the check, so the choice of framework cannot drift between them.
// The framework is the REPORT's resolved format, not something sniffed from the
// directory: resolveFramework returns "" when --format is absent (the documented
// usage), but launch.Framework is already resolved by the time we reach the
// attachment step.
func AttachIfRequested(launch *domain.Launch, dir string, warn io.Writer) error {
	if dir == "" {
		return nil
	}
	return AttachFromDirectory(launch, domain.Framework(launch.Framework), dir, warn)
}

// AttachFromDirectory matches the artifacts a framework left in dir to the
// cases in launch and appends them as attachments. framework is the REPORT's
// resolved format, not something sniffed from dir's contents: an unsupported
// format is a hard error rather than a silent no-op, because silently
// ignoring the flag would be indistinguishable from a matching bug.
func AttachFromDirectory(launch *domain.Launch, framework domain.Framework, dir string, warn io.Writer) error {
	if !detoxLayout[framework] {
		return fmt.Errorf("--artifacts-dir is not supported for %s reports; it currently applies to detox (and jest, since a Detox report is a Jest report)", framework)
	}

	matched, unmatched, unreadable, err := detox.Attach(launch, dir)
	if err != nil {
		return err
	}

	if len(unmatched) > 0 {
		shown := unmatched
		if len(shown) > 5 {
			shown = shown[:5]
		}
		fmt.Fprintf(warn, "warning: %d artifact directory/directories matched no case: %v\n", len(unmatched), shown)
	}
	// Reported separately from unmatched: this directory WAS the right one for
	// a case — the name matched — but something (permission denied, most
	// likely) stopped it from being read. Folding it into "matched no case"
	// sends the user hunting a naming bug instead of a mode bit.
	if len(unreadable) > 0 {
		shown := unreadable
		if len(shown) > 5 {
			shown = shown[:5]
		}
		fmt.Fprintf(warn, "warning: %d artifact directory/directories matched a case but could not be read: %v\n", len(unreadable), shown)
	}
	if matched == 0 {
		fmt.Fprintf(warn, "warning: --artifacts-dir %s matched no cases at all — check that it is the run you just uploaded\n", dir)
	}
	return nil
}
