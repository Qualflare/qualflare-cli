package karate

import (
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

func TestConvertScenario_StatusPrecedence(t *testing.T) {
	p := &Parser{}
	tests := []struct {
		name     string
		scenario Scenario
		want     domain.Status
	}{
		{"passed", Scenario{Name: "s"}, domain.StatusPassed},
		{"failed", Scenario{Name: "s", Failed: true}, domain.StatusFailed},
		{"skipped", Scenario{Name: "s", Skipped: true}, domain.StatusSkipped},
		// Skipped is checked first, so a scenario flagged both ways reads as skipped.
		{"skipped wins over failed", Scenario{Name: "s", Skipped: true, Failed: true}, domain.StatusSkipped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.convertScenario(tt.scenario, Report{}); got.Status != tt.want {
				t.Errorf("Status = %q, want %q", got.Status, tt.want)
			}
		})
	}
}

func TestConvertScenario_StepStatusArms(t *testing.T) {
	p := &Parser{}
	scenario := Scenario{
		Name: "s",
		StepResults: []Step{
			{Step: "Given a thing", Result: "passed", DurationNanos: 1_000_000},
			{Step: "When it fails", Result: "failed", ErrorMessage: "assertion failed"},
			{Step: "Then skipped", Result: "skipped"},
			{Step: "And unknown", Result: "somethingElse"},
		},
	}

	got := p.convertScenario(scenario, Report{})
	if len(got.Steps) != 4 {
		t.Fatalf("Steps = %d, want 4", len(got.Steps))
	}
	wants := []domain.Status{
		domain.StatusPassed,
		domain.StatusFailed,
		domain.StatusSkipped,
		// An unrecognised step result is treated as skipped rather than passed.
		domain.StatusSkipped,
	}
	for i, want := range wants {
		if got.Steps[i].Status != want {
			t.Errorf("step %d (%q) = %q, want %q", i, got.Steps[i].Name, got.Steps[i].Status, want)
		}
	}
	if got.Steps[0].Duration != time.Millisecond {
		t.Errorf("step duration = %v, want 1ms from durationNanos", got.Steps[0].Duration)
	}
	if got.Steps[1].Error != "assertion failed" {
		t.Errorf("failed step error = %q", got.Steps[1].Error)
	}
}

// Hidden steps are Karate's internal bookkeeping and must not appear as test steps.
func TestConvertScenario_SkipsHiddenSteps(t *testing.T) {
	p := &Parser{}
	got := p.convertScenario(Scenario{
		Name: "s",
		StepResults: []Step{
			{Step: "visible", Result: "passed"},
			{Step: "internal", Result: "passed", Hidden: true},
			{Step: "also visible", Result: "passed"},
		},
	}, Report{})

	if len(got.Steps) != 2 {
		t.Fatalf("Steps = %d, want 2 with the hidden one dropped", len(got.Steps))
	}
	for _, s := range got.Steps {
		if s.Name == "internal" {
			t.Error("a hidden step leaked into the case")
		}
	}
}

// A hidden step that failed still must not contribute its message, since it was never
// surfaced as a step.
func TestConvertScenario_HiddenFailureDoesNotContributeError(t *testing.T) {
	p := &Parser{}
	got := p.convertScenario(Scenario{
		Name:   "s",
		Failed: true,
		StepResults: []Step{
			{Step: "hidden boom", Result: "failed", ErrorMessage: "hidden failure", Hidden: true},
			{Step: "real boom", Result: "failed", ErrorMessage: "real failure"},
		},
	}, Report{})

	if strings.Contains(got.Error, "hidden failure") {
		t.Errorf("Error = %q, must not include a hidden step's message", got.Error)
	}
	if !strings.Contains(got.Error, "real failure") {
		t.Errorf("Error = %q, want the visible step's message", got.Error)
	}
}

// Error aggregation: one message is used verbatim; several are folded into a
// message-plus-trace so the first stays the headline.
func TestConvertScenario_ErrorAggregation(t *testing.T) {
	p := &Parser{}

	t.Run("single message verbatim", func(t *testing.T) {
		got := p.convertScenario(Scenario{
			Name:        "s",
			Failed:      true,
			StepResults: []Step{{Result: "failed", ErrorMessage: "only failure"}},
		}, Report{})
		if got.Error != "only failure" {
			t.Errorf("Error = %q, want the message unchanged", got.Error)
		}
	})

	t.Run("multiple messages keep the first as headline", func(t *testing.T) {
		got := p.convertScenario(Scenario{
			Name:   "s",
			Failed: true,
			StepResults: []Step{
				{Result: "failed", ErrorMessage: "first"},
				{Result: "failed", ErrorMessage: "second"},
				{Result: "failed", ErrorMessage: "third"},
			},
		}, Report{})
		if !strings.HasPrefix(got.Error, "first") {
			t.Errorf("Error = %q, want it to lead with the first message", got.Error)
		}
		for _, want := range []string{"second", "third"} {
			if !strings.Contains(got.Error, want) {
				t.Errorf("Error = %q, missing %q", got.Error, want)
			}
		}
	})

	t.Run("failed with no messages leaves error empty", func(t *testing.T) {
		got := p.convertScenario(Scenario{
			Name:        "s",
			Failed:      true,
			StepResults: []Step{{Result: "failed"}},
		}, Report{})
		if got.Error != "" {
			t.Errorf("Error = %q, want empty when no step supplied a message", got.Error)
		}
	})

	t.Run("passing scenario ignores stray messages", func(t *testing.T) {
		got := p.convertScenario(Scenario{
			Name:        "s",
			StepResults: []Step{{Result: "failed", ErrorMessage: "stray"}},
		}, Report{})
		if got.Error != "" {
			t.Errorf("Error = %q, want empty for a scenario not marked failed", got.Error)
		}
	})
}

// BUG-07: tags must be a fresh slice per scenario. Sharing report.Tags' backing array
// let scenarios overwrite each other's tags across the loop.
func TestConvertScenario_TagsAreFreshPerScenario(t *testing.T) {
	p := &Parser{}
	report := Report{FeatureName: "login.feature", Tags: []string{"@feature"}}

	a := p.convertScenario(Scenario{Name: "a", Tags: []string{"@smoke"}}, report)
	b := p.convertScenario(Scenario{Name: "b", Tags: []string{"@slow"}}, report)

	if strings.Join(a.Tags, ",") != "@feature,@smoke" {
		t.Errorf("a.Tags = %v, want the report tag plus its own", a.Tags)
	}
	if strings.Join(b.Tags, ",") != "@feature,@slow" {
		t.Errorf("b.Tags = %v, want the report tag plus its own", b.Tags)
	}
	// The report's own slice must be untouched by either conversion.
	if len(report.Tags) != 1 || report.Tags[0] != "@feature" {
		t.Errorf("report.Tags = %v, want it unmodified", report.Tags)
	}
}

func TestConvertScenario_IdentityAndProperties(t *testing.T) {
	p := &Parser{}
	got := p.convertScenario(
		Scenario{Name: "logs in", DurationMillis: 250},
		Report{FeatureName: "auth.feature"},
	)

	if got.ID != "auth.feature_logs in" {
		t.Errorf("ID = %q, want feature_scenario", got.ID)
	}
	if got.Name != "logs in" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Duration != 250*time.Millisecond {
		t.Errorf("Duration = %v, want 250ms", got.Duration)
	}
	if got.Properties["feature"] != "auth.feature" {
		t.Errorf("Properties = %v, want the feature recorded", got.Properties)
	}
	// No steps still yields a non-nil slice, so the report marshals as [] not null.
	if got.Steps == nil {
		t.Error("Steps = nil, want an empty slice")
	}
}

// Karate emits either a bare report object or an array of them; both must parse, and a
// multi-report run accumulates duration across all of them.
func TestParse_SingleReportAndArray(t *testing.T) {
	p := &Parser{}

	single := `{"featureName":"a.feature","name":"A","durationMillis":100,
	  "scenarioResults":[{"name":"s1","durationMillis":100}]}`
	suite, err := p.Parse(strings.NewReader(single))
	if err != nil {
		t.Fatalf("Parse(single) = %v", err)
	}
	if len(suite.Cases) != 1 || suite.Name != "A" {
		t.Errorf("single: cases=%d name=%q", len(suite.Cases), suite.Name)
	}

	array := `[
	  {"featureName":"a.feature","name":"A","durationMillis":100,
	   "scenarioResults":[{"name":"s1"}]},
	  {"featureName":"b.feature","name":"B","durationMillis":150,
	   "scenarioResults":[{"name":"s2","failed":true}]}
	]`
	suite, err = p.Parse(strings.NewReader(array))
	if err != nil {
		t.Fatalf("Parse(array) = %v", err)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("array: cases = %d, want 2", len(suite.Cases))
	}
	if suite.Duration != 250*time.Millisecond {
		t.Errorf("Duration = %v, want 250ms summed across reports", suite.Duration)
	}
	if suite.Passed != 1 || suite.Failed != 1 {
		t.Errorf("passed/failed = %d/%d, want 1/1", suite.Passed, suite.Failed)
	}
	// The first named report supplies the suite name.
	if suite.Name != "A" {
		t.Errorf("Name = %q, want the first report's name", suite.Name)
	}
}
