package golang

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestGolangParser_ParsePassAndFail(t *testing.T) {
	input := `{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"pkg","Test":"TestPass"}
{"Time":"2024-01-01T00:00:01Z","Action":"output","Package":"pkg","Test":"TestPass","Output":"ok\n"}
{"Time":"2024-01-01T00:00:01Z","Action":"pass","Package":"pkg","Test":"TestPass","Elapsed":1.0}
{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"pkg","Test":"TestFail"}
{"Time":"2024-01-01T00:00:01Z","Action":"output","Package":"pkg","Test":"TestFail","Output":"FAIL: expected 1 got 2\n"}
{"Time":"2024-01-01T00:00:01Z","Action":"fail","Package":"pkg","Test":"TestFail","Elapsed":0.5}
{"Time":"2024-01-01T00:00:02Z","Action":"pass","Package":"pkg","Elapsed":1.5}
`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(input))
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

	// Check status mapping
	for _, c := range suite.Cases {
		if c.Name == "TestPass" && c.Status != domain.StatusPassed {
			t.Errorf("expected TestPass to be passed, got %s", c.Status)
		}
		if c.Name == "TestFail" {
			if c.Status != domain.StatusFailed {
				t.Errorf("expected TestFail to be failed, got %s", c.Status)
			}
			if c.ErrorMessage == "" {
				t.Error("expected error message for failed test")
			}
		}
	}
}

func TestGolangParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestGolangParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
