package services

import (
	"context"
	"testing"

	"qualflare-cli/internal/adapters/parsers/factory"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
)

// Launch.Framework must name what PRODUCED the results, never how they
// travelled.
//
// qualflare-json and ctrf are the two collection formats: the @qualflare/*
// plugins write the first, third-party reporters the second. Labelling a launch
// with either says "this launch was produced by a file format", which is not a
// thing — and it made the framework filter useless for every plugin upload.
// Measured on production 2026-09-06: 81 launches across six @qualflare-*
// projects, all labelled qualflare-json.
//
// Driven through ParseTestResults rather than resolveLaunchFramework directly,
// because the fix depends on the parsers recording PropSourceFramework on the
// way through — testing the resolver alone would assert against a fixture I
// wrote rather than against what the parsers actually produce.

func parseLaunch(t *testing.T, filename, body string, format domain.Framework) *domain.Launch {
	t.Helper()
	s := NewReportService(factory.NewParserFactory(), nil, config.DefaultConfig())
	path := writeFile(t, t.TempDir(), filename, body)
	launch, err := s.ParseTestResults(context.Background(), []string{path}, format)
	if err != nil {
		t.Fatalf("ParseTestResults() = %v", err)
	}
	return launch
}

func TestLaunchFramework_QualflareJSONReportsItsProducer(t *testing.T) {
	const report = `{
	  "framework": "cypress",
	  "suites": [{
	    "name": "checkout",
	    "cases": [{"name": "adds to cart", "status": "passed", "duration": 120}]
	  }]
	}`

	launch := parseLaunch(t, "collect.json", report, domain.FrameworkQualflareJSON)

	if launch.Framework == string(domain.FrameworkQualflareJSON) {
		t.Fatalf("Framework = %q — that is the FORMAT the report arrived in, not the "+
			"tool that ran the tests. The file says cypress.", launch.Framework)
	}
	if launch.Framework != string(domain.FrameworkCypress) {
		t.Errorf("Framework = %q, want %q", launch.Framework, domain.FrameworkCypress)
	}
}

func TestLaunchFramework_CTRFReportsItsTool(t *testing.T) {
	const report = `{
	  "reportFormat": "CTRF",
	  "specVersion": "0.0.0",
	  "results": {
	    "tool": {"name": "playwright"},
	    "summary": {"tests": 1, "passed": 1, "failed": 0, "pending": 0, "skipped": 0,
	                "other": 0, "start": 0, "stop": 1},
	    "tests": [{"name": "loads", "status": "passed", "duration": 100}]
	  }
	}`

	launch := parseLaunch(t, "ctrf.json", report, domain.FrameworkCTRF)

	if launch.Framework == string(domain.FrameworkCTRF) {
		t.Fatalf("Framework = %q — CTRF is the transport, not the producer. "+
			"results.tool.name says playwright.", launch.Framework)
	}
	if launch.Framework != string(domain.FrameworkPlaywright) {
		t.Errorf("Framework = %q, want %q", launch.Framework, domain.FrameworkPlaywright)
	}
}

// An unresolvable producer falls back to the format name. That is at least TRUE,
// where guessing a near neighbour would not be — the same reasoning
// categoryForTool applies when it declines to map jasmine or the .NET runners.
func TestLaunchFramework_UnknownToolFallsBackToTheFormat(t *testing.T) {
	const report = `{
	  "reportFormat": "CTRF",
	  "results": {
	    "tool": {"name": "some-runner-we-do-not-model"},
	    "summary": {"tests": 1, "passed": 1, "failed": 0, "pending": 0, "skipped": 0,
	                "other": 0, "start": 0, "stop": 1},
	    "tests": [{"name": "loads", "status": "passed", "duration": 100}]
	  }
	}`

	launch := parseLaunch(t, "ctrf.json", report, domain.FrameworkCTRF)

	if launch.Framework != string(domain.FrameworkCTRF) {
		t.Errorf("Framework = %q, want the format name %q as an honest fallback",
			launch.Framework, domain.FrameworkCTRF)
	}
}

// A non-passthrough format is untouched: --format playwright still labels the
// launch playwright, straight from the parser.
func TestLaunchFramework_DirectFormatIsUnchanged(t *testing.T) {
	const report = `<?xml version="1.0" encoding="utf-8"?>
<testsuite name="pytest" tests="1" failures="0" errors="0" skipped="0" time="0.5">
  <testcase classname="tests.test_a" name="test_one" time="0.5"/>
</testsuite>`

	launch := parseLaunch(t, "results.xml", report, domain.FrameworkPython)

	if launch.Framework != string(domain.FrameworkPython) {
		t.Errorf("Framework = %q, want %q — only PASSTHROUGH formats defer to the file",
			launch.Framework, domain.FrameworkPython)
	}
}

// The producing framework rides on the suite so a MERGED shard file carrying two
// producers is detectable. Without a per-suite record the launch would silently
// take whichever parsed first — the shape of BUG-41.
func TestProducersOf_TwoProducersAreMixedNotWhicheverCameFirst(t *testing.T) {
	suites := []domain.Suite{
		{Properties: map[string]string{domain.PropSourceFramework: "cucumber"}},
		{Properties: map[string]string{domain.PropSourceFramework: "cypress"}},
	}
	got := resolveLaunchFramework(&stubParser{framework: domain.FrameworkQualflareJSON}, nil, "", suites)
	if got != "mixed" {
		t.Errorf("Framework = %q, want \"mixed\" for a file spanning two producers", got)
	}
}

// Ordering must not decide the answer.
func TestProducersOf_IsOrderIndependent(t *testing.T) {
	a := []domain.Suite{
		{Properties: map[string]string{domain.PropSourceFramework: "cypress"}},
		{Properties: map[string]string{domain.PropSourceFramework: "cypress"}},
	}
	b := []domain.Suite{a[1], a[0]}
	if resolveLaunchFramework(&stubParser{framework: domain.FrameworkQualflareJSON}, nil, "", a) !=
		resolveLaunchFramework(&stubParser{framework: domain.FrameworkQualflareJSON}, nil, "", b) {
		t.Error("the resolved framework depends on suite order")
	}
}
