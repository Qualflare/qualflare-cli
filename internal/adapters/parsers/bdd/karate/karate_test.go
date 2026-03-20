package karate

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestKarateParser_ParseScenarios(t *testing.T) {
	jsonReport := `[
    {
        "featureName": "users.feature",
        "name": "Users API",
        "durationMillis": 500,
        "passedCount": 1,
        "failedCount": 1,
        "scenarioCount": 2,
        "scenarioResults": [
            {
                "name": "Get user by ID",
                "durationMillis": 200,
                "failed": false,
                "skipped": false,
                "stepResults": [
                    {"step": "Given url 'http://localhost/users/1'", "line": 5, "durationNanos": 100000000, "result": "passed", "hidden": false},
                    {"step": "When method GET", "line": 6, "durationNanos": 50000000, "result": "passed", "hidden": false}
                ]
            },
            {
                "name": "Create user fails",
                "durationMillis": 300,
                "failed": true,
                "skipped": false,
                "stepResults": [
                    {"step": "Given url 'http://localhost/users'", "line": 10, "durationNanos": 100000000, "result": "passed", "hidden": false},
                    {"step": "When method POST", "line": 11, "durationNanos": 50000000, "result": "failed", "errorMessage": "status code was 500", "hidden": false}
                ]
            }
        ]
    }
]`

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

	for _, c := range suite.Cases {
		if c.Name == "Get user by ID" && c.Status != domain.StatusPassed {
			t.Errorf("expected Get user by ID to be passed, got %s", c.Status)
		}
		if c.Name == "Create user fails" {
			if c.Status != domain.StatusFailed {
				t.Errorf("expected Create user fails to be failed, got %s", c.Status)
			}
			if c.ErrorMessage == "" {
				t.Error("expected error message for failed scenario")
			}
		}
	}
}

func TestKarateParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestKarateParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
