package mocha

import (
	"strings"
	"testing"
)

func TestMochaParserDefaultRetryCount(t *testing.T) {
	jsonReport := `
    {
        "stats": {
            "tests": 1,
            "passes": 1,
            "failures": 0
        },
        "tests": [{
            "title": "example test",
            "state": "passed",
            "duration": 100
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
