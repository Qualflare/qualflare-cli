package maestro

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestMaestroParser_CategoryAndFramework(t *testing.T) {
	xml := `<testsuites><testsuite name="S" tests="1"><testcase name="t" classname="C"/></testsuite></testsuites>`
	p := New()

	if p.GetFramework() != domain.FrameworkMaestro {
		t.Errorf("expected FrameworkMaestro, got %s", p.GetFramework())
	}

	suite, err := p.Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if want := domain.FrameworkCategory(domain.FrameworkMaestro); suite.Category != want {
		t.Errorf("expected %q, got %s", want, suite.Category)
	}
}
