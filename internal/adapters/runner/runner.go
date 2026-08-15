// Package runner wraps execution of an external test command for `qf run`,
// tee-ing its combined stdout/stderr live to the terminal while also
// capturing it for a log-step extractor to parse. Modeled on
// internal/git/git.go's exec.LookPath + exec.CommandContext style.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Result is the outcome of running a wrapped command.
type Result struct {
	// Output is the interleaved stdout+stderr, in the order the process wrote it.
	Output   []byte
	ExitCode int
	Duration time.Duration
}

// syncBuffer is a mutex-guarded bytes.Buffer. os/exec serializes writes when
// Stdout and Stderr are set to the same comparable writer value (documented
// behavior: "If Stdout and Stderr are the same writer... at most one
// goroutine at a time will call Write"), but bytes.Buffer itself is not
// safe for concurrent use in general — this is defense in depth, not a
// workaround for a known race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())
	return out
}

// Run executes name/args, tee-ing combined stdout+stderr live to teeOut
// while also collecting the same bytes into Result.Output. stdin is
// connected to the CLI's own stdin, so an interactive wrapped command still
// works. A nonzero exit from the wrapped command ("tests failed") is
// reported via Result.ExitCode, not a Go error — that is the expected
// common case, not a failure to run the command at all.
func Run(ctx context.Context, teeOut io.Writer, name string, args ...string) (*Result, error) {
	binPath, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("command not found: %s: %w", name, err)
	}

	buf := &syncBuffer{}
	mw := io.MultiWriter(teeOut, buf)

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Stdout = mw
	cmd.Stderr = mw // same writer value as Stdout, so os/exec serializes writes
	cmd.Stdin = os.Stdin

	start := time.Now()
	runErr := cmd.Run()
	result := &Result{Output: buf.Bytes(), Duration: time.Since(start)}

	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		result.ExitCode = 0
	case errors.As(runErr, &exitErr):
		result.ExitCode = exitErr.ExitCode()
	default:
		return result, fmt.Errorf("failed to run command: %w", runErr)
	}
	return result, nil
}
