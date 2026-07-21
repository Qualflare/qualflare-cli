package junitxml

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

// TestParse_NestedTestSuiteCasesNotDropped (CLI-H8) proves that a failing test
// inside a nested <testsuite> is captured, not silently discarded. Before the
// fix the parser decoded only direct <testcase> children of a top-level suite,
// so the nested failure vanished and the launch reported all-green.
func TestParse_NestedTestSuiteCasesNotDropped(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="outer">
    <testcase name="outerPass" classname="Outer"/>
    <testsuite name="inner">
      <testcase name="innerFail" classname="Inner">
        <failure message="boom">stack</failure>
      </testcase>
    </testsuite>
  </testsuite>
</testsuites>`

	suite, err := Parse(strings.NewReader(xml), domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("got %d cases, want 2 (outerPass + innerFail); nested cases dropped", len(suite.Cases))
	}
	var sawFail bool
	for _, c := range suite.Cases {
		if c.Name == "innerFail" && c.Status == domain.StatusFailed {
			sawFail = true
		}
	}
	if !sawFail {
		t.Fatalf("nested failing case missing or not marked failed: %+v", suite.Cases)
	}
	if suite.Failed != 1 || suite.Passed != 1 {
		t.Fatalf("counts = passed:%d failed:%d, want passed:1 failed:1", suite.Passed, suite.Failed)
	}
	if suite.GetStatus() != domain.StatusFailed {
		t.Fatalf("suite status = %s, want failed", suite.GetStatus())
	}
}

// TestParse_EmptyDocumentHasNonNilCases (SYNC-06) ensures an empty report yields
// a non-nil (JSON `[]`, not `null`) Cases slice and a stamped Category, so it
// does not 400 the server's `required` validation and take a multi-file upload
// down with it.
func TestParse_EmptyDocumentHasNonNilCases(t *testing.T) {
	suite, err := Parse(strings.NewReader(`<?xml version="1.0"?><testsuites></testsuites>`), domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if suite.Cases == nil {
		t.Fatal("empty-document Cases is nil; must be a non-nil empty slice to marshal as []")
	}
	if suite.Category == "" {
		t.Fatal("empty-document Category is unset")
	}
}

// TestParse_NonUTF8Encoding (BUG-12) confirms a report declaring a non-UTF-8
// charset decodes instead of hard-erroring the whole upload.
func TestParse_NonUTF8Encoding(t *testing.T) {
	// "café" encoded as ISO-8859-1: é = 0xE9.
	body := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>" +
		"<testsuites><testsuite name=\"s\"><testcase name=\"caf\xe9\" classname=\"C\"/></testsuite></testsuites>"
	suite, err := Parse(strings.NewReader(body), domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("Parse rejected a non-UTF-8 report: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(suite.Cases))
	}
	if !strings.Contains(suite.Cases[0].Name, "caf") {
		t.Fatalf("decoded name = %q, want it to contain 'caf'", suite.Cases[0].Name)
	}
}
