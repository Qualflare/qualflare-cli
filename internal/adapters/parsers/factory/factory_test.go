package factory

import (
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestParserFactory_GetParser_KnownFramework(t *testing.T) {
	f := NewParserFactory()

	parser, err := f.GetParser(domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("expected no error for JUnit, got: %v", err)
	}
	if parser == nil {
		t.Fatal("expected non-nil parser for JUnit")
	}
	if parser.GetFramework() != domain.FrameworkJUnit {
		t.Errorf("expected JUnit framework, got %s", parser.GetFramework())
	}
}

func TestParserFactory_GetParser_UnknownFramework(t *testing.T) {
	f := NewParserFactory()

	_, err := f.GetParser(domain.Framework("nonexistent"))
	if err == nil {
		t.Error("expected error for unknown framework")
	}
}

func TestParserFactory_DetectFramework_FilenamePatterns(t *testing.T) {
	f := NewParserFactory()

	tests := []struct {
		filename  string
		framework domain.Framework
	}{
		{"trivy-report.json", domain.FrameworkTrivy},
		{"snyk-results.json", domain.FrameworkSnyk},
		{"zap-scan.json", domain.FrameworkZAP},
		{"owasp-results.json", domain.FrameworkZAP},
		{"sonar-issues.json", domain.FrameworkSonarQube},
		{"playwright-results.json", domain.FrameworkPlaywright},
		{"cypress-report.json", domain.FrameworkCypress},
		{"mochawesome.json", domain.FrameworkCypress},
		{"testcafe-report.json", domain.FrameworkTestCafe},
		{"newman-results.json", domain.FrameworkNewman},
		{"postman-results.json", domain.FrameworkNewman},
		{"k6-summary.json", domain.FrameworkK6},
		{"jest-results.json", domain.FrameworkJest},
		{"mocha-results.json", domain.FrameworkMocha},
		{"rspec-output.json", domain.FrameworkRSpec},
		{"phpunit-results.xml", domain.FrameworkPHPUnit},
		{"pytest-results.xml", domain.FrameworkPython},
		{"cucumber-report.json", domain.FrameworkCucumber},
		{"karate-summary.json", domain.FrameworkKarate},
		{"results.xml", domain.FrameworkJUnit}, // default for XML
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			fw, err := f.DetectFramework(tt.filename)
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tt.filename, err)
			}
			if fw != tt.framework {
				t.Errorf("for %s: expected %s, got %s", tt.filename, tt.framework, fw)
			}
		})
	}
}

func TestParserFactory_GetSupportedFrameworks(t *testing.T) {
	f := NewParserFactory()
	frameworks := f.GetSupportedFrameworks()

	if len(frameworks) == 0 {
		t.Fatal("expected at least one supported framework")
	}

	// Verify all expected frameworks are present
	expectedFrameworks := []domain.Framework{
		domain.FrameworkJUnit,
		domain.FrameworkPython,
		domain.FrameworkGolang,
		domain.FrameworkJest,
		domain.FrameworkMocha,
		domain.FrameworkRSpec,
		domain.FrameworkPHPUnit,
		domain.FrameworkCucumber,
		domain.FrameworkKarate,
		domain.FrameworkPlaywright,
		domain.FrameworkCypress,
		domain.FrameworkSelenium,
		domain.FrameworkTestCafe,
		domain.FrameworkNewman,
		domain.FrameworkK6,
		domain.FrameworkZAP,
		domain.FrameworkTrivy,
		domain.FrameworkSnyk,
		domain.FrameworkSonarQube,
	}

	frameworkSet := make(map[domain.Framework]bool)
	for _, fw := range frameworks {
		frameworkSet[fw] = true
	}

	for _, expected := range expectedFrameworks {
		if !frameworkSet[expected] {
			t.Errorf("expected framework %s to be supported", expected)
		}
	}
}
