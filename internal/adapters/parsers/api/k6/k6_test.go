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
