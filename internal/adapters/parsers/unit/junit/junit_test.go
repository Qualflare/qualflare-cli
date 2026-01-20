package junit

import (
	"strings"

	"qualflare-cli/internal/core/domain"
	"testing"
)

func TestJUnitParserDefaultRetryCount(t *testing.T) {
	xmlReport := `
    <testsuites>
        <testsuite name="Test Suite" tests="1">
            <testcase name="test example" classname="ExampleTest">
            </testcase>
        </testsuite>
    </testsuites>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount 0, got %d", testCase.RetryCount)
	}
	if testCase.IsFlaky {
		t.Errorf("expected default IsFlaky false, got true")
	}
}

func TestJUnitParserExtractsRetryFromProperties(t *testing.T) {
	// Some tools add retry count as a property
	xmlReport := `
    <testsuites>
        <testsuite name="Test Suite" tests="1">
            <testcase name="flaky test" classname="ExampleTest">
                <properties>
                    <property name="retries" value="3"/>
                </properties>
            </testcase>
        </testsuite>
    </testsuites>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != 3 {
		t.Errorf("expected RetryCount 3 from property, got %d", testCase.RetryCount)
	}
	if !testCase.IsFlaky {
		t.Errorf("expected IsFlaky true when retries > 0 and passed, got false")
	}
}

func TestJUnitParserFailedTestWithRetries(t *testing.T) {
	// Test that fails after retries - should not be marked as flaky
	xmlReport := `
    <testsuites>
        <testsuite name="Test Suite" tests="1">
            <testcase name="failing test" classname="ExampleTest">
                <properties>
                    <property name="retries" value="2"/>
                </properties>
                <failure message="Test failed">AssertionError</failure>
            </testcase>
        </testsuite>
    </testsuites>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != 2 {
		t.Errorf("expected RetryCount 2 from property, got %d", testCase.RetryCount)
	}
	if testCase.IsFlaky {
		t.Errorf("expected IsFlaky false for failed test, got true")
	}
	if testCase.Status != domain.StatusFailed {
		t.Errorf("expected StatusFailed, got %s", testCase.Status)
	}
}
