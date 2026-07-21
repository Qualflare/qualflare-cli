package k6

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestK6Parser_ParseChecks(t *testing.T) {
	jsonReport := `{
    "root_group": {
        "name": "",
        "path": "",
        "id": "root",
        "groups": [],
        "checks": [
            {
                "name": "status is 200",
                "path": "::status is 200",
                "id": "check-1",
                "passes": 100,
                "fails": 0
            },
            {
                "name": "body contains data",
                "path": "::body contains data",
                "id": "check-2",
                "passes": 0,
                "fails": 50
            }
        ]
    },
    "options": {"summaryTrendStats": ["avg", "p(95)"]},
    "state": {"isStdOutTTY": true, "isStdErrTTY": true, "testRunDurationMs": 5000},
    "metrics": {}
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

	// Verify assertion totals (passes + fails across all checks)
	if suite.Assertions != 150 {
		t.Errorf("expected 150 assertions (100+0+0+50), got %d", suite.Assertions)
	}

	for _, c := range suite.Cases {
		if c.Name == "status is 200" && c.Status != domain.StatusPassed {
			t.Errorf("expected status is 200 to be passed, got %s", c.Status)
		}
		if c.Name == "body contains data" {
			if c.Status != domain.StatusFailed {
				t.Errorf("expected body contains data to be failed, got %s", c.Status)
			}
			if c.Error == "" {
				t.Error("expected error message for all-fail check")
			}
		}
	}
}

// TestK6Parser_DuplicateCheckTitlesAcrossGroups reproduces BUG-05: two checks
// that share the same title in different groups must yield two cases with
// distinct Names. The server dedupes cases by Name within a suite, so a bare
// check title collides and one check is silently merged away (a QA false-green).
func TestK6Parser_DuplicateCheckTitlesAcrossGroups(t *testing.T) {
	jsonReport := `{
    "root_group": {
        "name": "",
        "path": "",
        "id": "root",
        "groups": [
            {
                "name": "Login Flow",
                "path": "::Login Flow",
                "id": "group-1",
                "groups": [],
                "checks": [
                    {
                        "name": "status is 200",
                        "path": "::Login Flow::status is 200",
                        "id": "check-login",
                        "passes": 10,
                        "fails": 0
                    }
                ]
            },
            {
                "name": "Checkout Flow",
                "path": "::Checkout Flow",
                "id": "group-2",
                "groups": [],
                "checks": [
                    {
                        "name": "status is 200",
                        "path": "::Checkout Flow::status is 200",
                        "id": "check-checkout",
                        "passes": 5,
                        "fails": 0
                    }
                ]
            }
        ],
        "checks": []
    },
    "options": {"summaryTrendStats": ["avg", "p(95)"]},
    "state": {"isStdOutTTY": true, "isStdErrTTY": true, "testRunDurationMs": 3000},
    "metrics": {}
}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(suite.Cases))
	}

	names := make(map[string]int)
	for _, c := range suite.Cases {
		names[c.Name]++
	}

	if len(names) != 2 {
		t.Errorf("expected 2 distinct case names (checks in different groups must not collide), got %d: %v", len(names), names)
	}
	if names["Login Flow > status is 200"] != 1 {
		t.Errorf("expected a case named 'Login Flow > status is 200', names were: %v", names)
	}
	if names["Checkout Flow > status is 200"] != 1 {
		t.Errorf("expected a case named 'Checkout Flow > status is 200', names were: %v", names)
	}
}

func TestK6Parser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestK6Parser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
