package sonarqube

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestSonarQubeParser_ParseIssues(t *testing.T) {
	jsonReport := `{
    "total": 2,
    "p": 1,
    "ps": 100,
    "paging": {"pageIndex": 1, "pageSize": 100, "total": 2},
    "effortTotal": 30,
    "issues": [
        {
            "key": "issue-1",
            "rule": "java:S1234",
            "severity": "CRITICAL",
            "component": "com.example:MyClass.java",
            "project": "my-project",
            "line": 42,
            "status": "OPEN",
            "message": "Remove this unused variable",
            "effort": "5min",
            "type": "CODE_SMELL",
            "tags": ["convention"]
        },
        {
            "key": "issue-2",
            "rule": "java:S5678",
            "severity": "MINOR",
            "component": "com.example:OtherClass.java",
            "project": "my-project",
            "line": 10,
            "status": "OPEN",
            "message": "Add a comment",
            "effort": "2min",
            "type": "CODE_SMELL",
            "tags": ["documentation"]
        }
    ],
    "components": [
        {"key": "com.example:MyClass.java", "enabled": true, "qualifier": "FIL", "name": "MyClass.java", "longName": "src/main/java/com/example/MyClass.java", "path": "src/main/java/com/example/MyClass.java"},
        {"key": "com.example:OtherClass.java", "enabled": true, "qualifier": "FIL", "name": "OtherClass.java", "longName": "src/main/java/com/example/OtherClass.java", "path": "src/main/java/com/example/OtherClass.java"}
    ],
    "rules": [
        {"key": "java:S1234", "name": "Unused variables should be removed", "status": "READY", "lang": "java", "langName": "Java"},
        {"key": "java:S5678", "name": "Comments are required", "status": "READY", "lang": "java", "langName": "Java"}
    ]
}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected 2 total tests, got %d", suite.TotalTests)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed (CRITICAL), got %d", suite.Failed)
	}
	if suite.Passed != 1 {
		t.Errorf("expected 1 passed (MINOR), got %d", suite.Passed)
	}

	for _, c := range suite.Cases {
		if c.ID == "issue-1" {
			if c.Severity != domain.SeverityCritical {
				t.Errorf("expected critical severity, got %s", c.Severity)
			}
			if c.Status != domain.StatusFailed {
				t.Errorf("expected failed status for CRITICAL, got %s", c.Status)
			}
		}
		if c.ID == "issue-2" {
			if c.Severity != domain.SeverityLow {
				t.Errorf("expected low severity for MINOR, got %s", c.Severity)
			}
			if c.Status != domain.StatusPassed {
				t.Errorf("expected passed status for MINOR, got %s", c.Status)
			}
		}
	}
}

func TestSonarQubeParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestSonarQubeParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
