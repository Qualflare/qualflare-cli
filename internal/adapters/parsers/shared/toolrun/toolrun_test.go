package toolrun

import (
	"context"
	"strings"
	"testing"
)

func TestRun_ReturnsStdout(t *testing.T) {
	out, err := Run(context.Background(), "echo", "-n", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != "hello" {
		t.Errorf("out = %q, want %q", out, "hello")
	}
}

func TestRun_NonzeroExitReturnsError(t *testing.T) {
	_, err := Run(context.Background(), "sh", "-c", "echo boom-detail >&2; exit 3")
	if err == nil {
		t.Fatal("expected an error for a nonzero exit")
	}
	if !strings.Contains(err.Error(), "boom-detail") {
		t.Errorf("err = %v, want it to fold in the stderr excerpt", err)
	}
}

func TestRun_MissingBinaryReturnsError(t *testing.T) {
	_, err := Run(context.Background(), "qf-toolrun-definitely-not-a-real-binary")
	if err == nil {
		t.Fatal("expected an error for a binary not on PATH")
	}
}

func TestRun_ContextCancellationReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, "echo", "hi")
	if err == nil {
		t.Fatal("expected an error for an already-cancelled context")
	}
}
