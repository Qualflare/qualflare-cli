package trivy

import (
	"strings"
	"testing"
)

// TestTrivyParser_SameCVEAcrossTargetsHasUniqueNames (SYNC-02) ensures the same
// CVE in the same package but two different scan targets produces two cases with
// DISTINCT names — the server dedupes cases by Name within a suite, so identical
// names would collapse N findings into one row.
func TestTrivyParser_SameCVEAcrossTargetsHasUniqueNames(t *testing.T) {
	report := `{
		"SchemaVersion": 2,
		"ArtifactName": "myimg",
		"Results": [
			{"Target": "app/Dockerfile", "Vulnerabilities": [
				{"VulnerabilityID": "CVE-1", "PkgName": "openssl", "Severity": "CRITICAL", "Title": "boom"}
			]},
			{"Target": "worker/Dockerfile", "Vulnerabilities": [
				{"VulnerabilityID": "CVE-1", "PkgName": "openssl", "Severity": "CRITICAL", "Title": "boom"}
			]}
		]
	}`
	suite, err := New().Parse(strings.NewReader(report))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(suite.Cases))
	}
	if suite.Cases[0].Name == suite.Cases[1].Name {
		t.Fatalf("two targets produced identical case names (would collapse server-side): %q", suite.Cases[0].Name)
	}
}
