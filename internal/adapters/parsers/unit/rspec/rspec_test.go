package rspec

import (
	"strings"
	"testing"
)

func TestRspecParserDefaultRetryCount(t *testing.T) {
	jsonReport := `
    {
        "version": "3.12",
        "examples": [
            {
                "id": "./spec/example_spec.rb[1:1]",
                "description": "example test",
                "full_description": "Example example test",
                "status": "passed",
                "file_path": "./spec/example_spec.rb",
                "line_number": 5,
                "run_time": 0.001
            }
        ],
        "summary": {
            "duration": 0.001,
            "example_count": 1,
            "failure_count": 0,
            "pending_count": 0,
            "errors_outside_of_examples_count": 0
        },
        "summary_line": "1 example, 0 failures"
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
