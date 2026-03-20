package pytest

import (
	"strings"
	"testing"
)

func TestPytestParserDefaultRetryCount(t *testing.T) {
	xmlReport := `
    <testsuite name="pytest" tests="1">
        <testcase name="test_example" classname="test_module">
        </testcase>
    </testsuite>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) == 0 {
		t.Fatal("expected at least one case")
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount 0, got %d", testCase.RetryCount)
	}
	if testCase.IsFlaky {
		t.Errorf("expected default IsFlaky false, got true")
	}
}

func TestPytestParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestPytestParser_MalformedXML(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("<not valid xml"))
	if err == nil {
		t.Error("expected error for malformed XML")
	}
}
