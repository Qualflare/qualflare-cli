package rspec

import (
	"strings"
	"testing"
)

func TestRspecParserDefaultRetryCount(t *testing.T) {
	jsonReport := `
    {
        "messages": [
            {
                "message": "example test",
                "passed": true
            }
        ]
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
