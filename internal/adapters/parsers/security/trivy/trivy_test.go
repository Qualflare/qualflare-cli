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
