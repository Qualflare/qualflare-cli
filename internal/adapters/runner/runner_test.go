package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRun_CapturesCombinedOutputInWriteOrder(t *testing.T) {
	var tee bytes.Buffer
	result, err := Run(context.Background(), &tee, "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(string(result.Output), "out") || !strings.Contains(string(result.Output), "err") {
		t.Errorf("expected captured output to contain both stdout and stderr, got %q", result.Output)
	}
}

func TestRun_TeesToWriter(t *testing.T) {
	var tee bytes.Buffer
	_, err := Run(context.Background(), &tee, "sh", "-c", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(tee.String(), "hello") {
		t.Errorf("expected the tee writer to receive the same output, got %q", tee.String())
	}
}

func TestRun_NonZeroExitCodeIsNotAGoError(t *testing.T) {
	var tee bytes.Buffer
	result, err := Run(context.Background(), &tee, "sh", "-c", "exit 3")
	if err != nil {
		t.Fatalf("a nonzero exit from the wrapped command must not be a Go error, got: %v", err)
	}
	if result.ExitCode != 3 {
		t.Errorf("expected exit code 3, got %d", result.ExitCode)
	}
}

func TestRun_CommandNotFoundIsAGoError(t *testing.T) {
	var tee bytes.Buffer
	_, err := Run(context.Background(), &tee, "definitely-not-a-real-command-xyz")
	if err == nil {
		t.Fatal("expected an error when the command cannot be found")
	}
}

func TestRun_MeasuresDuration(t *testing.T) {
	var tee bytes.Buffer
	result, err := Run(context.Background(), &tee, "sh", "-c", "true")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Duration < 0 {
		t.Errorf("expected a non-negative duration, got %v", result.Duration)
	}
}
