package testcafe

import (
	"strings"
	"testing"
)

// TestTestCafeParser_UnstableIsFlaky (BUG-30) asserts a quarantine-mode
// (unstable) test is recorded as flaky structurally, not only as a tag.
func TestTestCafeParser_UnstableIsFlaky(t *testing.T) {
	jsonReport := `{
		"total":1,"passed":1,"failed":0,"skipped":0,
		"fixtures":[{"name":"F","path":"f.js","tests":[
			{"name":"flaky test","errs":[],"durationMs":100,"skipped":false,"unstable":true}
		]}]
	}`
	suite, err := New().Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(suite.Cases))
	}
	c := suite.Cases[0]
	if c.IsFlaky == nil || !*c.IsFlaky {
		t.Fatalf("unstable test must set IsFlaky=true, got %v", c.IsFlaky)
	}
}
