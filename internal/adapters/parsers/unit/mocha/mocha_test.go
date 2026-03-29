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
        "passes": [{
            "title": "example test",
            "fullTitle": "Suite example test",
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
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected default IsFlaky nil or false, got true")
	}
}

func TestMochaParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestMochaParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
