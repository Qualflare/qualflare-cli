package rspec

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
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
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected default IsFlaky nil or false, got true")
	}
}

// BUG-17: a load-time error (spec_helper/rails_helper raising) makes RSpec report
// example_count 0, failure_count 0, but errors_outside_of_examples_count > 0. The
// parser used to ignore that field and upload an empty GREEN suite, hiding the
// failure. It must instead synthesize a failed case so the suite goes red.
func TestRspecParser_ErrorsOutsideOfExamples(t *testing.T) {
	jsonReport := `
    {
        "version": "3.12",
        "examples": [],
        "summary": {
            "duration": 0.05,
            "example_count": 0,
            "failure_count": 0,
            "pending_count": 0,
            "errors_outside_of_examples_count": 1
        },
        "summary_line": "0 examples, 0 failures, 1 error occurred outside of examples"
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) == 0 {
		t.Fatal("expected a synthesized case for the load-time error, got none (empty suite)")
	}

	if suite.Failed == 0 {
		t.Errorf("expected suite.Failed > 0 for a load-time error, got %d", suite.Failed)
	}

	if suite.GetStatus() != domain.StatusFailed {
		t.Errorf("expected suite status failed for a load-time error, got %s", suite.GetStatus())
	}

	// The synthesized case must carry the summary line so the failure is legible.
	found := false
	for _, c := range suite.Cases {
		if c.Status == domain.StatusFailed {
			found = true
			if !strings.Contains(c.Error, "outside of examples") {
				t.Errorf("expected synthesized case error to carry the summary line, got %q", c.Error)
			}
		}
	}
	if !found {
		t.Error("expected at least one failed case for the load-time error")
	}
}

// BUG-17: an unrecognized RSpec status must map to error (fail-visible), never
// skipped, so it can never be hidden from a red rollup.
func TestRspecParser_UnknownStatusIsError(t *testing.T) {
	jsonReport := `
    {
        "version": "3.12",
        "examples": [
            {
                "id": "./spec/example_spec.rb[1:1]",
                "description": "weird status test",
                "full_description": "Example weird status test",
                "status": "aborted",
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

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	if suite.Cases[0].Status != domain.StatusError {
		t.Errorf("expected unknown status to map to error, got %s", suite.Cases[0].Status)
	}
	if suite.GetStatus() != domain.StatusFailed {
		t.Errorf("expected suite to be red for an errored case, got %s", suite.GetStatus())
	}
}

func TestRspecParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestRspecParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
