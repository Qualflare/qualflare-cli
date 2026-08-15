package golang

import (
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

func TestGolangParser_ParsePassAndFail(t *testing.T) {
	input := `{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"pkg","Test":"TestPass"}
{"Time":"2024-01-01T00:00:01Z","Action":"output","Package":"pkg","Test":"TestPass","Output":"ok\n"}
{"Time":"2024-01-01T00:00:01Z","Action":"pass","Package":"pkg","Test":"TestPass","Elapsed":1.0}
{"Time":"2024-01-01T00:00:00Z","Action":"run","Package":"pkg","Test":"TestFail"}
{"Time":"2024-01-01T00:00:01Z","Action":"output","Package":"pkg","Test":"TestFail","Output":"FAIL: expected 1 got 2\n"}
{"Time":"2024-01-01T00:00:01Z","Action":"fail","Package":"pkg","Test":"TestFail","Elapsed":0.5}
{"Time":"2024-01-01T00:00:02Z","Action":"pass","Package":"pkg","Elapsed":1.5}
`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected 2 total tests, got %d", suite.TotalTests)
	}
	if suite.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", suite.Passed)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", suite.Failed)
	}

	// Check status mapping
	for _, c := range suite.Cases {
		if c.Name == "TestPass" && c.Status != domain.StatusPassed {
			t.Errorf("expected TestPass to be passed, got %s", c.Status)
		}
		if c.Name == "TestFail" {
			if c.Status != domain.StatusFailed {
				t.Errorf("expected TestFail to be failed, got %s", c.Status)
			}
			if c.Error == "" {
				t.Error("expected error message for failed test")
			}
		}
	}
}

// The "run" event's Time was already decoded but never surfaced —
// domain.Case.StartedAt exists precisely for this.
func TestGolangParser_CaseStartedAt(t *testing.T) {
	input := `{"Time":"2026-01-02T03:04:05Z","Action":"run","Package":"pkg","Test":"TestPass"}
{"Time":"2026-01-02T03:04:06Z","Action":"pass","Package":"pkg","Test":"TestPass","Elapsed":1.0}
`
	parser := New()
	suite, err := parser.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}

	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := suite.Cases[0].StartedAt
	if got == nil || !got.Equal(want) {
		t.Errorf("StartedAt = %v, want %v", got, want)
	}
}

// BUG-16: `go test ./...` emits one package-level pass/fail per package. The
// parser assigned instead of accumulated, so only the last package's elapsed
// time survived. Durations across packages must sum.
func TestGolangParser_MultiPackageDurationAccumulates(t *testing.T) {
	input := `{"Action":"run","Package":"pkgA","Test":"TestA"}
{"Action":"pass","Package":"pkgA","Test":"TestA","Elapsed":0.1}
{"Action":"pass","Package":"pkgA","Elapsed":1.0}
{"Action":"run","Package":"pkgB","Test":"TestB"}
{"Action":"pass","Package":"pkgB","Test":"TestB","Elapsed":0.2}
{"Action":"pass","Package":"pkgB","Elapsed":2.0}
`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	want := 3 * time.Second // 1.0s (pkgA) + 2.0s (pkgB)
	if suite.Duration != want {
		t.Errorf("expected accumulated duration %v, got %v", want, suite.Duration)
	}
}

// BUG-15: a test that panics/times out never emits a terminal pass/fail/skip
// event. It was silently dropped. In a QA product a dropped test is a cardinal
// sin — it must surface as an error case carrying its output.
func TestGolangParser_PanickedTestSurfacesAsError(t *testing.T) {
	input := `{"Action":"run","Package":"pkg","Test":"TestPanic"}
{"Action":"output","Package":"pkg","Test":"TestPanic","Output":"=== RUN   TestPanic\n"}
{"Action":"output","Package":"pkg","Test":"TestPanic","Output":"panic: boom\n"}
{"Action":"run","Package":"pkg","Test":"TestOK"}
{"Action":"pass","Package":"pkg","Test":"TestOK","Elapsed":0.1}
{"Action":"fail","Package":"pkg","Elapsed":0.5}
`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	var panicCase *domain.Case
	for i := range suite.Cases {
		if suite.Cases[i].Name == "TestPanic" {
			panicCase = &suite.Cases[i]
		}
	}
	if panicCase == nil {
		t.Fatalf("panicked test was dropped; cases=%+v", suite.Cases)
	}
	if panicCase.Status != domain.StatusError {
		t.Errorf("expected panicked test to be error, got %s", panicCase.Status)
	}
	if !strings.Contains(panicCase.Error, "panic") {
		t.Errorf("expected panic output preserved in error, got %q", panicCase.Error)
	}
	if suite.Errors < 1 {
		t.Errorf("expected at least 1 error in counts, got %d", suite.Errors)
	}
}

// BUG-35: Go reports a parent test and each of its subtests as separate events.
// The parent's Elapsed already includes the subtests, so counting the parent as
// its own case double-counts results. A parent with children must be skipped.
func TestGolangParser_SubtestParentNotDoubleCounted(t *testing.T) {
	input := `{"Action":"run","Package":"pkg","Test":"TestParent"}
{"Action":"run","Package":"pkg","Test":"TestParent/sub1"}
{"Action":"pass","Package":"pkg","Test":"TestParent/sub1","Elapsed":0.1}
{"Action":"run","Package":"pkg","Test":"TestParent/sub2"}
{"Action":"pass","Package":"pkg","Test":"TestParent/sub2","Elapsed":0.1}
{"Action":"pass","Package":"pkg","Test":"TestParent","Elapsed":0.5}
{"Action":"pass","Package":"pkg","Elapsed":0.6}
`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected 2 cases (subtests only), got %d", suite.TotalTests)
	}
	for _, c := range suite.Cases {
		if c.Name == "TestParent" {
			t.Errorf("parent test should not be counted as its own case")
		}
	}
}

// BUG-15 (part 2): a package-level failure with no failing test (e.g. build
// failure, TestMain failure) was turned into a parse error, dropping the signal.
// It must surface as a synthetic error case.
func TestGolangParser_PackageFailureSurfacesAsError(t *testing.T) {
	input := `{"Action":"start","Package":"pkg"}
{"Action":"output","Package":"pkg","Output":"FAIL\tpkg [build failed]\n"}
{"Action":"fail","Package":"pkg","Elapsed":0}
`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("expected package failure to surface, got error: %v", err)
	}

	if suite.Errors < 1 {
		t.Fatalf("expected a synthetic error case for the package failure, got errors=%d cases=%+v", suite.Errors, suite.Cases)
	}
}

func TestGolangParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestGolangParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
