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

// TestCucumberParser_FailingBackground reproduces CLI-H3: a failing Background is
// emitted by cucumber runners as a separate `background` element; the dependent
// scenario's own steps then all roll up to "skipped". If the parser skips
// `background` elements the failure vanishes and the scenario (and suite) report
// as skipped/green instead of red.
func TestCucumberParser_FailingBackground(t *testing.T) {
	jsonReport := `[
    {
        "uri": "features/checkout.feature",
        "id": "checkout-feature",
        "keyword": "Feature",
        "name": "Checkout",
        "elements": [
            {
                "id": "checkout-feature;background",
                "keyword": "Background",
                "name": "",
                "type": "background",
                "steps": [
                    {
                        "keyword": "Given ",
                        "name": "the payment gateway is available",
                        "line": 3,
                        "match": {"location": "steps.go:5"},
                        "result": {"status": "failed", "duration": 500000, "error_message": "gateway unreachable"}
                    }
                ]
            },
            {
                "id": "checkout-feature;purchase-item",
                "keyword": "Scenario",
                "name": "Purchase item",
                "type": "scenario",
                "steps": [
                    {
                        "keyword": "When ",
                        "name": "the user checks out",
                        "line": 7,
                        "match": {"location": "steps.go:20"},
                        "result": {"status": "skipped", "duration": 0}
                    },
                    {
                        "keyword": "Then ",
                        "name": "the order is confirmed",
                        "line": 8,
                        "match": {"location": "steps.go:30"},
                        "result": {"status": "skipped", "duration": 0}
                    }
                ]
            }
        ],
        "tags": []
    }
]`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 1 {
		t.Errorf("expected 1 total test, got %d", suite.TotalTests)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed (background step failed), got %d", suite.Failed)
	}
	if suite.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", suite.Skipped)
	}
	if got := suite.GetStatus(); got != domain.StatusFailed {
		t.Errorf("expected suite status failed, got %s", got)
	}

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	c := suite.Cases[0]
	if c.Status != domain.StatusFailed {
		t.Errorf("expected scenario to be failed due to failing background, got %s", c.Status)
	}
	if !strings.Contains(c.Error, "gateway unreachable") {
		t.Errorf("expected background error to be surfaced on the case, got %q", c.Error)
	}
	// Background step must be attached alongside the scenario's own steps.
	if len(c.Steps) != 3 {
		t.Errorf("expected 3 steps (1 background + 2 scenario), got %d", len(c.Steps))
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
