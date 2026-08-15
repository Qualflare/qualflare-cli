package phpunit

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestPHPUnitParser_ParsePassAndFail(t *testing.T) {
	xmlReport := `
<testsuites>
    <testsuite name="MyTests" tests="2" failures="1" errors="0" time="0.5">
        <testcase name="testSuccess" class="App\Tests\ExampleTest" classname="App.Tests.ExampleTest" file="/app/tests/ExampleTest.php" line="10" time="0.2">
        </testcase>
        <testcase name="testFailure" class="App\Tests\ExampleTest" classname="App.Tests.ExampleTest" file="/app/tests/ExampleTest.php" line="20" time="0.3">
            <failure type="PHPUnit\Framework\AssertionError">Expected true got false</failure>
        </testcase>
    </testsuite>
</testsuites>
`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected 2 total tests, got %d", suite.TotalTests)
	}
	if suite.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", suite.Passed)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", suite.Failed)
	}

	for _, c := range suite.Cases {
		if c.Name == "testSuccess" && c.Status != domain.StatusPassed {
			t.Errorf("expected testSuccess to be passed, got %s", c.Status)
		}
		if c.Name == "testFailure" {
			if c.Status != domain.StatusFailed {
				t.Errorf("expected testFailure to be failed, got %s", c.Status)
			}
			if c.Error == "" {
				t.Error("expected error message for failed test")
			}
		}
	}
}

// TestCase.Assertions was already decoded but never rolled up —
// domain.Suite.Assertions exists precisely for this and is already
// populated by newman/k6, just not phpunit. Summed per-case (not from
// TestSuite.Assertions, which nested suites would double-count) so nesting
// depth never affects the total.
func TestPHPUnitParser_SuiteAssertionsSummedAcrossCases(t *testing.T) {
	xmlReport := `
<testsuites>
    <testsuite name="MyTests" tests="2" failures="0" errors="0" time="0.5">
        <testcase name="testA" class="App\Tests\ExampleTest" assertions="3" time="0.2">
        </testcase>
        <testcase name="testB" class="App\Tests\ExampleTest" assertions="2" time="0.3">
        </testcase>
    </testsuite>
</testsuites>
`
	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Assertions != 5 {
		t.Errorf("Assertions = %d, want 5 (3+2 summed per case)", suite.Assertions)
	}
}

func TestPHPUnitParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestPHPUnitParser_MalformedXML(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("<not valid xml"))
	if err == nil {
		t.Error("expected error for malformed XML")
	}
}
