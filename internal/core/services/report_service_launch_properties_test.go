package services

import (
	"reflect"
	"testing"

	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
)

func TestPromoteConsistentSuiteProperties(t *testing.T) {
	tests := []struct {
		name   string
		suites []domain.Suite
		keys   []string
		want   map[string]string
	}{
		{
			"all suites agree on a key",
			[]domain.Suite{
				{Properties: map[string]string{"browser": "chromium"}},
				{Properties: map[string]string{"browser": "chromium"}},
			},
			[]string{"browser"},
			map[string]string{"browser": "chromium"},
		},
		{
			"suites disagree — key omitted, never guessed",
			[]domain.Suite{
				{Properties: map[string]string{"browser": "chromium"}},
				{Properties: map[string]string{"browser": "firefox"}},
			},
			[]string{"browser"},
			map[string]string{},
		},
		{
			"a single suite's value is promoted",
			[]domain.Suite{
				{Properties: map[string]string{"browser": "webkit"}},
			},
			[]string{"browser"},
			map[string]string{"browser": "webkit"},
		},
		{
			"key unset everywhere is omitted",
			[]domain.Suite{
				{Properties: map[string]string{}},
				{Properties: nil},
			},
			[]string{"browser"},
			map[string]string{},
		},
		{
			"no suites at all yields an empty map",
			nil,
			[]string{"browser"},
			map[string]string{},
		},
		{
			"multiple keys handled independently",
			[]domain.Suite{
				{Properties: map[string]string{"browser": "chromium", "platform": "linux"}},
				{Properties: map[string]string{"browser": "chromium", "platform": "macos"}},
			},
			[]string{"browser", "platform"},
			map[string]string{"browser": "chromium"},
		},
		{
			"a suite that doesn't set the key at all is simply not counted as disagreement",
			[]domain.Suite{
				{Properties: map[string]string{"browser": "chromium"}},
				{Properties: map[string]string{}},
			},
			[]string{"browser"},
			map[string]string{"browser": "chromium"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := promoteConsistentSuiteProperties(tt.suites, tt.keys...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// End-to-end: a realistic multi-suite Selenium-style report (every suite
// already carrying browser/platform in its own Properties, per
// selenium.go's "for Launch to use" comment) ends up with Launch.Properties
// AND the pre-existing but previously-dead Launch.Browser field populated.
func TestCreateReport_PromotesConsistentBrowserAndPlatformToLaunch(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewReportService(nil, nil, cfg)

	suites := []domain.Suite{
		{Name: "Login", Properties: map[string]string{"browser": "chromium", "platform": "linux"}},
		{Name: "Checkout", Properties: map[string]string{"browser": "chromium", "platform": "linux"}},
	}

	report := s.createReport(suites, domain.FrameworkSelenium)

	if report.Properties["browser"] != "chromium" {
		t.Errorf("Launch.Properties[browser] = %q, want chromium", report.Properties["browser"])
	}
	if report.Properties["platform"] != "linux" {
		t.Errorf("Launch.Properties[platform] = %q, want linux", report.Properties["platform"])
	}
	if report.Browser != "chromium" {
		t.Errorf("Launch.Browser = %q, want chromium (same root cause, same fix)", report.Browser)
	}
	// Suite-level values must survive untouched — this is additive, not a move.
	if report.Suites[0].Properties["browser"] != "chromium" {
		t.Error("Suite.Properties[browser] must be left in place, not removed by promotion")
	}
}

func TestCreateReport_DisagreeingBrowsersLeavesLaunchLevelUnset(t *testing.T) {
	cfg := config.DefaultConfig()
	s := NewReportService(nil, nil, cfg)

	suites := []domain.Suite{
		{Name: "Login", Properties: map[string]string{"browser": "chromium"}},
		{Name: "Checkout", Properties: map[string]string{"browser": "firefox"}},
	}

	report := s.createReport(suites, domain.FrameworkSelenium)

	if _, ok := report.Properties["browser"]; ok {
		t.Errorf("Launch.Properties[browser] = %q, want absent for a genuine multi-browser run", report.Properties["browser"])
	}
	if report.Browser != "" {
		t.Errorf("Launch.Browser = %q, want empty for a genuine multi-browser run", report.Browser)
	}
}
