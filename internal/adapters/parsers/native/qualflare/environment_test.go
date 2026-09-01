package qualflare

import (
	"strings"
	"testing"
)

// report returns a minimal qualflare-json document, optionally carrying a
// top-level environment (the field every reporter writes from its own
// `environment` option).
func report(environment string) string {
	env := ""
	if environment != "" {
		env = `"environment":"` + environment + `",`
	}
	return `{"framework":"vitest","platform":"web",` + env +
		`"metadata":{"version":"0.1.0","timestamp":"2026-09-02T00:00:00Z","cliName":"qualflare-vitest"},` +
		`"suites":[{"name":"s","duration":1000000,"cases":[` +
		`{"id":"1","name":"c","status":"passed","duration":1000000}]}]}`
}

// The regression this guards: the parser decoded framework/platform/browser
// and dropped `environment` on the floor, so a reporter configured for
// `staging` produced a launch in the CLI's default environment instead. That
// is a wrong destination, not a missing label, and nothing warned about it.
func TestParse_CarriesEnvironmentFromTheReport(t *testing.T) {
	suite, err := New().Parse(strings.NewReader(report("staging")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := suite.Properties["environment"]; got != "staging" {
		t.Errorf("environment must survive parsing; got %q, want %q", got, "staging")
	}
}

// Reports from reporters released before this existed carry no environment.
// Inventing one would be worse than the bug: the CLI's own default is at
// least the documented behaviour those users already have.
func TestParse_NoEnvironmentInReportSetsNoProperty(t *testing.T) {
	suite, err := New().Parse(strings.NewReader(report("")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, ok := suite.Properties["environment"]; ok {
		t.Errorf("absent environment must stay absent, not become %q", got)
	}
}

func TestParse_EnvironmentDoesNotDisturbPlatform(t *testing.T) {
	suite, err := New().Parse(strings.NewReader(report("staging")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := suite.Properties["platform"]; got != "web" {
		t.Errorf("platform should still be carried; got %q", got)
	}
}
