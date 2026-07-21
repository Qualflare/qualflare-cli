package selenium

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestSeleniumParserDefaultRetryCount(t *testing.T) {
	jsonReport := `
    {
        "total": 1,
        "passed": 1,
        "failed": 0,
        "skipped": 0,
        "duration": 1.5,
        "suites": [{
            "name": "Login Suite",
            "tests": [{
                "name": "test example",
                "status": "passed",
                "duration": 1.5
            }]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) == 0 {
		t.Fatal("expected at least one case")
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected default IsFlaky nil or false, got true")
	}
}

// CLI-H5: Allure-style "broken" (and any unknown) status must fail-visible as
// StatusError with its error attached — never the old default:StatusPassed, which
// uploaded a broken/errored test as GREEN with its stack trace stripped.
func TestSeleniumParser_BrokenStatusMapsToError(t *testing.T) {
	jsonReport := `
    {
        "suites": [{
            "name": "Checkout Suite",
            "tests": [{
                "name": "broken test",
                "status": "broken",
                "duration": 2.0,
                "error": {
                    "message": "NoSuchElementException: unable to locate element",
                    "stackTrace": "at Checkout.pay(Checkout.java:42)",
                    "type": "NoSuchElementException"
                }
            }]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}

	testCase := suite.Cases[0]
	if testCase.Status != domain.StatusError {
		t.Errorf("expected broken status to map to StatusError, got %q", testCase.Status)
	}
	if !strings.Contains(testCase.Error, "NoSuchElementException") {
		t.Errorf("expected error trace to be attached, got %q", testCase.Error)
	}

	// The suite must not roll up green.
	if suite.Passed != 0 {
		t.Errorf("expected Passed=0, got %d", suite.Passed)
	}
	if suite.Errors != 1 {
		t.Errorf("expected Errors=1, got %d", suite.Errors)
	}
}

// CLI-H5: an unrecognized status must fail-visible as StatusError, not StatusPassed.
func TestSeleniumParser_UnknownStatusMapsToError(t *testing.T) {
	jsonReport := `
    {
        "suites": [{
            "name": "Weird Suite",
            "tests": [{
                "name": "mystery test",
                "status": "quantum-superposition",
                "duration": 0.1
            }]
        }]
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	if suite.Cases[0].Status != domain.StatusError {
		t.Errorf("expected unknown status to map to StatusError, got %q", suite.Cases[0].Status)
	}
	if suite.Passed != 0 {
		t.Errorf("expected Passed=0, got %d", suite.Passed)
	}
}

func TestSeleniumParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestSeleniumParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
