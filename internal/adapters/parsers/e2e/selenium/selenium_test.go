package selenium

import (
	"strings"
	"testing"
)

func TestSeleniumParserDefaultRetryCount(t *testing.T) {
	jsonReport := `
    {
        "total": 1,
        "passed": 1,
        "failed": 0,
        "skipped": 0,
        "duration": 1.5,
        "suites": [{
            "name": "Login Suite",
            "tests": [{
                "name": "test example",
                "status": "passed",
                "duration": 1.5
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
