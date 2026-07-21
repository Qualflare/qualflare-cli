package jest

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestJestParserDefaultRetryCount(t *testing.T) {
	jsonReport := `
    {
        "numTotalTests": 1,
        "numPassedTests": 1,
        "numFailedTests": 0,
        "numPendingTests": 0,
        "success": true,
        "testResults": [{
            "name": "test.js",
            "assertionResults": [{
                "fullName": "test should pass",
                "status": "passed",
                "title": "should pass"
            }]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) == 0 {
		t.Fatal("expected at least one case")
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected default IsFlaky nil or false, got true")
	}
}

func TestJestParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestJestParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

// CLI-H9: a test file that throws at import/collection time yields a testResult
// with status "failed" and an EMPTY assertionResults array. The parser must
// synthesize a failed case so the crash cannot roll up as a green launch.
func TestJestParser_SuiteCrashEmptyAssertions(t *testing.T) {
	jsonReport := `
    {
        "numTotalTests": 0,
        "numPassedTests": 0,
        "numFailedTests": 0,
        "numPendingTests": 0,
        "numRuntimeErrorTestSuites": 1,
        "success": false,
        "startTime": 1700000000000,
        "testResults": [{
            "name": "broken.test.js",
            "status": "failed",
            "message": "Cannot find module './missing' from 'broken.test.js'",
            "assertionResults": []
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) == 0 {
		t.Fatal("CLI-H9: expected a synthesized failed case for the crashed suite, got zero cases")
	}
	if suite.Failed == 0 {
		t.Errorf("CLI-H9: expected suite.Failed > 0, got %d (suite rolled up green)", suite.Failed)
	}
	if suite.GetStatus() != domain.StatusFailed {
		t.Errorf("CLI-H9: expected suite status failed, got %s", suite.GetStatus())
	}
}

// BUG-36: "disabled" must map to skipped and any unknown status must map to
// error (fail-visible), never to passed.
func TestJestParser_DisabledAndUnknownStatus(t *testing.T) {
	jsonReport := `
    {
        "numTotalTests": 2,
        "success": true,
        "startTime": 1700000000000,
        "testResults": [{
            "name": "test.js",
            "status": "passed",
            "assertionResults": [
                {"fullName": "disabled test", "status": "disabled", "title": "disabled test"},
                {"fullName": "weird test", "status": "borked", "title": "weird test"}
            ]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	byName := map[string]domain.Status{}
	for _, c := range suite.Cases {
		byName[c.Name] = c.Status
	}

	if got := byName["disabled test"]; got != domain.StatusSkipped {
		t.Errorf("BUG-36: expected disabled test -> skipped, got %s", got)
	}
	if got := byName["weird test"]; got != domain.StatusError {
		t.Errorf("BUG-36: expected unknown status -> error, got %s", got)
	}
}

// BUG-37: with no startTime the suite timestamp must not fall back to the Unix
// epoch (1970) — it should be a real, recent time.
func TestJestParser_MissingStartTime(t *testing.T) {
	jsonReport := `
    {
        "numTotalTests": 1,
        "numPassedTests": 1,
        "success": true,
        "testResults": [{
            "name": "test.js",
            "status": "passed",
            "assertionResults": [{
                "fullName": "test should pass",
                "status": "passed",
                "title": "should pass"
            }]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.Timestamp.Year() < 2000 {
		t.Errorf("BUG-37: expected a real timestamp, got epoch-ish %v", suite.Timestamp)
	}
}
