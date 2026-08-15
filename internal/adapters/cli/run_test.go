package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/adapters/runner"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
)

// --- fakes -----------------------------------------------------------------

type fakeExtractor struct {
	framework  domain.Framework
	cases      []domain.Case
	extractErr error
	detectHits bool
}

func (f *fakeExtractor) ExtractCases(_ []byte) ([]domain.Case, error) { return f.cases, f.extractErr }
func (f *fakeExtractor) GetFramework() domain.Framework               { return f.framework }
func (f *fakeExtractor) Detect(_ []byte) bool                         { return f.detectHits }

type fakeRegistry struct {
	byFramework map[domain.Framework]ports.LogStepExtractor
	detected    ports.LogStepExtractor
	detectErr   error
}

func (r *fakeRegistry) GetExtractor(fw domain.Framework) (ports.LogStepExtractor, error) {
	e, ok := r.byFramework[fw]
	if !ok {
		return nil, errors.New("no extractor for framework: " + string(fw))
	}
	return e, nil
}
func (r *fakeRegistry) DetectExtractor(_ []byte) (ports.LogStepExtractor, error) {
	if r.detectErr != nil {
		return nil, r.detectErr
	}
	return r.detected, nil
}
func (r *fakeRegistry) RegisterExtractor(ports.LogStepExtractor)   {}
func (r *fakeRegistry) GetSupportedFrameworks() []domain.Framework { return nil }

func fakeRunCmd(result *runner.Result, err error) func(context.Context, io.Writer, string, ...string) (*runner.Result, error) {
	return func(_ context.Context, _ io.Writer, _ string, _ ...string) (*runner.Result, error) {
		return result, err
	}
}

func newRunCLI(t *testing.T) (*CLI, *stubReportService) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.APIKey = "qf_token"
	svc := &stubReportService{}
	return &CLI{reportService: svc, config: cfg}, svc
}

// --- tests -------------------------------------------------------------------

func TestCreateRunCommand_RequiresDashDash(t *testing.T) {
	c, _ := newRunCLI(t)
	c.logExtractors = &fakeRegistry{}
	cmd := c.createRunCommand()
	cmd.SetArgs([]string{"--"}) // no command after --
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error when no command is given after --")
	}
}

func TestRunRun_HappyPath_ExplicitFormat(t *testing.T) {
	c, svc := newRunCLI(t)
	rspec := &fakeExtractor{framework: domain.FrameworkRSpec, cases: []domain.Case{{Name: "a test", Status: domain.StatusPassed}}}
	c.logExtractors = &fakeRegistry{byFramework: map[domain.Framework]ports.LogStepExtractor{domain.FrameworkRSpec: rspec}}
	c.runCmd = fakeRunCmd(&runner.Result{Output: []byte("2 examples, 0 failures\n"), ExitCode: 0, Duration: time.Second}, nil)

	err := c.runRun(context.Background(), []string{"bundle", "exec", "rspec"}, runOptions{format: "rspec", timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.processed != 1 {
		t.Fatalf("expected ProcessCapturedSuite to be called once, got %d", svc.processed)
	}
	if svc.lastFW != domain.FrameworkRSpec {
		t.Errorf("expected framework rspec to reach the report service, got %q", svc.lastFW)
	}
}

func TestRunRun_AutoDetectsFrameworkWhenFormatOmitted(t *testing.T) {
	c, svc := newRunCLI(t)
	mocha := &fakeExtractor{framework: domain.FrameworkMocha, cases: []domain.Case{{Name: "a test"}}, detectHits: true}
	c.logExtractors = &fakeRegistry{detected: mocha}
	c.runCmd = fakeRunCmd(&runner.Result{Output: []byte("1 passing (5ms)\n"), ExitCode: 0}, nil)

	err := c.runRun(context.Background(), []string{"npx", "mocha"}, runOptions{timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastFW != domain.FrameworkMocha {
		t.Errorf("expected auto-detected framework mocha, got %q", svc.lastFW)
	}
}

// The wrapped command's own exit code is the primary signal, propagated even
// when the upload itself succeeds — a real test failure must be visible to CI.
func TestRunRun_WrappedCommandFailure_PropagatesExitCode(t *testing.T) {
	c, svc := newRunCLI(t)
	e := &fakeExtractor{framework: domain.FrameworkRSpec, cases: []domain.Case{{Name: "a test", Status: domain.StatusFailed}}}
	c.logExtractors = &fakeRegistry{byFramework: map[domain.Framework]ports.LogStepExtractor{domain.FrameworkRSpec: e}}
	c.runCmd = fakeRunCmd(&runner.Result{Output: []byte("1 example, 1 failure\n"), ExitCode: 1}, nil)

	err := c.runRun(context.Background(), []string{"rspec"}, runOptions{format: "rspec", timeout: 5 * time.Second})
	var wrapped *WrappedCommandError
	if !errors.As(err, &wrapped) {
		t.Fatalf("expected a *WrappedCommandError, got %v", err)
	}
	if wrapped.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", wrapped.ExitCode)
	}
	if svc.processed != 1 {
		t.Error("the upload must still be attempted even when the wrapped command failed")
	}
}

// An upload failure must never be silently swallowed, but it also must never
// mask a real test failure — the wrapped command's exit code still wins.
func TestRunRun_WrappedFailureAndUploadFailure_StillReportsWrappedExitCode(t *testing.T) {
	c, svc := newRunCLI(t)
	svc.processErr = errors.New("network blip")
	e := &fakeExtractor{framework: domain.FrameworkRSpec, cases: []domain.Case{{Name: "a test"}}}
	c.logExtractors = &fakeRegistry{byFramework: map[domain.Framework]ports.LogStepExtractor{domain.FrameworkRSpec: e}}
	c.runCmd = fakeRunCmd(&runner.Result{Output: []byte("out"), ExitCode: 2}, nil)

	err := c.runRun(context.Background(), []string{"rspec"}, runOptions{format: "rspec", timeout: 5 * time.Second})
	var wrapped *WrappedCommandError
	if !errors.As(err, &wrapped) || wrapped.ExitCode != 2 {
		t.Fatalf("expected *WrappedCommandError{ExitCode: 2}, got %v", err)
	}
}

// An upload failure on an otherwise-passing run must be a real, visible error
// — not swallowed just because the tests themselves passed.
func TestRunRun_UploadFailureOnPassingRun_ReturnsError(t *testing.T) {
	c, svc := newRunCLI(t)
	svc.processErr = errors.New("network blip")
	e := &fakeExtractor{framework: domain.FrameworkRSpec, cases: []domain.Case{{Name: "a test", Status: domain.StatusPassed}}}
	c.logExtractors = &fakeRegistry{byFramework: map[domain.Framework]ports.LogStepExtractor{domain.FrameworkRSpec: e}}
	c.runCmd = fakeRunCmd(&runner.Result{Output: []byte("out"), ExitCode: 0}, nil)

	err := c.runRun(context.Background(), []string{"rspec"}, runOptions{format: "rspec", timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("expected an error when the upload fails on an otherwise-passing run")
	}
	var wrapped *WrappedCommandError
	if errors.As(err, &wrapped) {
		t.Error("an upload-only failure on a passing run must not be reported as a WrappedCommandError")
	}
}

func TestRunRun_CommandNotFound_ReturnsPlainError(t *testing.T) {
	c, _ := newRunCLI(t)
	c.logExtractors = &fakeRegistry{}
	c.runCmd = fakeRunCmd(nil, errors.New("command not found: nosuchtool"))

	err := c.runRun(context.Background(), []string{"nosuchtool"}, runOptions{timeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "nosuchtool") {
		t.Fatalf("expected the command-not-found error to surface, got %v", err)
	}
}

func TestRunRun_NoCasesRecognized_WarnsButDoesNotError(t *testing.T) {
	c, svc := newRunCLI(t)
	e := &fakeExtractor{framework: domain.FrameworkRSpec, cases: nil}
	c.logExtractors = &fakeRegistry{byFramework: map[domain.Framework]ports.LogStepExtractor{domain.FrameworkRSpec: e}}
	c.runCmd = fakeRunCmd(&runner.Result{Output: []byte("nothing recognizable"), ExitCode: 0}, nil)

	err := c.runRun(context.Background(), []string{"rspec"}, runOptions{format: "rspec", timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.processed != 1 {
		t.Error("expected the (empty) suite to still be uploaded, not skipped")
	}
}
