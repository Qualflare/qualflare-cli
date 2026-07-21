package trivy

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestTrivyParser_ParseVulnerabilities(t *testing.T) {
	jsonReport := `{
    "SchemaVersion": 2,
    "ArtifactName": "myapp:latest",
    "ArtifactType": "container_image",
    "Metadata": {
        "RepoTags": ["myapp:latest"],
        "RepoDigests": [],
        "ImageConfig": {"architecture": "amd64", "os": "linux"}
    },
    "Results": [
        {
            "Target": "myapp:latest (debian 11)",
            "Class": "os-pkgs",
            "Type": "debian",
            "Vulnerabilities": [
                {
                    "VulnerabilityID": "CVE-2023-0001",
                    "PkgName": "openssl",
                    "InstalledVersion": "1.1.1",
                    "FixedVersion": "1.1.2",
                    "Severity": "HIGH",
                    "Title": "OpenSSL buffer overflow",
                    "Description": "A buffer overflow in OpenSSL"
                },
                {
                    "VulnerabilityID": "CVE-2023-0002",
                    "PkgName": "zlib",
                    "InstalledVersion": "1.2.11",
                    "FixedVersion": "1.2.12",
                    "Severity": "LOW",
                    "Title": "Zlib minor issue",
                    "Description": "A minor issue in zlib"
                }
            ]
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
	if suite.Failed != 2 {
		t.Errorf("expected 2 failed (HIGH + LOW), got %d", suite.Failed)
	}
	if suite.Passed != 0 {
		t.Errorf("expected 0 passed, got %d", suite.Passed)
	}

	for _, c := range suite.Cases {
		if c.ID == "CVE-2023-0001" {
			if c.Priority != domain.SeverityHigh {
				t.Errorf("expected HIGH severity, got %s", c.Priority)
			}
			if c.Status != domain.StatusFailed {
				t.Errorf("expected failed status for HIGH vuln, got %s", c.Status)
			}
		}
		if c.ID == "CVE-2023-0002" {
			if c.Priority != domain.SeverityLow {
				t.Errorf("expected LOW severity, got %s", c.Priority)
			}
			if c.Status != domain.StatusFailed {
				t.Errorf("expected failed status for LOW vuln, got %s", c.Status)
			}
		}
	}
}

func TestTrivyParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestTrivyParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

// CLI-H7: Trivy legitimately emits "UNKNOWN". A security finding is never a pass;
// the old default branch mapped UNKNOWN to StatusPassed, silently turning a real
// vulnerability green. It must fail closed (StatusFailed) and the suite must be red.
func TestTrivyParser_UnknownSeverityFailsClosed(t *testing.T) {
	jsonReport := `{
    "SchemaVersion": 2,
    "ArtifactName": "myapp:latest",
    "ArtifactType": "container_image",
    "Results": [
        {
            "Target": "myapp:latest (debian 11)",
            "Class": "os-pkgs",
            "Type": "debian",
            "Vulnerabilities": [
                {
                    "VulnerabilityID": "CVE-2023-9999",
                    "PkgName": "somelib",
                    "InstalledVersion": "1.0.0",
                    "Severity": "UNKNOWN",
                    "Title": "Unclassified vulnerability",
                    "Description": "A vulnerability with unknown severity"
                }
            ]
        }
    ]
}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	if got := suite.Cases[0].Status; got != domain.StatusFailed {
		t.Errorf("UNKNOWN-severity CVE must be StatusFailed (fail closed), got %s", got)
	}
	if suite.Passed != 0 {
		t.Errorf("expected 0 passed for an UNKNOWN CVE, got %d", suite.Passed)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed for an UNKNOWN CVE, got %d", suite.Failed)
	}
	if suite.GetStatus() != domain.StatusFailed {
		t.Errorf("suite must roll up red for an UNKNOWN CVE, got %s", suite.GetStatus())
	}
}

// CLI-H6: a wrong-schema file (e.g. SARIF) decodes into an empty Report with no
// SchemaVersion and no Results. Returning an empty passing suite would silently
// pass a scan that never ran against this tool; the parser must error instead.
func TestTrivyParser_WrongSchemaErrors(t *testing.T) {
	// A SARIF document — valid JSON, but none of its keys map to the Trivy schema.
	sarif := `{
    "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
    "version": "2.1.0",
    "runs": [
        {"tool": {"driver": {"name": "SomeScanner"}}, "results": []}
    ]
}`

	parser := New()
	_, err := parser.Parse(strings.NewReader(sarif))
	if err == nil {
		t.Error("expected error for a wrong-schema (SARIF) file decoded into an empty Trivy report")
	}
}
