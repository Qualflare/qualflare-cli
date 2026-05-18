package xctest

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestXCTestParser_CategoryAndFramework(t *testing.T) {
	xml := `<testsuites><testsuite name="S" tests="1"><testcase name="t" classname="C"/></testsuite></testsuites>`
	p := New()

	if p.GetFramework() != domain.FrameworkXCTest {
		t.Errorf("expected FrameworkXCTest, got %s", p.GetFramework())
	}

	suite, err := p.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Category != domain.CategoryE2E {
		t.Errorf("expected CategoryE2E, got %s", suite.Category)
	}
}
