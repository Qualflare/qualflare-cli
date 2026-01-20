package cypress

import (
	"strings"

	"qualflare-cli/internal/core/domain"
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
	if testCase.RetryCount != 2 {
		t.Errorf("expected RetryCount 2, got %d", testCase.RetryCount)
	}
	if !testCase.IsFlaky {
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
	if testCase.RetryCount != 0 {
		t.Errorf("expected RetryCount 0, got %d", testCase.RetryCount)
	}
	if testCase.IsFlaky {
		t.Errorf("expected IsFlaky false, got true")
	}
}
