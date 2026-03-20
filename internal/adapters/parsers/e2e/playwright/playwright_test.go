package playwright

import (
	"strings"
	"testing"
)

func TestPlaywrightParserExtractsRetryCount(t *testing.T) {
	// Playwright JSON with retries (test fails twice, passes on 3rd try)
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "line": 1,
            "specs": [{
                "title": "flaky test",
                "tests": [{
                    "results": [
                        {"status": "failed", "duration": 1000, "retry": 0},
                        {"status": "failed", "duration": 1000, "retry": 1},
                        {"status": "passed", "duration": 1000, "retry": 2}
                    ],
                    "status": "passed"
                }]
            }]
        }],
        "stats": {"flaky": 1}
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

	testCase := suite.Cases[0]
	if testCase.RetryCount != 2 {
		t.Errorf("expected RetryCount 2, got %d", testCase.RetryCount)
	}
	if !testCase.IsFlaky {
		t.Errorf("expected IsFlaky true, got false")
	}
}

func TestPlaywrightParserNoRetries(t *testing.T) {
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [{
                "title": "stable test",
                "tests": [{
                    "results": [
                        {"status": "passed", "duration": 1000, "retry": 0}
                    ],
                    "status": "passed"
                }]
            }]
        }],
        "stats": {"flaky": 0}
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

func TestPlaywrightParserFailedAfterRetries(t *testing.T) {
	// Test that fails even after retries - should not be marked as flaky
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [{
                "title": "failing test",
                "tests": [{
                    "results": [
                        {"status": "failed", "duration": 1000, "retry": 0},
                        {"status": "failed", "duration": 1000, "retry": 1}
                    ],
                    "status": "failed"
                }]
            }]
        }],
        "stats": {"flaky": 0}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != 1 {
		t.Errorf("expected RetryCount 1, got %d", testCase.RetryCount)
	}
	if testCase.IsFlaky {
		t.Errorf("expected IsFlaky false for failed test, got true")
	}
}

func TestPlaywrightParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestPlaywrightParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
