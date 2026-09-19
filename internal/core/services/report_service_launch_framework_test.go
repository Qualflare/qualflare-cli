package services

import (
	"context"
	"encoding/json"
	"strings"
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

// Nobody passes --format when uploading a report directory: `qf <project> collect
// ./qualflare-results` is the documented line in every reporter's README and in the
// CI snippets on qualflare.com. That path resolved to the FORMAT, because
// resolveLaunchFramework only unwrapped a passthrough format in its explicit-parser
// arm, leaving the auto-detect arm to return whatever was detected.
//
// Measured on production 2026-09-20, after the passthrough fix shipped in v0.1.25:
// the newest launch of qualflare-maestro, qualflare-testng, qualflare-jest and
// qualflare-go was still labelled qualflare-json, because all four upload a
// directory without --format.
func TestLaunchFramework_AutoDetectedReportReportsItsProducer(t *testing.T) {
	// framework + metadata + suites is what the content detector keys on, and what
	// every native reporter writes.
	const report = `{
	  "framework": "maestro",
	  "metadata": {"reporterVersion": "0.1.0"},
	  "suites": [{
	    "name": "flows",
	    "cases": [{"name": "Settings opens", "status": "passed", "duration": 120}]
	  }]
	}`

	// The empty format is what collect passes when --format is absent.
	launch := parseLaunch(t, "collect.json", report, "")

	if launch.Framework == string(domain.FrameworkQualflareJSON) {
		t.Fatalf("Framework = %q — the report says maestro, and an absent --format "+
			"must not relabel it as the format it arrived in", launch.Framework)
	}
	if launch.Framework != "maestro" {
		t.Errorf("Framework = %q, want %q", launch.Framework, "maestro")
	}
}

// Every native reporter writes platform and environment alongside framework, and
// the parser used to REPLACE the suite's property map when it carried either --
// dropping the PropSourceFramework marker set moments earlier. That defeated the
// label fix silently: the existing tests above all use fixtures with neither key,
// so both the v0.1.25 fix and its auto-detect counterpart above passed while
// production stayed labelled qualflare-json.
func TestLaunchFramework_PlatformInTheReportKeepsTheProducer(t *testing.T) {
	const report = `{
	  "framework": "maestro",
	  "platform": "ios",
	  "environment": "production",
	  "metadata": {"reporterVersion": "0.1.0"},
	  "suites": [{
	    "name": "flows",
	    "cases": [{"name": "Settings opens", "status": "passed", "duration": 120}]
	  }]
	}`

	for _, format := range []domain.Framework{"", domain.FrameworkQualflareJSON} {
		launch := parseLaunch(t, "collect.json", report, format)
		if launch.Framework != "maestro" {
			t.Errorf("--format %q: Framework = %q, want \"maestro\"", format, launch.Framework)
		}
	}
}

// Native reporters detect their CI and write it into the report; nothing read it,
// so every launch from every reporter reported no CI at all. Measured on
// production 2026-09-20: qualflare-maestro, qualflare-testng, qualflare-jest and
// qualflare-go all showed ciProvider: null on runs that plainly happened in
// GitHub Actions.
func TestLaunchCI_ComesFromTheReport(t *testing.T) {
	const report = `{
	  "framework": "maestro",
	  "metadata": {"reporterVersion": "0.1.0"},
	  "ciProvider": "github",
	  "ciBuildNumber": "35398054421",
	  "ciRunUrl": "https://github.com/Qualflare/qualflare-maestro/actions/runs/35398054421",
	  "ciPrNumber": 42,
	  "suites": [{
	    "name": "flows",
	    "cases": [{"name": "Settings opens", "status": "passed", "duration": 120}]
	  }]
	}`

	launch := parseLaunch(t, "collect.json", report, "")

	if launch.CIProvider != "github" || launch.CIBuildNumber != "35398054421" {
		t.Errorf("CIProvider = %q, CIBuildNumber = %q", launch.CIProvider, launch.CIBuildNumber)
	}
	if launch.CIRunURL == "" {
		t.Error("CIRunURL is empty — the report carried one")
	}
	if launch.CIPRNumber == nil || *launch.CIPRNumber != 42 {
		t.Errorf("CIPRNumber = %v, want 42", launch.CIPRNumber)
	}
	// Promoted onto the launch, not left duplicated in its properties.
	for _, k := range []string{domain.PropCIProvider, domain.PropCIBuildNumber, domain.PropCIRunURL, domain.PropCIPRNumber} {
		if _, dup := launch.Properties[k]; dup {
			t.Errorf("property %q is still on the launch as well", k)
		}
	}
}

// The API validates CI metadata per launch, so one bad value must not cost the
// whole upload: send nothing rather than something it will 422.
func TestLaunchCI_ValuesTheAPIWouldRejectAreDropped(t *testing.T) {
	report := `{
	  "framework": "maestro",
	  "metadata": {"reporterVersion": "0.1.0"},
	  "ciProvider": "` + strings.Repeat("p", 65) + `",
	  "ciBuildNumber": "` + strings.Repeat("9", 129) + `",
	  "ciRunUrl": "not-a-url",
	  "ciPrNumber": 0,
	  "suites": [{"name": "flows", "cases": [{"name": "a", "status": "passed", "duration": 1}]}]
	}`

	launch := parseLaunch(t, "collect.json", report, "")

	if launch.CIProvider != "" || launch.CIBuildNumber != "" || launch.CIRunURL != "" || launch.CIPRNumber != nil {
		t.Errorf("kept a value the API rejects: provider=%q build=%q url=%q pr=%v",
			launch.CIProvider, launch.CIBuildNumber, launch.CIRunURL, launch.CIPRNumber)
	}
}

// A report with no CI metadata must leave the fields absent, not empty-but-present:
// the API distinguishes them.
func TestLaunchCI_AbsentWhenTheReportHasNone(t *testing.T) {
	const report = `{
	  "framework": "maestro",
	  "metadata": {"reporterVersion": "0.1.0"},
	  "suites": [{"name": "flows", "cases": [{"name": "a", "status": "passed", "duration": 1}]}]
	}`

	launch := parseLaunch(t, "collect.json", report, "")
	body, err := json.Marshal(launch)
	if err != nil {
		t.Fatalf("Marshal() = %v", err)
	}
	for _, k := range []string{"ciProvider", "ciBuildNumber", "ciRunUrl", "ciPrNumber"} {
		if strings.Contains(string(body), k) {
			t.Errorf("%q is in the payload for a report that carried no CI metadata", k)
		}
	}
}
