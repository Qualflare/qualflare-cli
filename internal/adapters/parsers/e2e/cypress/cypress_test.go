package cypress

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
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

// BUG-08: a mochawesome `state:"pending"` test (an it.skip) must map to
// domain.StatusSkipped, not StatusPending. The server ranks pending above passed,
// so a single it.skip would otherwise flip the whole suite's rolled-up status to
// pending. Mocha semantics: pending == skipped.
func TestCypressParser_ItSkipMapsToSkipped(t *testing.T) {
	jsonReport := `
    {
        "stats": {
            "tests": 2,
            "passes": 1,
            "failures": 0,
            "pending": 1,
            "duration": 500
        },
        "results": [{
            "file": "spec.cy.js",
            "tests": [
                {"title": "runs", "state": "passed", "duration": 100, "uuid": "p1"},
                {"title": "is skipped", "state": "pending", "pending": true, "duration": 0, "uuid": "p2"}
            ]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(suite.Cases))
	}
	skip := suite.Cases[1]
	if skip.Status != domain.StatusSkipped {
		t.Errorf("it.skip: expected StatusSkipped, got %q", skip.Status)
	}
	// A pending status would leak through and out-rank passed on the server.
	if skip.Status == domain.StatusPending {
		t.Errorf("it.skip must not map to StatusPending")
	}
}

// BUG-29: the skip count must come from the real cases, not a misread header.
// The old code summed `pending` + a nonexistent `skippedRegistered` stats field,
// so a report that reported its skips via the real `skipped` field rolled up with
// suite.Skipped == 0 while a genuinely-skipped case was present (a hidden skip).
func TestCypressParser_SuiteSkipCountReflectsCases(t *testing.T) {
	jsonReport := `
    {
        "stats": {
            "tests": 2,
            "passes": 1,
            "failures": 0,
            "pending": 0,
            "skipped": 1,
            "duration": 500
        },
        "results": [{
            "file": "spec.cy.js",
            "tests": [
                {"title": "runs", "state": "passed", "duration": 100, "uuid": "s1"},
                {"title": "registered but skipped", "state": "", "skipped": true, "duration": 0, "uuid": "s2"}
            ]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected TotalTests 2, got %d", suite.TotalTests)
	}
	if suite.Passed != 1 {
		t.Errorf("expected Passed 1, got %d", suite.Passed)
	}
	if suite.Skipped != 1 {
		t.Errorf("expected Skipped 1 (reflecting the report's skipped test), got %d", suite.Skipped)
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
