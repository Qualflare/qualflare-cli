package testcafe

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestTestCafeParser_ParseFixtureWithTests(t *testing.T) {
	jsonReport := `{
    "startTime": "2024-01-01T00:00:00.000Z",
    "endTime": "2024-01-01T00:00:05.000Z",
    "userAgents": ["Chrome 120"],
    "passed": 1,
    "failed": 1,
    "skipped": 0,
    "total": 2,
    "duration": 5000,
    "fixtures": [
        {
            "name": "Login Page",
            "path": "tests/login.js",
            "tests": [
                {
                    "name": "should login successfully",
                    "errs": [],
                    "durationMs": 2000,
                    "skipped": false
                },
                {
                    "name": "should show error on bad password",
                    "errs": [
                        {
                            "errMsg": "Expected element to be visible",
                            "stack": "at Selector (login.js:15)",
                            "isTestCafeError": true,
                            "code": "E24"
                        }
                    ],
                    "durationMs": 3000,
                    "skipped": false
                }
            ]
        }
    ],
    "warnings": []
}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected 2 total tests, got %d", suite.TotalTests)
	}
	if suite.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", suite.Passed)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", suite.Failed)
	}

	if len(suite.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(suite.Cases))
	}

	for _, c := range suite.Cases {
		if c.Name == "should login successfully" && c.Status != domain.StatusPassed {
			t.Errorf("expected passed status, got %s", c.Status)
		}
		if c.Name == "should show error on bad password" {
			if c.Status != domain.StatusFailed {
				t.Errorf("expected failed status, got %s", c.Status)
			}
			if c.ErrorMessage == "" {
				t.Error("expected error message for failed test")
			}
			if c.ErrorType != "E24" {
				t.Errorf("expected error type E24, got %s", c.ErrorType)
			}
		}
	}
}

func TestTestCafeParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestTestCafeParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
