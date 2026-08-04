package services

import (
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
)

// suitesWithCapturedOutput builds a report whose one case carries both captured
// streams (system-out/system-err) plus an unrelated metadata property. The streams
// hold values a real environment might print — exactly what SEC-04 keeps off the wire.
func suitesWithCapturedOutput() []domain.Suite {
	return []domain.Suite{{
		Name: "Suite",
		Cases: []domain.Case{{
			Name:   "leaks env",
			Status: domain.StatusFailed,
			Properties: map[string]string{
				"system-out": "AWS_SECRET_ACCESS_KEY=AKIAEXAMPLE\n...",
				"system-err": "panic: authorization: Bearer sk-live-abc123",
				"browser":    "chrome",
			},
		}},
	}}
}

// SEC-04: with --no-capture-output the captured stdout/stderr must be gone from the
// report the service builds (and therefore from what SendReport uploads), while the
// failure/metadata that make the report useful survive.
func TestCreateReport_NoCaptureOutput_StripsStreams(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.NoCaptureOutput = true
	s := NewReportService(nil, nil, cfg)

	report := s.createReport(suitesWithCapturedOutput(), "pytest")
	props := report.Suites[0].Cases[0].Properties

	if _, ok := props["system-out"]; ok {
		t.Error("system-out must be stripped when --no-capture-output is set")
	}
	if _, ok := props["system-err"]; ok {
		t.Error("system-err must be stripped when --no-capture-output is set")
	}
	if props["browser"] != "chrome" {
		t.Errorf("non-output metadata must be preserved, got %q", props["browser"])
	}
	if report.Suites[0].Cases[0].Status != domain.StatusFailed {
		t.Error("the case status must be untouched — only output is dropped")
	}
}

// The default (flag absent) must still upload captured output — the feature is opt-in.
func TestCreateReport_DefaultKeepsCapturedOutput(t *testing.T) {
	cfg := config.DefaultConfig() // NoCaptureOutput defaults false
	s := NewReportService(nil, nil, cfg)

	report := s.createReport(suitesWithCapturedOutput(), "pytest")
	props := report.Suites[0].Cases[0].Properties

	if props["system-out"] == "" {
		t.Error("by default captured stdout must be preserved")
	}
	if props["system-err"] == "" {
		t.Error("by default captured stderr must be preserved")
	}
}
