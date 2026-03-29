package cucumber

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestCucumberParser_ParseFeatureWithScenarios(t *testing.T) {
	jsonReport := `[
    {
        "uri": "features/login.feature",
        "id": "login-feature",
        "keyword": "Feature",
        "name": "Login",
        "elements": [
            {
                "id": "login-feature;valid-login",
                "keyword": "Scenario",
                "name": "Valid Login",
                "type": "scenario",
                "tags": [{"name": "@smoke", "line": 1}],
                "steps": [
                    {
                        "keyword": "Given ",
                        "name": "user is on login page",
                        "line": 5,
                        "match": {"location": "steps.go:10"},
                        "result": {"status": "passed", "duration": 1000000}
                    },
                    {
                        "keyword": "When ",
                        "name": "user enters credentials",
                        "line": 6,
                        "match": {"location": "steps.go:20"},
                        "result": {"status": "passed", "duration": 2000000}
                    }
                ]
            },
            {
                "id": "login-feature;invalid-login",
                "keyword": "Scenario",
                "name": "Invalid Login",
                "type": "scenario",
                "steps": [
                    {
                        "keyword": "Given ",
                        "name": "user is on login page",
                        "line": 10,
                        "match": {"location": "steps.go:10"},
                        "result": {"status": "passed", "duration": 1000000}
                    },
                    {
                        "keyword": "When ",
                        "name": "user enters bad credentials",
                        "line": 11,
                        "match": {"location": "steps.go:30"},
                        "result": {"status": "failed", "duration": 500000, "error_message": "assertion failed"}
                    }
                ]
            }
        ],
        "tags": [{"name": "@login", "line": 1}]
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
		if c.Name == "Login - Valid Login" {
			if c.Status != domain.StatusPassed {
				t.Errorf("expected Valid Login to be passed, got %s", c.Status)
			}
			if len(c.Steps) != 2 {
				t.Errorf("expected 2 steps, got %d", len(c.Steps))
			}
			// Check tags - should have both feature and scenario tags
			foundSmoke := false
			foundLogin := false
			for _, tag := range c.Tags {
				if tag == "smoke" {
					foundSmoke = true
				}
				if tag == "login" {
					foundLogin = true
				}
			}
			if !foundSmoke {
				t.Error("expected @smoke tag")
			}
			if !foundLogin {
				t.Error("expected @login tag")
			}
		}
		if c.Name == "Login - Invalid Login" {
			if c.Status != domain.StatusFailed {
				t.Errorf("expected Invalid Login to be failed, got %s", c.Status)
			}
			if c.Error == "" {
				t.Error("expected error message for failed scenario")
			}
		}
	}
}

func TestCucumberParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestCucumberParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
