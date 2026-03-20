package jest

import (
	"strings"
	"testing"
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
	if testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount 0, got %d", testCase.RetryCount)
	}
	if testCase.IsFlaky {
		t.Errorf("expected default IsFlaky false, got true")
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
