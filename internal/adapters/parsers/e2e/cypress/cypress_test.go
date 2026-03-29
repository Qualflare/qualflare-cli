package cypress

import (
	"strings"
	"testing"
)

func TestCypressParserExtractsRetryCount(t *testing.T) {
	// Mochawesome JSON with retry information
	jsonReport := `
    {
        "stats": {
            "tests": 1,
            "passes": 1,
            "failures": 0,
            "duration": 3000
        },
        "results": [{
            "file": "test.cy.js",
            "tests": [{
                "title": "flaky test",
                "state": "passed",
                "duration": 1000,
                "passes": true,
                "_retries": 2,
                "attempts": [
                    {"state": "failed", "duration": 1000},
                    {"state": "failed", "duration": 1000},
                    {"state": "passed", "duration": 1000}
                ]
            }]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount == nil || *testCase.RetryCount != 2 {
		t.Errorf("expected RetryCount 2, got %v", testCase.RetryCount)
	}
	if testCase.IsFlaky == nil || !*testCase.IsFlaky {
		t.Errorf("expected IsFlaky true, got false")
	}
}

func TestCypressParserNoRetries(t *testing.T) {
	jsonReport := `
    {
        "stats": {
            "tests": 1,
            "passes": 1,
            "failures": 0,
            "duration": 1000
        },
        "results": [{
            "file": "test.cy.js",
            "tests": [{
                "title": "stable test",
                "state": "passed",
                "duration": 1000,
                "passes": true
            }]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected IsFlaky nil or false, got true")
	}
}

func TestCypressParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestCypressParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
