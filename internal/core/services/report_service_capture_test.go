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
func TestIsStructuralCaseProperty(t *testing.T) {
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
		if !isStructuralCaseProperty(key) {
			t.Errorf("isStructuralCaseProperty(%q) = false; a parser emits this key and "+
				"--no-capture-output would silently drop it", key)
		}
	}

	userAuthored := []string{
		"AWS_SECRET_ACCESS_KEY", "db_password", "token", "", "Shard", "FILE",
		"fixture.apiKey", "test.tenant", "system-out", "system-err", "cvss", "my_cvss_x",
	}
	for _, key := range userAuthored {
		if isStructuralCaseProperty(key) {
			t.Errorf("isStructuralCaseProperty(%q) = true; a key no parser generates must "+
				"be treated as user-authored and stripped", key)
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
