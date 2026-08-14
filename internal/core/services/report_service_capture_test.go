package services

import (
	"context"
	"testing"

	"qualflare-cli/internal/adapters/parsers/factory"
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

// The hazard --no-capture-output exists for is not limited to the two capture keys.
// Once generic <property> values started flowing through the JUnit/pytest parsers, a
// test that records a secret — record_property("AWS_SECRET_ACCESS_KEY", ...), a
// TestCafe fixture meta entry, a custom CI property — landed in the payload untouched
// by the very flag meant to prevent that. Every key a test author chose must go.
func TestCreateReport_NoCaptureOutput_StripsCustomProperties(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.NoCaptureOutput = true
	s := NewReportService(nil, nil, cfg)

	suites := []domain.Suite{{
		Name: "Suite",
		Cases: []domain.Case{{
			Name:   "records secrets",
			Status: domain.StatusPassed,
			Properties: map[string]string{
				// User-authored keys: all must be dropped.
				"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI/K7MDENG",
				"db_password":           "hunter2",
				"deploy_token":          "ghp_abc123",
				"fixture.apiKey":        "sk-live-abc123", // TestCafe fixture meta
				"test.tenant":           "acme-internal",  // TestCafe test meta
				"system-out":            "echo $AWS_SECRET_ACCESS_KEY",
				"system-err":            "Bearer sk-live-abc123",
				// Parser-generated structural keys: all must survive.
				"shard":    "2",
				"file":     "tests/test_login.py",
				"line":     "42",
				"project":  "chromium",
				"severity": "critical",
				"package":  "openssl",
				"cvss_nvd": "9.8",
			},
		}},
	}}

	props := s.createReport(suites, "python").Suites[0].Cases[0].Properties

	for _, secret := range []string{
		"AWS_SECRET_ACCESS_KEY", "db_password", "deploy_token",
		"fixture.apiKey", "test.tenant", "system-out", "system-err",
	} {
		if v, ok := props[secret]; ok {
			t.Errorf("property %q survived --no-capture-output with value %q", secret, v)
		}
	}
	want := map[string]string{
		"shard":    "2",
		"file":     "tests/test_login.py",
		"line":     "42",
		"project":  "chromium",
		"severity": "critical",
		"package":  "openssl",
		"cvss_nvd": "9.8",
	}
	for key, value := range want {
		if props[key] != value {
			t.Errorf("structural property %q = %q, want %q — --no-capture-output must not "+
				"break shard reporting or per-case metadata", key, props[key], value)
		}
	}
	if len(props) != len(want) {
		t.Errorf("properties = %v, want exactly the %d structural keys", props, len(want))
	}
}

// The strip is opt-in: without the flag, even a property that looks like a secret is
// uploaded as-is. Users who want it gone have a flag; the default must not silently
// drop data the report declared.
func TestCreateReport_DefaultKeepsCustomProperties(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewReportService(nil, nil, cfg)

	suites := []domain.Suite{{
		Cases: []domain.Case{{
			Name:       "t",
			Properties: map[string]string{"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI", "shard": "1"},
		}},
	}}

	props := s.createReport(suites, "junit").Suites[0].Cases[0].Properties
	if props["AWS_SECRET_ACCESS_KEY"] != "wJalrXUtnFEMI" || props["shard"] != "1" {
		t.Errorf("properties = %v, want both preserved without --no-capture-output", props)
	}
}

// The allowlist is what keeps --no-capture-output from gutting reports whose entire
// value lives in case properties (security scanners, API runners). Every key the
// repo's own parsers synthesize has to be on it.
func TestIsStructuralProperty(t *testing.T) {
	structural := []string{
		"shard", "retries", "retryCount", // junitxml/pytest signals
		"file", "line", "line_number", "path", // source location
		"project", "fullTitle", "methodName", "fixture", "feature", "uri",
		"group", "speed", "browser", "userAgent",
		"method", "responseCode", "responseTime", // newman
		"passes", "fails", "passRate", // k6 checks
		"package", "installedVersion", "fixedVersion", "version", "severity", "url",
		"resolution", "cvssScore", "isPatchable", "isUpgradable", "language",
		"packageManager", "fixedIn", "dependencyPath", // trivy/snyk
		"cvss_nvd", "cvss_redhat", "cvss_ghsa", // trivy, per scoring source
		"host", "port", "riskCode", "riskDesc", "confidence", "cweId", "wascId",
		"solution", "reference", "instanceCount", "affectedURL", // zap
		"rule", "ruleName", "type", "status", "effort", "component", "assignee", // sonarqube
	}
	for _, key := range structural {
		if !isStructuralProperty(key, "anything") {
			t.Errorf("isStructuralProperty(%q) = false; a parser emits this key and "+
				"--no-capture-output would silently drop it", key)
		}
	}

	userAuthored := []string{
		"AWS_SECRET_ACCESS_KEY", "db_password", "token", "", "Shard", "FILE",
		"fixture.apiKey", "test.tenant", "system-out", "system-err", "cvss", "my_cvss_x",
	}
	for _, key := range userAuthored {
		if isStructuralProperty(key, "a-secret") {
			t.Errorf("isStructuralProperty(%q) = true; a key no parser generates must "+
				"be treated as user-authored and stripped", key)
		}
	}
}

// k6's threshold cases carry the metric's aggregations (avg, p(95), ...), which are
// measurements the parser copied out of the report — as structural as trivy's cvss_*
// scores, and stripping them was pure data loss for no security gain. But unlike
// cvss_*, several of those keys are bare words a test author could also pick, so the
// value has to look like the formatted float the k6 parser writes; anything else is
// treated as a user-authored property that happened to collide.
func TestIsStructuralProperty_K6MetricAggregations(t *testing.T) {
	tests := []struct {
		key, value string
		want       bool
	}{
		{"avg", "234.56", true},
		{"min", "45.23", true},
		{"med", "189.34", true},
		{"max", "1234.56", true},
		{"count", "1000", true},
		{"rate", "0.05", true},
		{"value", "16.60", true},
		{"p(90)", "456.78", true},
		{"p(95)", "567.89", true},
		{"p(99.9)", "1200.00", true}, // any percentile a threshold defines
		// The collision the value check exists for: same key, a test author's secret.
		{"max", "sk-live-abc123", false},
		{"rate", "$RATE_LIMIT_TOKEN", false},
		{"value", "wJalrXUtnFEMI/K7MDENG", false},
		{"p(95)", "Bearer abc", false},
		{"avg", "", false},
		// A p(-prefixed key is not automatically structural either.
		{"p(assword)", "hunter2", false},
	}
	for _, tt := range tests {
		if got := isStructuralProperty(tt.key, tt.value); got != tt.want {
			t.Errorf("isStructuralProperty(%q, %q) = %v, want %v", tt.key, tt.value, got, tt.want)
		}
	}
}

// Filtering must survive the shapes real parsers produce: a nil Properties map, an
// empty one, and cases spread across several suites.
func TestStripSensitiveCaseProperties_EdgeShapes(t *testing.T) {
	suites := []domain.Suite{
		{Cases: []domain.Case{{Name: "nil props"}}},
		{Cases: []domain.Case{{Name: "empty props", Properties: map[string]string{}}}},
		{Cases: []domain.Case{
			{Name: "a", Properties: map[string]string{"secret": "s", "file": "a.py"}},
			{Name: "b", Properties: map[string]string{"secret": "s"}},
		}},
	}

	stripSensitiveCaseProperties(suites)

	if suites[0].Cases[0].Properties != nil {
		t.Errorf("a nil Properties map must stay nil, got %v", suites[0].Cases[0].Properties)
	}
	if len(suites[1].Cases[0].Properties) != 0 {
		t.Errorf("empty properties = %v, want still empty", suites[1].Cases[0].Properties)
	}
	if got := suites[2].Cases[0].Properties; len(got) != 1 || got["file"] != "a.py" {
		t.Errorf("properties = %v, want only the structural file key", got)
	}
	if got := suites[2].Cases[1].Properties; len(got) != 0 {
		t.Errorf("properties = %v, want every user-authored key gone", got)
	}
}

// --- suite-level properties -------------------------------------------------------

// record_testsuite_property() is the sibling of the record_property() the case-level
// filter covers, and it writes to the <testsuite> element instead of the <testcase>.
// Stripping only case properties left --no-capture-output claiming, in its own help
// text, a coverage it did not have: a secret recorded one line differently sailed
// straight through.
//
// Driven through the real pytest parser rather than a hand-built domain.Suite, because
// the gap was precisely that a real report reaches suite.Properties by a path the
// case-level fix never touched.
func TestParseTestResults_NoCaptureOutput_StripsPytestSuiteProperties(t *testing.T) {
	const report = `<?xml version="1.0" encoding="utf-8"?>
<testsuite name="pytest" tests="1" failures="0" errors="0" skipped="0" time="0.5">
  <properties>
    <property name="AWS_SECRET_ACCESS_KEY" value="wJalrXUtnFEMI/K7MDENG"/>
    <property name="db_password" value="hunter2"/>
    <property name="shard" value="3"/>
  </properties>
  <testcase classname="tests.test_a" name="test_one" file="tests/test_a.py" line="3" time="0.5"/>
</testsuite>`

	parse := func(t *testing.T, noCapture bool) map[string]string {
		t.Helper()
		cfg := config.DefaultConfig()
		cfg.NoCaptureOutput = noCapture
		s := NewReportService(factory.NewParserFactory(), nil, cfg)

		path := writeFile(t, t.TempDir(), "results.xml", report)
		launch, err := s.ParseTestResults(context.Background(), []string{path}, domain.FrameworkPython)
		if err != nil {
			t.Fatalf("ParseTestResults() = %v", err)
		}
		if len(launch.Suites) != 1 {
			t.Fatalf("suites = %d, want 1", len(launch.Suites))
		}
		return launch.Suites[0].Properties
	}

	t.Run("stripped with the flag", func(t *testing.T) {
		props := parse(t, true)
		for _, secret := range []string{"AWS_SECRET_ACCESS_KEY", "db_password"} {
			if v, ok := props[secret]; ok {
				t.Errorf("suite property %q survived --no-capture-output with value %q", secret, v)
			}
		}
		// The same allowlist as case properties, so a structural key still survives and
		// the flag does not blanket-empty the map.
		if props["shard"] != "3" {
			t.Errorf("suite property shard = %q, want %q — the allowlist applies here too",
				props["shard"], "3")
		}
	})

	t.Run("preserved by default", func(t *testing.T) {
		props := parse(t, false)
		if props["AWS_SECRET_ACCESS_KEY"] != "wJalrXUtnFEMI/K7MDENG" || props["db_password"] != "hunter2" {
			t.Errorf("suite properties = %v, want all preserved without --no-capture-output", props)
		}
	})
}

// The filter is deliberately pytest-only: every other parser fills suite Properties from
// its own fixed structural vocabulary (zapVersion, artifactName, browser, k6's
// http_req_* summary, ...), none of it user-authored, and filtering those would need a
// second allowlist whose only effect would be to gut scanner and load-test summaries the
// day a key is missed. A launch can mix frameworks, so this has to be decided per suite.
func TestStripUserAuthoredSuiteProperties_PytestOnly(t *testing.T) {
	tests := []struct {
		name      string
		framework domain.Framework
		props     map[string]string
		want      map[string]string
	}{
		{
			"pytest suite properties are user-authored and filtered",
			domain.FrameworkPython,
			map[string]string{"AWS_SECRET_ACCESS_KEY": "wJalrXUtnFEMI", "shard": "1"},
			map[string]string{"shard": "1"},
		},
		{
			"trivy scan metadata is untouched",
			domain.FrameworkTrivy,
			map[string]string{"artifactName": "myapp:latest", "artifactType": "container_image"},
			map[string]string{"artifactName": "myapp:latest", "artifactType": "container_image"},
		},
		{
			"k6 metric summary is untouched",
			domain.FrameworkK6,
			map[string]string{"http_req_duration_avg": "234.56", "http_reqs_count": "1000"},
			map[string]string{"http_req_duration_avg": "234.56", "http_reqs_count": "1000"},
		},
		{
			"zap version is untouched",
			domain.FrameworkZAP,
			map[string]string{"zapVersion": "2.14.0"},
			map[string]string{"zapVersion": "2.14.0"},
		},
		{
			"junit carries no suite properties at all",
			domain.FrameworkJUnit,
			nil,
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suite := &domain.Suite{Name: "s", Properties: tt.props}
			stripUserAuthoredSuiteProperties(suite, tt.framework)
			if len(suite.Properties) != len(tt.want) {
				t.Fatalf("properties = %v, want %v", suite.Properties, tt.want)
			}
			for key, value := range tt.want {
				if suite.Properties[key] != value {
					t.Errorf("property %q = %q, want %q", key, suite.Properties[key], value)
				}
			}
		})
	}
}
