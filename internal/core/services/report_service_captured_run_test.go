package services

import (
	"context"
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
)

func capturedSuiteWithStepError() *domain.Suite {
	return &domain.Suite{
		Name: "rspec (captured run)",
		Cases: []domain.Case{{
			Name:   "shows an error message",
			Status: domain.StatusFailed,
			Error:  "AssertionError: expected 'Welcome' to include 'Invalid credentials'\nat test/login.test.js:22",
			Steps: []domain.Step{
				{Keyword: "given", Name: "invalid credentials", Status: domain.StatusFailed},
				{Keyword: "then", Name: "shows an error message", Status: domain.StatusFailed,
					Error: "AssertionError: expected 'Welcome' to include 'Invalid credentials'"},
			},
		}},
	}
}

func TestBuildCapturedLaunch_WrapsSuiteIntoLaunch(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewReportService(nil, nil, cfg)

	launch := s.BuildCapturedLaunch(capturedSuiteWithStepError(), domain.FrameworkRSpec)

	if len(launch.Suites) != 1 {
		t.Fatalf("expected exactly 1 suite wrapped into the launch, got %d", len(launch.Suites))
	}
	if launch.Framework != string(domain.FrameworkRSpec) {
		t.Errorf("expected framework %q, got %q", domain.FrameworkRSpec, launch.Framework)
	}
	if launch.Suites[0].Cases[0].Name != "shows an error message" {
		t.Errorf("case data must pass through unchanged by default, got %+v", launch.Suites[0].Cases[0])
	}
}

// SEC-04 extension: log-capture mode's Case.Name/Step.Name/Status are the
// structural narration (same spirit as cucumber/karate's Gherkin step text)
// and must survive --no-capture-output, but Error text on both the case and
// its steps is reconstructed directly from raw captured stdout/stderr and is
// exactly what --no-capture-output promises to drop.
func TestBuildCapturedLaunch_NoCaptureOutputStripsStepAndCaseErrorButKeepsNarration(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.NoCaptureOutput = true
	s := NewReportService(nil, nil, cfg)

	launch := s.BuildCapturedLaunch(capturedSuiteWithStepError(), domain.FrameworkRSpec)
	c := launch.Suites[0].Cases[0]

	if c.Error != "" {
		t.Errorf("case Error must be stripped under --no-capture-output, got %q", c.Error)
	}
	if c.Name != "shows an error message" || c.Status != domain.StatusFailed {
		t.Errorf("case narration/status must survive, got name=%q status=%v", c.Name, c.Status)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("expected 2 steps to survive, got %d", len(c.Steps))
	}
	for _, step := range c.Steps {
		if step.Error != "" {
			t.Errorf("step Error must be stripped under --no-capture-output, got %q", step.Error)
		}
		if step.Name == "" || step.Keyword == "" {
			t.Errorf("step narration must survive, got %+v", step)
		}
	}
}

func TestBuildCapturedLaunch_DefaultKeepsStepAndCaseError(t *testing.T) {
	cfg := config.DefaultConfig() // NoCaptureOutput defaults false
	s := NewReportService(nil, nil, cfg)

	launch := s.BuildCapturedLaunch(capturedSuiteWithStepError(), domain.FrameworkRSpec)
	c := launch.Suites[0].Cases[0]

	if c.Error == "" {
		t.Error("by default the case Error must be preserved")
	}
	if c.Steps[1].Error == "" {
		t.Error("by default step Error must be preserved")
	}
}

func TestProcessCapturedSuite_DryRunDoesNotSend(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = true
	sender := &stubSender{}
	s := NewReportService(nil, sender, cfg)

	err := s.ProcessCapturedSuite(context.Background(), capturedSuiteWithStepError(), domain.FrameworkRSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.sent != 0 {
		t.Errorf("expected --dry-run to skip sending, but SendReport was called %d time(s)", sender.sent)
	}
}

func TestProcessCapturedSuite_SendsWhenNotDryRun(t *testing.T) {
	cfg := config.DefaultConfig()
	sender := &stubSender{}
	s := NewReportService(nil, sender, cfg)

	err := s.ProcessCapturedSuite(context.Background(), capturedSuiteWithStepError(), domain.FrameworkRSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.sent != 1 {
		t.Fatalf("expected exactly 1 SendReport call, got %d", sender.sent)
	}
	if sender.last.Framework != string(domain.FrameworkRSpec) {
		t.Errorf("expected the sent launch to carry the framework, got %q", sender.last.Framework)
	}
}
