package trivy

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

// Trivy's misconfiguration findings (IaC scanning) were an entirely untested code path.
// Unlike vulnerabilities, a misconfig carries a Status: only FAIL is a finding, and a
// PASS must not be reported as one.
func TestConvertMisconfig_StatusDrivesResult(t *testing.T) {
	p := &Parser{}

	t.Run("FAIL is a failure carrying message and resolution", func(t *testing.T) {
		got := p.convertMisconfig(Misconfig{
			ID:         "AVD-AWS-0088",
			Title:      "S3 bucket not encrypted",
			Message:    "Bucket does not have encryption enabled",
			Resolution: "Enable server-side encryption",
			Severity:   "HIGH",
			Status:     "FAIL",
		}, "terraform/main.tf")

		if got.Status != domain.StatusFailed {
			t.Errorf("Status = %q, want failed", got.Status)
		}
		for _, want := range []string{"Bucket does not have encryption enabled", "Enable server-side encryption"} {
			if !strings.Contains(got.Error, want) {
				t.Errorf("Error = %q, missing %q", got.Error, want)
			}
		}
	})

	t.Run("PASS is not a finding", func(t *testing.T) {
		got := p.convertMisconfig(Misconfig{
			ID: "AVD-AWS-0088", Severity: "HIGH", Status: "PASS",
			Message: "checked", Resolution: "none needed",
		}, "terraform/main.tf")

		if got.Status != domain.StatusPassed {
			t.Errorf("Status = %q, want passed", got.Status)
		}
		if got.Error != "" {
			t.Errorf("Error = %q, want empty for a passing check", got.Error)
		}
	})

	// Anything that is not exactly "FAIL" passes, including an empty status.
	t.Run("unknown status is not a failure", func(t *testing.T) {
		for _, status := range []string{"", "EXCEPTION", "SKIP"} {
			got := p.convertMisconfig(Misconfig{ID: "x", Status: status}, "f.tf")
			if got.Status != domain.StatusPassed {
				t.Errorf("status %q -> %q, want passed", status, got.Status)
			}
		}
	})
}

func TestConvertMisconfig_SeverityMapping(t *testing.T) {
	p := &Parser{}
	tests := []struct {
		in   string
		want domain.Severity
	}{
		{"CRITICAL", domain.SeverityCritical},
		{"HIGH", domain.SeverityHigh},
		{"MEDIUM", domain.SeverityMedium},
		{"LOW", domain.SeverityLow},
		{"UNKNOWN", domain.SeverityUnknown},
		{"", domain.SeverityUnknown},
		// Matching is case-insensitive, because Trivy emits upper-case and Snyk
		// lower-case through the same shared mapping. A differently-cased label is
		// ranked rather than discarded — that can only add information, never promote
		// a finding to a higher severity than it claimed.
		{"high", domain.SeverityHigh},
		{"Medium", domain.SeverityMedium},
		// Still nothing unrecognised sneaks through as a real severity.
		{"catastrophic", domain.SeverityUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := p.convertMisconfig(Misconfig{ID: "x", Severity: tt.in}, "f.tf")
			if got.Priority != tt.want {
				t.Errorf("severity %q -> priority %q, want %q", tt.in, got.Priority, tt.want)
			}
		})
	}
}

func TestConvertMisconfig_IdentityTagsAndProperties(t *testing.T) {
	p := &Parser{}
	got := p.convertMisconfig(Misconfig{
		ID:         "AVD-AWS-0088",
		Type:       "Terraform Security Check",
		Title:      "S3 bucket not encrypted",
		Severity:   "HIGH",
		Resolution: "Enable SSE",
		PrimaryURL: "https://avd.aquasec.com/misconfig/avd-aws-0088",
		Status:     "FAIL",
	}, "terraform/main.tf")

	if got.ID != "AVD-AWS-0088" {
		t.Errorf("ID = %q, want the Trivy check ID", got.ID)
	}
	// The name has to carry severity, title, and target — it is what a reviewer scans.
	for _, want := range []string{"HIGH", "S3 bucket not encrypted", "terraform/main.tf"} {
		if !strings.Contains(got.Name, want) {
			t.Errorf("Name = %q, missing %q", got.Name, want)
		}
	}
	if got.ClassName != "terraform/main.tf" {
		t.Errorf("ClassName = %q, want the scanned target", got.ClassName)
	}

	joined := strings.Join(got.Tags, ",")
	for _, want := range []string{"misconfiguration", "severity:HIGH", "Terraform Security Check"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Tags = %v, missing %q", got.Tags, want)
		}
	}

	wantProps := map[string]string{
		"type":       "Terraform Security Check",
		"severity":   "HIGH",
		"resolution": "Enable SSE",
		"url":        "https://avd.aquasec.com/misconfig/avd-aws-0088",
	}
	for k, want := range wantProps {
		if got.Properties[k] != want {
			t.Errorf("Properties[%q] = %q, want %q", k, got.Properties[k], want)
		}
	}
}

// End to end: a report carrying only misconfigurations must produce cases, and the
// counters must reflect the failures rather than rolling up green.
func TestParse_MisconfigurationsOnly(t *testing.T) {
	p := &Parser{}
	report := `{
	  "SchemaVersion": 2,
	  "ArtifactName": "terraform/",
	  "Results": [{
	    "Target": "terraform/main.tf",
	    "Class": "config",
	    "Type": "terraform",
	    "Misconfigurations": [
	      {"ID":"AVD-AWS-0088","Type":"Terraform Security Check","Title":"S3 not encrypted",
	       "Message":"no encryption","Resolution":"enable SSE","Severity":"HIGH","Status":"FAIL"},
	      {"ID":"AVD-AWS-0089","Type":"Terraform Security Check","Title":"Logging enabled",
	       "Message":"ok","Severity":"LOW","Status":"PASS"}
	    ]
	  }]
	}`

	suite, err := p.Parse(strings.NewReader(report))
	if err != nil {
		t.Fatalf("Parse = %v", err)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(suite.Cases))
	}
	if suite.Failed != 1 || suite.Passed != 1 {
		t.Errorf("passed/failed = %d/%d, want 1/1", suite.Passed, suite.Failed)
	}
}
