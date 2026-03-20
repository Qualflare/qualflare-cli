package snyk

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestSnykParser_ParseVulnerabilities(t *testing.T) {
	jsonReport := `{
    "ok": false,
    "dependencyCount": 100,
    "org": "my-org",
    "projectName": "my-project",
    "packageManager": "npm",
    "summary": "2 vulnerabilities found",
    "uniqueCount": 2,
    "vulnerabilities": [
        {
            "id": "SNYK-JS-LODASH-1234",
            "title": "Prototype Pollution",
            "severity": "critical",
            "description": "Prototype pollution in lodash",
            "cvssScore": 9.8,
            "packageName": "lodash",
            "version": "4.17.20",
            "language": "js",
            "packageManager": "npm",
            "identifiers": {"CVE": ["CVE-2021-23337"], "CWE": ["CWE-1321"]},
            "semver": {"vulnerable": ["<4.17.21"]},
            "fixedIn": ["4.17.21"]
        },
        {
            "id": "SNYK-JS-MINIMIST-5678",
            "title": "Minor info leak",
            "severity": "low",
            "description": "Minor information leak",
            "cvssScore": 2.1,
            "packageName": "minimist",
            "version": "1.2.5",
            "language": "js",
            "packageManager": "npm",
            "identifiers": {"CVE": [], "CWE": []},
            "semver": {"vulnerable": ["<1.2.6"]}
        }
    ]
}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected 2 total tests, got %d", suite.TotalTests)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed (critical), got %d", suite.Failed)
	}
	if suite.Passed != 1 {
		t.Errorf("expected 1 passed (low), got %d", suite.Passed)
	}

	for _, c := range suite.Cases {
		if c.ID == "SNYK-JS-LODASH-1234" {
			if c.Severity != domain.SeverityCritical {
				t.Errorf("expected critical severity, got %s", c.Severity)
			}
			if c.Status != domain.StatusFailed {
				t.Errorf("expected failed status for critical vuln, got %s", c.Status)
			}
		}
		if c.ID == "SNYK-JS-MINIMIST-5678" {
			if c.Severity != domain.SeverityLow {
				t.Errorf("expected low severity, got %s", c.Severity)
			}
			if c.Status != domain.StatusPassed {
				t.Errorf("expected passed status for low vuln, got %s", c.Status)
			}
		}
	}
}

func TestSnykParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestSnykParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
