package rspecdoc

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

const worked = `User login
  Given the user is on the login page
    When the user enters valid credentials
      Then the user is redirected to the dashboard
    When the user enters invalid credentials
      Then an error message is shown (FAILED - 1)

Failures:

  1) User login Given the user is on the login page When the user enters invalid credentials Then an error message is shown
     Failure/Error: expect(page).to have_content("Invalid credentials")
       expected to find text "Invalid credentials" in "Welcome back"
     # ./spec/login_spec.rb:22

Finished in 0.15 seconds (files took 0.4 seconds to load)
2 examples, 1 failure
`

func TestExtractCases_NestedGivenWhenThen(t *testing.T) {
	e := New()
	cases, err := e.ExtractCases([]byte(worked))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases (2 leaves), got %d: %+v", len(cases), cases)
	}

	passing := cases[0]
	if passing.Status != domain.StatusPassed {
		t.Errorf("expected first case passed, got %v", passing.Status)
	}
	if len(passing.Steps) != 3 {
		t.Fatalf("expected 3 steps (Given/When/Then ancestors), got %d: %+v", len(passing.Steps), passing.Steps)
	}
	if passing.Steps[0].Keyword != "Given" || passing.Steps[1].Keyword != "When" || passing.Steps[2].Keyword != "Then" {
		t.Errorf("unexpected step keywords: %+v", passing.Steps)
	}
	if passing.Name != "User login Given the user is on the login page When the user enters valid credentials Then the user is redirected to the dashboard" {
		t.Errorf("unexpected case name (should match RSpec's own full description join): %q", passing.Name)
	}

	failing := cases[1]
	if failing.Status != domain.StatusFailed {
		t.Errorf("expected second case failed, got %v", failing.Status)
	}
	for _, s := range failing.Steps {
		if s.Status != domain.StatusFailed {
			t.Errorf("expected all steps of a failed example to be failed, got %+v", s)
		}
	}
	if !strings.Contains(failing.Error, "expect(page).to have_content") {
		t.Errorf("expected case Error to be populated from the Failures: section, got %q", failing.Error)
	}
}

func TestExtractCases_NonBDDWordingDegradesGracefully(t *testing.T) {
	input := `Calculator
  #add
    returns the sum of two numbers
    handles negative numbers

Finished in 0.02 seconds
2 examples, 0 failures
`
	e := New()
	cases, err := e.ExtractCases([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(cases))
	}
	for _, c := range cases {
		if len(c.Steps) != 0 {
			t.Errorf("non-BDD wording must produce zero steps, not an error: %+v", c.Steps)
		}
		if c.Status != domain.StatusPassed {
			t.Errorf("expected passed status even with no BDD steps, got %v", c.Status)
		}
		if c.Name == "" {
			t.Error("case must still be correctly named even without BDD steps")
		}
	}
}

func TestExtractCases_ANSIColorWrapped(t *testing.T) {
	input := "User login\n" +
		"  \x1b[32mGiven the user is on the login page\x1b[0m\n" +
		"    \x1b[32mThen the dashboard is shown\x1b[0m\n" +
		"\nFinished in 0.01 seconds\n1 example, 0 failures\n"
	e := New()
	cases, err := e.ExtractCases([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}
	if len(cases[0].Steps) != 2 {
		t.Fatalf("expected ANSI codes stripped before keyword matching, got steps: %+v", cases[0].Steps)
	}
}

// Real console output (wrapper noise like a shell banner, or a blank line a
// framework prints before its own output) can precede the tree with blank
// lines. Splitting the body/footer boundary on the FIRST blank line anywhere
// in the output — rather than anchoring to RSpec's own summary line —
// mistakes a leading blank line for the tree/footer boundary and discards
// the entire real tree into the unused footer. Mirrors the equivalent
// mochaspec regression.
func TestExtractCases_LeadingBlankLinesBeforeTreeDoNotConfuseTheSplit(t *testing.T) {
	input := "\n\n" + worked
	e := New()
	cases, err := e.ExtractCases([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases despite leading blank lines, got %d: %+v", len(cases), cases)
	}
}

func TestDetect(t *testing.T) {
	e := New()
	if !e.Detect([]byte(worked)) {
		t.Error("expected Detect to recognize the '2 examples, 1 failure' summary line")
	}
	if e.Detect([]byte("not rspec output at all\n")) {
		t.Error("expected Detect to reject non-RSpec output")
	}
}

func TestGetFramework(t *testing.T) {
	if New().GetFramework() != domain.FrameworkRSpec {
		t.Error("expected FrameworkRSpec")
	}
}
