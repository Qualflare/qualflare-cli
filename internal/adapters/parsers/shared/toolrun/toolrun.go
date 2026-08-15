// Package toolrun runs a short-lived external helper tool synchronously and
// captures its stdout — for parsers that shell out to extract data (e.g.
// Apple's xcresulttool). This is deliberately not internal/adapters/runner:
// that package tees a wrapped *test* command's live output to the terminal
// for `qf run` and reports the wrapped command's own exit code as the report
// outcome (nonzero there means "tests failed", not "broken"). Here a
// nonzero exit always means the extraction tool itself failed, there is no
// tee and no interactive stdin, and the caller gets stdout back directly.
package toolrun

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"qualflare-cli/internal/adapters/parsers/base"
)

const maxStderrExcerpt = 4000

// Run executes name with args and returns its stdout. A failure to start the
// process (name not on PATH), a nonzero exit, or ctx cancellation is
// returned as an error, with a bounded excerpt of stderr folded in for
// diagnosability.
func Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, base.TruncateString(stderr.String(), maxStderrExcerpt))
	}
	return stdout.Bytes(), nil
}
