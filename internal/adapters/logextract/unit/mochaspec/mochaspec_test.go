package mochaspec

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

const worked = `User login
  given valid credentials
    when submitting the form
      ✔ then redirects to the dashboard (45ms)
  given invalid credentials
    when submitting the form
      1) then shows an error message


  1 passing (120ms)
  1 failing

  1) User login
       given invalid credentials
         when submitting the form
           then shows an error message:

      AssertionError: expected 'Welcome' to include 'Invalid credentials'
      at Context.<anonymous> (test/login.test.js:22:34)
`

func TestExtractCases_PassingAndFailingLeaves(t *testing.T) {
	e := New()
	cases, err := e.ExtractCases([]byte(worked))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d: %+v", len(cases), cases)
	}

	passing := cases[0]
	if passing.Status != domain.StatusPassed {
		t.Errorf("expected passed, got %v", passing.Status)
	}
	if len(passing.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d: %+v", len(passing.Steps), passing.Steps)
	}
	if passing.Steps[0].Keyword != "given" || passing.Steps[1].Keyword != "when" || passing.Steps[2].Keyword != "then" {
		t.Errorf("unexpected step keywords (case-insensitive match, lowercase as written): %+v", passing.Steps)
	}
	if passing.Steps[2].Name != "redirects to the dashboard" {
		t.Errorf("expected checkmark+duration stripped from leaf step name, got %q", passing.Steps[2].Name)
	}

	failing := cases[1]
	if failing.Status != domain.StatusFailed {
		t.Errorf("expected failed, got %v", failing.Status)
	}
	for _, s := range failing.Steps {
		if s.Status != domain.StatusFailed {
			t.Errorf("expected all steps of a failing case to be failed, got %+v", s)
		}
	}
	if !strings.Contains(failing.Error, "AssertionError: expected 'Welcome'") {
		t.Errorf("expected case Error populated from the numbered failure block, got %q", failing.Error)
	}
}

// Real Mocha spec-reporter output (and wrapper noise like npm/npx banners)
// can precede the tree with blank lines — Mocha itself prints a leading
// blank line before the tree. Splitting the body/footer boundary on the
// FIRST blank line anywhere in the output (rather than anchoring to the
// summary line) mistakes that leading blank line for the tree/footer
// boundary, discarding the entire real tree into the unused "footer". This
// reproduces that exact shape, captured from a real `npx mocha` run.
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
	if cases[0].Status != domain.StatusPassed || cases[1].Status != domain.StatusFailed {
		t.Errorf("unexpected statuses: %v, %v", cases[0].Status, cases[1].Status)
	}
}

// A non-blank noise line before the tree (e.g. an npm/npx banner, followed
// by its own blank line) is not "a leading blank line" — the earlier fix
// for that only skips a leading run of *blank* lines. Since the noise line
// itself is non-blank, the boundary search still stops at the very next
// blank line (the one after the noise), so the noise becomes "body"
// (fabricating a fake case from it) while the entire real tree is discarded
// into the unused footer.
func TestExtractCases_NonBlankNoiseBeforeTreeDoesNotConfuseTheSplit(t *testing.T) {
	input := "npm warn deprecated foo@1.0.0: bar\n\n" + worked
	e := New()
	cases, err := e.ExtractCases([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 real cases despite leading noise, got %d: %+v", len(cases), cases)
	}
	for _, c := range cases {
		if strings.Contains(c.Name, "npm warn") {
			t.Errorf("noise line leaked into a case name: %q", c.Name)
		}
	}
}

func TestExtractCases_PendingLeafIsSkipped(t *testing.T) {
	input := `Calculator
  - adds two numbers


  0 passing (5ms)
  1 pending
`
	e := New()
	cases, err := e.ExtractCases([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}
	if cases[0].Status != domain.StatusSkipped {
		t.Errorf("expected a '- ' prefixed leaf to map to skipped, got %v", cases[0].Status)
	}
}

func TestDetect(t *testing.T) {
	e := New()
	if !e.Detect([]byte(worked)) {
		t.Error("expected Detect to recognize Mocha's 'N passing (Nms)' summary line")
	}
	if e.Detect([]byte("not mocha output\n")) {
		t.Error("expected Detect to reject non-Mocha output")
	}
}

func TestGetFramework(t *testing.T) {
	if New().GetFramework() != domain.FrameworkMocha {
		t.Error("expected FrameworkMocha")
	}
}
