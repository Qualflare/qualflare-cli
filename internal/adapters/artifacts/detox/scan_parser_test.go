package detox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qualflare-cli/internal/adapters/parsers/native/qualflare"
	"qualflare-cli/internal/adapters/parsers/unit/jest"
	"qualflare-cli/internal/core/domain"
)

// TestAttachMatchesARealJestReportThroughTheParser is the parser-to-matcher
// test the earlier task reviews never wrote. Every other test in this package
// hand-builds a domain.Case{Name: ...} directly, which is exactly why the
// Name-vs-ID defect (matching Detox directories against Case.Name, when the
// Jest parser puts the describe-qualified fullName in Case.ID and only the
// bare title in Case.Name) survived every one of them: a hand-built Case can
// set Name to whatever the test wants, but a REAL Jest report — which is what
// a Detox run actually produces — never does.
//
// This test parses a small Jest report with a describe block (so title !=
// fullName) through the real Jest parser, builds a Detox artifacts tree keyed
// off the fullName the way Detox actually names its directories, and asserts
// Attach finds it.
func TestAttachMatchesARealJestReportThroughTheParser(t *testing.T) {
	const jestJSON = `{
		"numTotalTests": 1,
		"numPassedTests": 1,
		"numFailedTests": 0,
		"numPendingTests": 0,
		"numTodoTests": 0,
		"numTotalTestSuites": 1,
		"numRuntimeErrorTestSuites": 0,
		"startTime": 1700000000000,
		"success": true,
		"testResults": [
			{
				"name": "e2e/login.test.js",
				"status": "passed",
				"startTime": 1700000000000,
				"endTime": 1700000001000,
				"assertionResults": [
					{
						"ancestorTitles": ["Login"],
						"fullName": "Login should sign in",
						"status": "passed",
						"title": "should sign in",
						"duration": 1000,
						"failureMessages": [],
						"failureDetails": []
					}
				]
			}
		]
	}`

	suite, err := jest.New().Parse(strings.NewReader(jestJSON))
	if err != nil {
		t.Fatalf("parsing the Jest report: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(suite.Cases))
	}
	// Confirm the shape this test exists to exercise: ID carries the
	// describe-qualified fullName, Name carries only the bare title. Matching
	// on Name alone (the pre-fix behaviour) could never find the directory
	// below, because Detox never named anything "should sign in".
	if suite.Cases[0].ID != "Login should sign in" {
		t.Fatalf("Case.ID = %q, want the fullName %q", suite.Cases[0].ID, "Login should sign in")
	}
	if suite.Cases[0].Name != "should sign in" {
		t.Fatalf("Case.Name = %q, want the bare title %q", suite.Cases[0].Name, "should sign in")
	}

	// Detox builds its directory name from fullName, not title.
	run := t.TempDir()
	dir := filepath.Join(run, DirectoryName(suite.Cases[0].ID, "passed", 1))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "testDone.png"), []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	launch := &domain.Launch{Suites: []domain.Suite{*suite}}

	matched, unmatched, unreadable, err := Attach(launch, run)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(unmatched) != 0 {
		t.Errorf("unmatched = %v, want none", unmatched)
	}
	if len(unreadable) != 0 {
		t.Errorf("unreadable = %v, want none", unreadable)
	}
	if matched != 1 {
		t.Fatalf("matched = %d, want 1: a real parsed Jest case must match its Detox directory by fullName (Case.ID)", matched)
	}
	if len(launch.Suites[0].Cases[0].Attachments) != 1 {
		t.Fatalf("got %d attachments, want the one screenshot Detox wrote", len(launch.Suites[0].Cases[0].Attachments))
	}
	if got := launch.Suites[0].Cases[0].Attachments[0].Name; got != "testDone.png" {
		t.Errorf("attachment name = %q, want %q", got, "testDone.png")
	}
}

// TestAttachMatchesAQualflareJSONReportFromTheDetoxReporter is the other half of
// the test above, and the half whose absence shipped a feature that matched
// nothing at all.
//
// The test above proves the CLI's own Jest parser works, where Case.ID IS the
// fullName. But a Detox user does not produce that file — they produce
// qualflare-json, via @qualflare/detox (which is @qualflare/jest's reporter).
// That format carries fullName in Name and a FILE-QUALIFIED
// "<file>#<fullName>" in ID:
//
//	id   = "e2e/login.test.js#Login should sign in"
//	name = "Login should sign in"
//
// Matching on ID computed the directory name `✓ e2e_login.test.js#Login should
// sign in`, which Detox never wrote, so a real end-to-end run attached zero
// artifacts and warned that every directory was unmatched. Both parsers are now
// covered, and neither field is privileged — see candidateMatchKeys.
//
// The report body below is the real output of @qualflare/jest, trimmed: the IDs
// and names are copied verbatim from a run of it against a describe block whose
// test names exercise the sanitiser (a slash, quotes, a colon, a dollar).
func TestAttachMatchesAQualflareJSONReportFromTheDetoxReporter(t *testing.T) {
	const report = `{
		"framework": "detox",
		"platform": "android",
		"suites": [
			{
				"name": "e2e/login.test.js",
				"cases": [
					{"id": "e2e/login.test.js#Login should sign in", "name": "Login should sign in", "status": "passed"},
					{"id": "e2e/login.test.js#Login should handle a/b paths", "name": "Login should handle a/b paths", "status": "failed"},
					{"id": "e2e/login.test.js#Login costs $5", "name": "Login costs $5", "status": "passed"}
				]
			}
		]
	}`

	suite, err := qualflare.New().Parse(strings.NewReader(report))
	if err != nil {
		t.Fatalf("parsing the qualflare-json report: %v", err)
	}
	if len(suite.Cases) != 3 {
		t.Fatalf("got %d cases, want 3", len(suite.Cases))
	}
	// Pin the shape this test exists for: a NON-EMPTY, file-qualified ID. The
	// pre-fix code fell back to Name only when ID was empty, which never
	// happened here — that is precisely why the defect was invisible.
	if got := suite.Cases[0].ID; got != "e2e/login.test.js#Login should sign in" {
		t.Fatalf("Case.ID = %q, want the file-qualified id this format actually emits", got)
	}
	if got := suite.Cases[0].Name; got != "Login should sign in" {
		t.Fatalf("Case.Name = %q, want the fullName", got)
	}

	// Directory names as Detox's own constructSafeFilename produces them —
	// verified by running detox@20.51.4's module against these exact names.
	run := t.TempDir()
	for _, d := range []string{
		"✓ Login should sign in",
		"✗ Login should handle a_b paths",
		"✓ Login costs _5",
	} {
		dir := filepath.Join(run, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "test.png"), []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	launch := &domain.Launch{Suites: []domain.Suite{*suite}}
	matched, unmatched, unreadable, err := Attach(launch, run)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(unreadable) != 0 {
		t.Errorf("unreadable = %v, want none", unreadable)
	}
	if len(unmatched) != 0 {
		t.Errorf("unmatched = %v, want none — every directory here belongs to a case", unmatched)
	}
	if matched != 3 {
		t.Fatalf("matched = %d, want 3: a qualflare-json report from @qualflare/detox must match by fullName (Case.Name), not by its file-qualified ID", matched)
	}
	for i := range launch.Suites[0].Cases {
		if n := len(launch.Suites[0].Cases[i].Attachments); n != 1 {
			t.Errorf("case %q got %d attachments, want 1", launch.Suites[0].Cases[i].Name, n)
		}
	}
}
