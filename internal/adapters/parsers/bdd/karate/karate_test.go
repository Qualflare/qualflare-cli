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
			if c.Error == "" {
				t.Error("expected error message for failed scenario")
			}
		}
	}
}

// BUG-07: convertScenario aliased report.Tags' backing array via
// append(report.Tags, scenario.Tags...). When report.Tags has spare capacity
// (here 3 tags decode to len=3/cap=4), each scenario's append writes its own
// tag into the SAME backing slot, so the second scenario silently overwrites
// the first scenario's tag. Each scenario must keep its own tags (plus the
// shared report tags). The three shared @report* tags are what give the
// backing array spare capacity and expose the aliasing.
func TestKarateParser_ScenarioTagsNotAliased(t *testing.T) {
	jsonReport := `[
    {
        "featureName": "users.feature",
        "name": "Users API",
        "durationMillis": 500,
        "tags": ["@report1", "@report2", "@report3"],
        "scenarioResults": [
            {
                "name": "Scenario One",
                "durationMillis": 200,
                "failed": false,
                "skipped": false,
                "tags": ["@smoke"],
                "stepResults": [
                    {"step": "Given something", "line": 5, "durationNanos": 100000000, "result": "passed", "hidden": false}
                ]
            },
            {
                "name": "Scenario Two",
                "durationMillis": 300,
                "failed": false,
                "skipped": false,
                "tags": ["@regression"],
                "stepResults": [
                    {"step": "Given something else", "line": 10, "durationNanos": 100000000, "result": "passed", "hidden": false}
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

	tagsFor := func(name string) []string {
		for _, c := range suite.Cases {
			if c.Name == name {
				return c.Tags
			}
		}
		t.Fatalf("case %q not found", name)
		return nil
	}
	contains := func(tags []string, want string) bool {
		for _, tag := range tags {
			if tag == want {
				return true
			}
		}
		return false
	}

	one := tagsFor("Scenario One")
	if !contains(one, "@smoke") {
		t.Errorf("Scenario One should keep its own tag @smoke, got %v", one)
	}
	if !contains(one, "@report1") {
		t.Errorf("Scenario One should keep the shared report tags, got %v", one)
	}
	if contains(one, "@regression") {
		t.Errorf("Scenario One's tags were corrupted by Scenario Two (aliasing): got %v", one)
	}

	two := tagsFor("Scenario Two")
	if !contains(two, "@regression") {
		t.Errorf("Scenario Two should keep its own tag @regression, got %v", two)
	}
	if !contains(two, "@report1") {
		t.Errorf("Scenario Two should keep the shared report tags, got %v", two)
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
