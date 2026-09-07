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

// PHPUnit distinguishes <failure> (a failed assertion) from <error> (an
// exception, or a test PHPUnit marks risky). The rollup this replaces folded
// both into Failed and never touched Errors, so an errored run reported
// errors=0 and an inflated failure count -- disagreeing with PHPUnit's own
// header, which counts them separately, and with every other parser in the
// CLI, which derives its buckets from RecomputeCounts.
const phpunitMixedReport = `
<testsuites>
    <testsuite name="MyTests" tests="4" assertions="4" errors="1" failures="1" skipped="1" time="0.4">
        <testcase name="testPasses" class="App\Tests\T" classname="App.Tests.T" assertions="1" time="0.1"></testcase>
        <testcase name="testFails" class="App\Tests\T" classname="App.Tests.T" assertions="1" time="0.1">
            <failure type="PHPUnit\Framework\ExpectationFailedException">Failed asserting that false is true</failure>
        </testcase>
        <testcase name="testErrors" class="App\Tests\T" classname="App.Tests.T" assertions="1" time="0.1">
            <error type="RuntimeException">Connection refused</error>
        </testcase>
        <testcase name="testSkipped" class="App\Tests\T" classname="App.Tests.T" assertions="1" time="0.1">
            <skipped/>
        </testcase>
    </testsuite>
</testsuites>
`

func TestPHPUnitParser_AnErrorIsCountedAsAnErrorNotAFailure(t *testing.T) {
	suite, err := New().Parse(strings.NewReader(phpunitMixedReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.Errors != 1 {
		t.Errorf("expected Errors == 1 (the <error> case), got %d", suite.Errors)
	}
	if suite.Failed != 1 {
		t.Errorf("expected Failed == 1 (the <failure> case only), got %d", suite.Failed)
	}
	if suite.Passed != 1 {
		t.Errorf("expected Passed == 1, got %d", suite.Passed)
	}
	if suite.Skipped != 1 {
		t.Errorf("expected Skipped == 1, got %d", suite.Skipped)
	}
}

func TestPHPUnitParser_BucketsAccountForEveryCase(t *testing.T) {
	// The property that makes a summary trustworthy: nothing falls between the
	// buckets. A status the rollup does not handle used to vanish from the
	// totals while still inflating TotalTests.
	suite, err := New().Parse(strings.NewReader(phpunitMixedReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != len(suite.Cases) {
		t.Errorf("TotalTests %d != %d cases", suite.TotalTests, len(suite.Cases))
	}
	if sum := suite.Passed + suite.Failed + suite.Skipped + suite.Errors; sum != suite.TotalTests {
		t.Errorf("buckets sum to %d but TotalTests is %d", sum, suite.TotalTests)
	}
}

func TestPHPUnitParser_TheCasesOwnStatusIsUnchanged(t *testing.T) {
	// Only the rollup moved. The per-case status is what reaches the server,
	// and it must still distinguish error from failure.
	suite, err := New().Parse(strings.NewReader(phpunitMixedReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	want := map[string]domain.Status{
		"testPasses":  domain.StatusPassed,
		"testFails":   domain.StatusFailed,
		"testErrors":  domain.StatusError,
		"testSkipped": domain.StatusSkipped,
	}
	for _, c := range suite.Cases {
		if expected, ok := want[c.Name]; ok && c.Status != expected {
			t.Errorf("case %q: expected status %s, got %s", c.Name, expected, c.Status)
		}
		delete(want, c.Name)
	}
	for name := range want {
		t.Errorf("case %q missing from the report", name)
	}
}

func TestPHPUnitParser_AssertionsAreStillSummed(t *testing.T) {
	// Assertions is a PHPUnit-specific total with no equivalent in
	// RecomputeCounts, so it has to survive the move.
	suite, err := New().Parse(strings.NewReader(phpunitMixedReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Assertions != 4 {
		t.Errorf("expected 4 assertions summed across cases, got %d", suite.Assertions)
	}
}
