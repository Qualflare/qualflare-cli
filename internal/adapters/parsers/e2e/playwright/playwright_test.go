package playwright

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
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
	if testCase.RetryCount == nil || *testCase.RetryCount != 2 {
		t.Errorf("expected RetryCount 2, got %v", testCase.RetryCount)
	}
	if testCase.IsFlaky == nil || !*testCase.IsFlaky {
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
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected IsFlaky nil or false, got true")
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
	if testCase.RetryCount == nil || *testCase.RetryCount != 1 {
		t.Errorf("expected RetryCount 1, got %v", testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected IsFlaky nil or false for failed test, got true")
	}
}

func TestPlaywrightParserInterruptedStatusIsError(t *testing.T) {
	// CLI-H4: Playwright emits "interrupted" on --max-failures / SIGINT. It fell
	// through the status switch's `default` and was uploaded as PASSED — an
	// interrupted (aborted) test must surface as an error, never a green pass.
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [{
                "title": "interrupted test",
                "tests": [{
                    "results": [
                        {"status": "interrupted", "duration": 500, "retry": 0}
                    ],
                    "status": "unexpected"
                }]
            }]
        }],
        "stats": {}
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
	if got := suite.Cases[0].Status; got != domain.StatusError {
		t.Errorf("expected StatusError for interrupted result, got %q", got)
	}
	if suite.Errors != 1 {
		t.Errorf("expected suite.Errors 1, got %d", suite.Errors)
	}
	if suite.Passed != 0 {
		t.Errorf("expected suite.Passed 0, got %d", suite.Passed)
	}
}

func TestPlaywrightParserExpectedFailureIsPassed(t *testing.T) {
	// BUG-09: A test.fail() test has expectedStatus "failed"; when it actually
	// fails (result status == expectedStatus) that is the designed outcome and
	// must be recorded as PASSED (preserving the message), not a real failure.
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [{
                "title": "known-broken feature",
                "ok": true,
                "tests": [{
                    "expectedStatus": "failed",
                    "results": [
                        {"status": "failed", "duration": 800, "retry": 0,
                         "error": {"message": "boom", "stack": "at x"}}
                    ],
                    "status": "expected"
                }]
            }]
        }],
        "stats": {}
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
	if testCase.Status != domain.StatusPassed {
		t.Errorf("expected StatusPassed for expected-failure, got %q", testCase.Status)
	}
	if testCase.Error == "" {
		t.Error("expected the failure message to be preserved, got empty Error")
	}
	if suite.Passed != 1 {
		t.Errorf("expected suite.Passed 1, got %d", suite.Passed)
	}
	if suite.Failed != 0 {
		t.Errorf("expected suite.Failed 0, got %d", suite.Failed)
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
