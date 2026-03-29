package newman

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestNewmanParser_ParseExecutionWithAssertions(t *testing.T) {
	jsonReport := `{
    "collection": {
        "info": {
            "name": "My API Tests",
            "description": ""
        }
    },
    "run": {
        "stats": {
            "iterations": {"total": 1, "pending": 0, "failed": 0},
            "items": {"total": 1, "pending": 0, "failed": 0},
            "scripts": {"total": 2, "pending": 0, "failed": 0},
            "prerequests": {"total": 1, "pending": 0, "failed": 0},
            "requests": {"total": 1, "pending": 0, "failed": 0},
            "tests": {"total": 2, "pending": 0, "failed": 1},
            "assertions": {"total": 2, "pending": 0, "failed": 1},
            "testScripts": {"total": 1, "pending": 0, "failed": 0}
        },
        "timings": {
            "responseAverage": 100,
            "started": 1700000000000,
            "completed": 1700000001000
        },
        "executions": [
            {
                "item": {"id": "req-1", "name": "Get Users"},
                "request": {"url": "http://localhost/users", "method": "GET"},
                "response": {"code": 200, "status": "OK", "responseTime": 100, "responseSize": 512},
                "assertions": [
                    {"assertion": "Status code is 200", "skipped": false},
                    {"assertion": "Response has users", "skipped": false, "error": {"name": "AssertionError", "index": 1, "test": "Response has users", "message": "expected array to have length 5", "stack": "at line 10"}}
                ]
            }
        ],
        "failures": []
    }
}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected 2 total tests (assertions), got %d", suite.TotalTests)
	}
	if suite.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", suite.Passed)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", suite.Failed)
	}

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 execution case, got %d", len(suite.Cases))
	}

	c := suite.Cases[0]
	if c.Name != "Get Users" {
		t.Errorf("expected case name 'Get Users', got '%s'", c.Name)
	}
	if c.Status != domain.StatusFailed {
		t.Errorf("expected failed status, got %s", c.Status)
	}
	if c.Error == "" {
		t.Error("expected error message for failed assertion")
	}
}

func TestNewmanParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestNewmanParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
