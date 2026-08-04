package newman

import (
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

func exec(id string, assertions ...Assertion) Execution {
	return Execution{
		Item:       Item{ID: id, Name: "GET /" + id},
		Request:    Request{Method: "GET"},
		Response:   Response{Code: 200, ResponseTime: 42},
		Assertions: assertions,
	}
}

// BUG-06 is the whole point of this function: a request whose assertions were all
// skipped was never actually verified, so it must not pass. A request with genuinely
// zero assertions is a different case and still passes.
func TestConvertExecution_SkippedAssertions(t *testing.T) {
	p := &Parser{}
	tests := []struct {
		name string
		e    Execution
		want domain.Status
	}{
		{
			"all assertions skipped is not a pass",
			exec("a", Assertion{Assertion: "status is 200", Skipped: true}),
			domain.StatusSkipped,
		},
		{
			"every one of several skipped",
			exec("a",
				Assertion{Assertion: "one", Skipped: true},
				Assertion{Assertion: "two", Skipped: true},
			),
			domain.StatusSkipped,
		},
		{
			"no assertions at all still passes",
			exec("a"),
			domain.StatusPassed,
		},
		{
			"one ran and passed, one skipped",
			exec("a",
				Assertion{Assertion: "ran"},
				Assertion{Assertion: "skipped", Skipped: true},
			),
			domain.StatusPassed,
		},
		{
			"a skipped assertion does not mask a real failure",
			exec("a",
				Assertion{Assertion: "skipped", Skipped: true},
				Assertion{Assertion: "failed", Error: &Error{Message: "expected 200"}},
			),
			domain.StatusFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.convertExecution(tt.e, nil); got.Status != tt.want {
				t.Errorf("Status = %q, want %q", got.Status, tt.want)
			}
		})
	}
}

func TestConvertExecution_AssertionErrors(t *testing.T) {
	p := &Parser{}
	got := p.convertExecution(exec("a",
		Assertion{Assertion: "first", Error: &Error{Message: "first failed"}},
		Assertion{Assertion: "second", Error: &Error{Message: "second failed"}},
	), nil)

	if got.Status != domain.StatusFailed {
		t.Fatalf("Status = %q, want failed", got.Status)
	}
	// The first message is the headline.
	if !strings.Contains(got.Error, "first failed") {
		t.Errorf("Error = %q, want it to lead with the first failure", got.Error)
	}
}

// The run-level failure map is a second source of failures, keyed by item ID. A request
// listed there fails even when its own assertions all looked fine.
func TestConvertExecution_FailureMap(t *testing.T) {
	p := &Parser{}

	t.Run("failure map fails an otherwise-passing request", func(t *testing.T) {
		failures := map[string][]Failure{
			"a": {{Error: Error{Message: "run-level failure", Stack: "at assertion (line 3)"}}},
		}
		got := p.convertExecution(exec("a", Assertion{Assertion: "looked fine"}), failures)

		if got.Status != domain.StatusFailed {
			t.Errorf("Status = %q, want failed", got.Status)
		}
		for _, want := range []string{"run-level failure", "at assertion (line 3)"} {
			if !strings.Contains(got.Error, want) {
				t.Errorf("Error = %q, missing %q", got.Error, want)
			}
		}
	})

	t.Run("first stack wins across several failures", func(t *testing.T) {
		failures := map[string][]Failure{
			"a": {
				{Error: Error{Message: "one", Stack: "first stack"}},
				{Error: Error{Message: "two", Stack: "second stack"}},
			},
		}
		got := p.convertExecution(exec("a"), failures)
		if !strings.Contains(got.Error, "first stack") || strings.Contains(got.Error, "second stack") {
			t.Errorf("Error = %q, want only the first stack retained", got.Error)
		}
	})

	t.Run("a failure for another item is ignored", func(t *testing.T) {
		failures := map[string][]Failure{"other": {{Error: Error{Message: "not ours"}}}}
		got := p.convertExecution(exec("a", Assertion{Assertion: "fine"}), failures)
		if got.Status != domain.StatusPassed {
			t.Errorf("Status = %q, want passed — the failure belongs to another item", got.Status)
		}
	})

	t.Run("empty failure list for the item still fails it", func(t *testing.T) {
		// The key being present is what marks the request as failed, so an empty
		// slice must not silently pass.
		failures := map[string][]Failure{"a": {}}
		got := p.convertExecution(exec("a"), failures)
		if got.Status != domain.StatusFailed {
			t.Errorf("Status = %q, want failed when the item is keyed in the failure map", got.Status)
		}
		if got.Error != "" {
			t.Errorf("Error = %q, want empty when no message accompanied the failure", got.Error)
		}
	})
}

func TestConvertExecution_IdentityAndProperties(t *testing.T) {
	p := &Parser{}
	e := Execution{
		Item:     Item{ID: "req-1", Name: "Create user"},
		Request:  Request{Method: "POST"},
		Response: Response{Code: 201, ResponseTime: 137},
	}

	got := p.convertExecution(e, nil)

	if got.ID != "req-1" || got.Name != "Create user" {
		t.Errorf("ID/Name = %q/%q", got.ID, got.Name)
	}
	if got.Duration != 137*time.Millisecond {
		t.Errorf("Duration = %v, want 137ms from responseTime", got.Duration)
	}
	if got.Properties["method"] != "POST" {
		t.Errorf("method = %q", got.Properties["method"])
	}
	if got.Properties["responseCode"] != "201" {
		t.Errorf("responseCode = %q, want %q", got.Properties["responseCode"], "201")
	}
	if got.Properties["responseTime"] != "137ms" {
		t.Errorf("responseTime = %q, want %q", got.Properties["responseTime"], "137ms")
	}
}
