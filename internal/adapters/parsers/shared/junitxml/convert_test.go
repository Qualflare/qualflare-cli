package junitxml

import (
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

// convertTestCase decides the status of every JUnit-family case, so its four arms are
// the single most load-bearing mapping in the shared parser.
func TestConvertTestCase_StatusArms(t *testing.T) {
	tests := []struct {
		name       string
		tc         TestCase
		wantStatus domain.Status
		wantInErr  []string
	}{
		{
			"failure",
			TestCase{Name: "t", Failure: &Failure{Message: "expected 1", Text: "at foo()", Type: "AssertionError"}},
			domain.StatusFailed,
			[]string{"AssertionError", "expected 1", "at foo()"},
		},
		{
			"error outranks nothing but failure",
			TestCase{Name: "t", Error: &Error{Message: "panic", Text: "stack", Type: "RuntimeError"}},
			domain.StatusError,
			[]string{"RuntimeError", "panic", "stack"},
		},
		{
			"skipped",
			TestCase{Name: "t", Skipped: &Skipped{Message: "not on windows"}},
			domain.StatusSkipped,
			[]string{"not on windows"},
		},
		{
			"passed",
			TestCase{Name: "t"},
			domain.StatusPassed,
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTestCase(tt.tc)
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			for _, want := range tt.wantInErr {
				if !strings.Contains(got.Error, want) {
					t.Errorf("Error = %q, missing %q", got.Error, want)
				}
			}
			if tt.wantInErr == nil && got.Error != "" {
				t.Errorf("Error = %q, want empty for a passing case", got.Error)
			}
		})
	}
}

// Failure wins when a case carries several outcome elements — a report that says both
// "failed" and "skipped" must not roll up as skipped.
func TestConvertTestCase_FailureTakesPrecedence(t *testing.T) {
	got := convertTestCase(TestCase{
		Name:    "t",
		Failure: &Failure{Message: "boom"},
		Error:   &Error{Message: "also boom"},
		Skipped: &Skipped{Message: "also skipped"},
	})
	if got.Status != domain.StatusFailed {
		t.Errorf("Status = %q, want %q", got.Status, domain.StatusFailed)
	}
	if !strings.Contains(got.Error, "boom") || strings.Contains(got.Error, "also boom") {
		t.Errorf("Error = %q, want the failure message only", got.Error)
	}
}

func TestConvertTestCase_Identity(t *testing.T) {
	got := convertTestCase(TestCase{Name: "handles login", Classname: "auth.LoginTest", Time: "1.5"})
	if got.Name != "handles login" || got.ClassName != "auth.LoginTest" {
		t.Errorf("name/class = %q/%q", got.Name, got.ClassName)
	}
	if got.ID != "auth.LoginTest.handles login" {
		t.Errorf("ID = %q, want classname.name", got.ID)
	}
	if got.Duration != 1500*time.Millisecond {
		t.Errorf("Duration = %v, want 1.5s", got.Duration)
	}
	// An unparseable time must leave the duration at zero rather than failing the case.
	if bad := convertTestCase(TestCase{Name: "t", Time: "not-a-number"}); bad.Duration != 0 {
		t.Errorf("Duration = %v for an unparseable time, want 0", bad.Duration)
	}
}

// Retry metadata drives flaky detection: a case that passed after one or more retries is
// flaky, one that passed first time is not, and a case with no retry property at all
// must stay nil so "unknown" is distinguishable from "zero retries".
func TestConvertTestCase_RetriesAndFlaky(t *testing.T) {
	tests := []struct {
		name      string
		props     []Property
		wantRetry *int
		wantFlaky *bool
	}{
		{"no property", nil, nil, nil},
		{"retries 2", []Property{{Name: "retries", Value: "2"}}, domain.IntPtr(2), domain.BoolPtr(true)},
		{"retryCount alias", []Property{{Name: "retryCount", Value: "3"}}, domain.IntPtr(3), domain.BoolPtr(true)},
		{"retries 0 is not flaky", []Property{{Name: "retries", Value: "0"}}, domain.IntPtr(0), domain.BoolPtr(false)},
		{"unrelated property ignored", []Property{{Name: "browser", Value: "chrome"}}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTestCase(TestCase{Name: "t", Properties: tt.props})

			switch {
			case tt.wantRetry == nil && got.RetryCount != nil:
				t.Errorf("RetryCount = %d, want nil", *got.RetryCount)
			case tt.wantRetry != nil && got.RetryCount == nil:
				t.Errorf("RetryCount = nil, want %d", *tt.wantRetry)
			case tt.wantRetry != nil && *got.RetryCount != *tt.wantRetry:
				t.Errorf("RetryCount = %d, want %d", *got.RetryCount, *tt.wantRetry)
			}

			switch {
			case tt.wantFlaky == nil && got.IsFlaky != nil:
				t.Errorf("IsFlaky = %v, want nil", *got.IsFlaky)
			case tt.wantFlaky != nil && got.IsFlaky == nil:
				t.Errorf("IsFlaky = nil, want %v", *tt.wantFlaky)
			case tt.wantFlaky != nil && *got.IsFlaky != *tt.wantFlaky:
				t.Errorf("IsFlaky = %v, want %v", *got.IsFlaky, *tt.wantFlaky)
			}
		})
	}
}

// A failing case is never flaky, even with retries recorded — flakiness means it
// eventually passed.
func TestConvertTestCase_FailedCaseIsNotFlaky(t *testing.T) {
	got := convertTestCase(TestCase{
		Name:       "t",
		Properties: []Property{{Name: "retries", Value: "2"}},
		Failure:    &Failure{Message: "still failing"},
	})
	if got.IsFlaky != nil {
		t.Errorf("IsFlaky = %v for a failing case, want nil", *got.IsFlaky)
	}
	if got.RetryCount == nil || *got.RetryCount != 2 {
		t.Error("RetryCount should still be recorded for a failing case")
	}
}

// SEC-04 depends on these landing in Properties under exactly these keys.
func TestConvertTestCase_CapturedOutput(t *testing.T) {
	got := convertTestCase(TestCase{Name: "t", SystemOut: "stdout here", SystemErr: "stderr here"})
	if got.Properties["system-out"] != "stdout here" || got.Properties["system-err"] != "stderr here" {
		t.Errorf("Properties = %v, want both captured streams", got.Properties)
	}
	// No captured output means no map at all, rather than an empty one.
	if plain := convertTestCase(TestCase{Name: "t"}); plain.Properties != nil {
		t.Errorf("Properties = %v, want nil when nothing was captured", plain.Properties)
	}
	// Only one stream present still produces just that key.
	only := convertTestCase(TestCase{Name: "t", SystemOut: "just stdout"})
	if len(only.Properties) != 1 || only.Properties["system-out"] != "just stdout" {
		t.Errorf("Properties = %v, want only system-out", only.Properties)
	}
}

// Mechanism C: generic <properties><property> pairs must reach domain.Case.Properties,
// not just captured stdout/stderr — pytest's record_property (and any other runner's
// custom <property> extension) relies on this passthrough.
func TestConvertTestCase_GenericPropertyPassthrough(t *testing.T) {
	got := convertTestCase(TestCase{
		Name:       "t",
		Properties: []Property{{Name: "browser", Value: "chrome"}},
	})
	if got.Properties["browser"] != "chrome" {
		t.Errorf("Properties[browser] = %q, want %q", got.Properties["browser"], "chrome")
	}
}

// Mechanism C's fallback: a <property name="shard" value="..."/> sets ShardIndex when no
// more specific mechanism (e.g. Playwright's native workerIndex) already has.
func TestConvertTestCase_ShardPropertyFallback(t *testing.T) {
	tests := []struct {
		name      string
		props     []Property
		wantShard *int
	}{
		{"valid integer", []Property{{Name: "shard", Value: "2"}}, domain.IntPtr(2)},
		{"non-numeric value", []Property{{Name: "shard", Value: "not-a-number"}}, nil},
		{"empty value", []Property{{Name: "shard", Value: ""}}, nil},
		{"whitespace-padded value", []Property{{Name: "shard", Value: " 5 "}}, domain.IntPtr(5)},
		{"no shard property", []Property{{Name: "browser", Value: "chrome"}}, nil},
		{"no properties at all", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTestCase(TestCase{Name: "t", Properties: tt.props})
			switch {
			case tt.wantShard == nil && got.ShardIndex != nil:
				t.Errorf("ShardIndex = %d, want nil", *got.ShardIndex)
			case tt.wantShard != nil && got.ShardIndex == nil:
				t.Errorf("ShardIndex = nil, want %d", *tt.wantShard)
			case tt.wantShard != nil && *got.ShardIndex != *tt.wantShard:
				t.Errorf("ShardIndex = %d, want %d", *got.ShardIndex, *tt.wantShard)
			}
		})
	}
}

// Captured stdout/stderr always win over a same-named <property> so a rogue property
// can't spoof what the runner actually captured.
func TestConvertTestCase_CapturedOutputWinsOverProperty(t *testing.T) {
	got := convertTestCase(TestCase{
		Name:       "t",
		SystemOut:  "real stdout",
		Properties: []Property{{Name: "system-out", Value: "fake"}},
	})
	if got.Properties["system-out"] != "real stdout" {
		t.Errorf("Properties[system-out] = %q, want the real captured output to win", got.Properties["system-out"])
	}
}

// BUG-13: prefer the suite-level time attribute, and fall back to summing the case
// durations when it is missing or unparseable — otherwise a whole suite reports as 0s.
func TestCollectSuite_DurationFallback(t *testing.T) {
	cases := []TestCase{{Name: "a", Time: "1"}, {Name: "b", Time: "2"}}

	tests := []struct {
		name      string
		suiteTime string
		want      time.Duration
	}{
		{"suite time wins", "10", 10 * time.Second},
		{"missing falls back to sum", "", 3 * time.Second},
		{"unparseable falls back to sum", "abc", 3 * time.Second},
		{"zero falls back to sum", "0", 3 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var total time.Duration
			var ts time.Time
			dst := &domain.Suite{Cases: []domain.Case{}}
			collectSuite(&TestSuite{Time: tt.suiteTime, TestCases: cases}, dst, &total, &ts)
			if total != tt.want {
				t.Errorf("total = %v, want %v", total, tt.want)
			}
		})
	}
}

// BUG-14: the first non-empty report timestamp wins, so upload wall-clock time is not
// stamped onto suites that told us when they actually ran.
func TestCollectSuite_TimestampLayouts(t *testing.T) {
	for _, in := range []string{
		"2026-01-02T15:04:05",
		"2026-01-02T15:04:05Z",
		"2026-01-02T15:04:05.123456789",
	} {
		t.Run(in, func(t *testing.T) {
			var total time.Duration
			var ts time.Time
			dst := &domain.Suite{Cases: []domain.Case{}}
			collectSuite(&TestSuite{Timestamp: in}, dst, &total, &ts)
			if ts.IsZero() {
				t.Errorf("timestamp %q was not parsed", in)
			}
			if ts.Year() != 2026 {
				t.Errorf("parsed year = %d, want 2026", ts.Year())
			}
		})
	}

	t.Run("unparseable leaves it unset", func(t *testing.T) {
		var total time.Duration
		var ts time.Time
		dst := &domain.Suite{Cases: []domain.Case{}}
		collectSuite(&TestSuite{Timestamp: "yesterday"}, dst, &total, &ts)
		if !ts.IsZero() {
			t.Errorf("ts = %v, want it left unset so the caller stamps upload time", ts)
		}
	})

	t.Run("first one wins", func(t *testing.T) {
		var total time.Duration
		ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		dst := &domain.Suite{Cases: []domain.Case{}}
		collectSuite(&TestSuite{Timestamp: "2026-01-02T15:04:05"}, dst, &total, &ts)
		if ts.Year() != 2020 {
			t.Errorf("ts year = %d, want the already-set 2020 to survive", ts.Year())
		}
	})
}

// CLI-H8: nested suites recurse, so tests inside them are not silently dropped. This
// checks depth beyond the two levels the existing XML test covers.
func TestCollectSuite_RecursesDeeply(t *testing.T) {
	deep := TestSuite{
		Name:      "root",
		TestCases: []TestCase{{Name: "l0"}},
		TestSuites: []TestSuite{{
			Name:      "mid",
			TestCases: []TestCase{{Name: "l1"}},
			TestSuites: []TestSuite{{
				Name:      "leaf",
				TestCases: []TestCase{{Name: "l2", Failure: &Failure{Message: "deep failure"}}},
			}},
		}},
	}

	var total time.Duration
	var ts time.Time
	dst := &domain.Suite{Cases: []domain.Case{}}
	collectSuite(&deep, dst, &total, &ts)

	if len(dst.Cases) != 3 {
		t.Fatalf("collected %d cases, want 3 across all nesting levels", len(dst.Cases))
	}
	var sawDeepFailure bool
	for _, c := range dst.Cases {
		if c.Name == "l2" && c.Status == domain.StatusFailed {
			sawDeepFailure = true
		}
	}
	if !sawDeepFailure {
		t.Error("the failure nested three levels deep was lost")
	}
}

// Parse accepts a bare <testsuite> root, not just a <testsuites> wrapper — many runners
// emit the former for a single-suite run.
func TestParse_SingleTestSuiteRoot(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="solo" tests="2" time="4">
  <testcase name="ok" classname="C" time="1"/>
  <testcase name="bad" classname="C" time="3"><failure message="nope">trace</failure></testcase>
</testsuite>`

	suite, err := Parse(strings.NewReader(xml), domain.FrameworkJUnit)
	if err != nil {
		t.Fatalf("Parse = %v", err)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(suite.Cases))
	}
	if suite.Failed != 1 || suite.Passed != 1 {
		t.Errorf("passed/failed = %d/%d, want 1/1", suite.Passed, suite.Failed)
	}
	if suite.Duration != 4*time.Second {
		t.Errorf("Duration = %v, want 4s from the suite attribute", suite.Duration)
	}
}

func TestParse_MalformedXMLErrors(t *testing.T) {
	if _, err := Parse(strings.NewReader("<testsuite><unclosed>"), domain.FrameworkJUnit); err == nil {
		t.Error("Parse of malformed XML = nil error, want a failure")
	}
}
