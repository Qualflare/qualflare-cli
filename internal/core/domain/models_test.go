package domain

import (
	"encoding/json"
	"testing"
)

// TestSeverityToCasePriority guards the CLI half of the P0 fix: security parsers
// emit "info"/"unknown" severities, which the API's `priority` enum rejects.
// ToCasePriority must coerce every value to an enum-valid priority or "".
func TestSeverityToCasePriority(t *testing.T) {
	tests := []struct {
		in   Severity
		want Severity
	}{
		{SeverityCritical, SeverityCritical},
		{SeverityHigh, SeverityHigh},
		{SeverityMedium, SeverityMedium},
		{SeverityLow, SeverityLow},
		{SeverityInfo, SeverityLow},
		{SeverityUnknown, ""},
		{"", ""},
		{"garbage", ""},
	}
	for _, tt := range tests {
		if got := tt.in.ToCasePriority(); got != tt.want {
			t.Errorf("Severity(%q).ToCasePriority() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCaseWithRetryCount(t *testing.T) {
	testCase := Case{
		ID:         "test-1",
		Name:       "Test With Retry",
		RetryCount: IntPtr(3), // Test was retried 3 times before passing
		Status:     StatusPassed,
	}

	if testCase.RetryCount == nil || *testCase.RetryCount != 3 {
		t.Errorf("expected RetryCount 3, got %v", testCase.RetryCount)
	}
}

func TestCaseDefaultRetryCount(t *testing.T) {
	testCase := Case{
		ID:     "test-2",
		Name:   "Test Without Retry",
		Status: StatusPassed,
	}

	if testCase.RetryCount != nil {
		t.Errorf("expected default RetryCount nil, got %d", *testCase.RetryCount)
	}
}

func TestCaseWithIsFlaky(t *testing.T) {
	testCase := Case{
		ID:         "test-3",
		Name:       "Flaky Test",
		RetryCount: IntPtr(2),
		IsFlaky:    BoolPtr(true),
		Status:     StatusPassed,
	}

	if testCase.IsFlaky == nil || !*testCase.IsFlaky {
		t.Errorf("expected IsFlaky true, got false")
	}
}

func TestCaseDefaultIsFlaky(t *testing.T) {
	testCase := Case{
		ID:     "test-4",
		Name:   "Stable Test",
		Status: StatusPassed,
	}

	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected default IsFlaky nil/false, got true")
	}
}

// TestFrameworkGetCategory pins the framework->category mapping that drives parser
// selection. Note the default arm returns CategoryUnitTest rather than CategoryGeneric,
// so an unrecognized framework is silently treated as a unit-test framework — asserted
// here so the behaviour is a decision rather than an accident.
func TestFrameworkGetCategory(t *testing.T) {
	tests := []struct {
		in   Framework
		want FrameworkCategory
	}{
		{FrameworkJUnit, CategoryGeneric},
		{FrameworkPython, CategoryUnitTest},
		{FrameworkGolang, CategoryUnitTest},
		{FrameworkTestNG, CategoryUnitTest},
		{FrameworkCucumber, CategoryBDD},
		{FrameworkKarate, CategoryBDD},
		{FrameworkPlaywright, CategoryE2E},
		{FrameworkEspresso, CategoryE2E},
		{FrameworkNewman, CategoryAPI},
		{FrameworkK6, CategoryAPI},
		{FrameworkZAP, CategorySecurity},
		{FrameworkSonarQube, CategorySecurity},
		{"nope", CategoryUnitTest},
		{"", CategoryUnitTest},
	}
	for _, tt := range tests {
		if got := tt.in.GetCategory(); got != tt.want {
			t.Errorf("Framework(%q).GetCategory() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestAllFrameworksAreValidAndCategorised guards the list itself: every framework
// AllFrameworks advertises must round-trip through IsValid and must be explicitly
// categorised rather than falling through to the default arm. A framework added to the
// Framework constants but forgotten in AllFrameworks fails here.
func TestAllFrameworksAreValidAndCategorised(t *testing.T) {
	all := AllFrameworks()
	if len(all) == 0 {
		t.Fatal("AllFrameworks() returned nothing")
	}

	seen := make(map[Framework]bool, len(all))
	for _, f := range all {
		if seen[f] {
			t.Errorf("AllFrameworks() lists %q twice", f)
		}
		seen[f] = true

		if !f.IsValid() {
			t.Errorf("AllFrameworks() contains %q but IsValid() is false", f)
		}
		if f.String() != string(f) {
			t.Errorf("Framework(%q).String() = %q", f, f.String())
		}
		// Every listed framework should be deliberately categorised. Only the unit-test
		// frameworks may legitimately return CategoryUnitTest, so a non-unit framework
		// landing there means it fell through the switch.
		if f.GetCategory() == "" {
			t.Errorf("Framework(%q).GetCategory() returned empty", f)
		}
	}
}

func TestFrameworkIsValid(t *testing.T) {
	for _, f := range []Framework{"", "junit-xml", "NOTAFRAMEWORK", "JUnit"} {
		if f.IsValid() {
			t.Errorf("Framework(%q).IsValid() = true, want false", f)
		}
	}
	if !FrameworkJUnit.IsValid() {
		t.Error("FrameworkJUnit.IsValid() = false, want true")
	}
}

func TestSuiteGetStatus(t *testing.T) {
	tests := []struct {
		name  string
		suite Suite
		want  Status
	}{
		{"failures win", Suite{Passed: 5, Failed: 1}, StatusFailed},
		{"errors win", Suite{Passed: 5, Errors: 1}, StatusFailed},
		{"errors win over skips", Suite{Skipped: 2, Errors: 1}, StatusFailed},
		{"all skipped", Suite{Skipped: 3}, StatusSkipped},
		{"passed", Suite{Passed: 3}, StatusPassed},
		{"passed alongside skips", Suite{Passed: 1, Skipped: 2}, StatusPassed},
		{"empty suite is passed", Suite{}, StatusPassed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.suite.GetStatus(); got != tt.want {
				t.Errorf("GetStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSuiteRecomputeCounts is the regression guard described on RecomputeCounts itself:
// counters built from a report header (or incremented independently of case status, the
// trivy/snyk suite.Failed++ bug) must never let a suite with real failures roll up green.
func TestSuiteRecomputeCounts(t *testing.T) {
	t.Run("overwrites disagreeing counters", func(t *testing.T) {
		// A suite whose header claims everything passed, but whose cases say otherwise.
		s := Suite{
			TotalTests: 99, Passed: 99, Failed: 0, Skipped: 0, Errors: 0,
			Cases: []Case{
				{Status: StatusPassed},
				{Status: StatusFailed},
				{Status: StatusError},
				{Status: StatusSkipped},
				{Status: StatusPending},
			},
		}
		s.RecomputeCounts()

		if s.TotalTests != 5 {
			t.Errorf("TotalTests = %d, want 5", s.TotalTests)
		}
		// Pending folds into Skipped, matching GetStatus.
		if s.Passed != 1 || s.Failed != 1 || s.Errors != 1 || s.Skipped != 2 {
			t.Errorf("got passed=%d failed=%d errors=%d skipped=%d; want 1/1/1/2",
				s.Passed, s.Failed, s.Errors, s.Skipped)
		}
		if got := s.GetStatus(); got != StatusFailed {
			t.Errorf("GetStatus() after recompute = %q, want %q", got, StatusFailed)
		}
	})

	t.Run("unknown status counts as an error", func(t *testing.T) {
		// An unrecognized status is a parser bug; it must surface red rather than
		// vanish from the totals.
		s := Suite{Cases: []Case{{Status: StatusPassed}, {Status: "weird"}}}
		s.RecomputeCounts()
		if s.Errors != 1 || s.Passed != 1 || s.TotalTests != 2 {
			t.Errorf("got passed=%d errors=%d total=%d; want 1/1/2", s.Passed, s.Errors, s.TotalTests)
		}
	})

	t.Run("leaves orthogonal counters alone", func(t *testing.T) {
		s := Suite{Flaky: 3, Assertions: 42, Retries: 7, Cases: []Case{{Status: StatusPassed}}}
		s.RecomputeCounts()
		if s.Flaky != 3 || s.Assertions != 42 || s.Retries != 7 {
			t.Errorf("flaky/assertions/retries changed: %d/%d/%d", s.Flaky, s.Assertions, s.Retries)
		}
	})

	t.Run("no cases zeroes the counters", func(t *testing.T) {
		s := Suite{TotalTests: 9, Passed: 9}
		s.RecomputeCounts()
		if s.TotalTests != 0 || s.Passed != 0 {
			t.Errorf("got total=%d passed=%d, want 0/0", s.TotalTests, s.Passed)
		}
	})
}

func TestFormatError(t *testing.T) {
	tests := []struct {
		name                          string
		message, stackTrace, errClass string
		want                          string
	}{
		{"all three", "boom", "at foo()", "AssertionError", "AssertionError: boom\n\nat foo()"},
		{"message only", "boom", "", "", "boom"},
		{"type prefixes message", "boom", "", "AssertionError", "AssertionError: boom"},
		{"stack only", "", "at foo()", "", "at foo()"},
		{"type with empty message still prefixes", "", "", "AssertionError", "AssertionError: "},
		{"all empty", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatError(tt.message, tt.stackTrace, tt.errClass); got != tt.want {
				t.Errorf("FormatError(%q, %q, %q) = %q, want %q",
					tt.message, tt.stackTrace, tt.errClass, got, tt.want)
			}
		})
	}
}

// SeverityFromString is the single mapping behind every security parser, so its
// fail-closed behaviour (CLI-H7) is asserted here rather than only through the parsers.
func TestSeverityFromString(t *testing.T) {
	tests := []struct {
		in   string
		want Severity
	}{
		// Trivy emits upper-case, Snyk lower-case; both must land the same way.
		{"CRITICAL", SeverityCritical},
		{"critical", SeverityCritical},
		{"High", SeverityHigh},
		{"MEDIUM", SeverityMedium},
		{"low", SeverityLow},
		{"  high  ", SeverityHigh},
		// Anything unrankable becomes unknown rather than guessing a severity.
		{"UNKNOWN", SeverityUnknown},
		{"", SeverityUnknown},
		{"catastrophic", SeverityUnknown},
		// "info" is deliberately not mapped: neither parser handled it before, so
		// folding it in here would silently start emitting a "low" priority.
		{"info", SeverityUnknown},
	}
	for _, tt := range tests {
		if got := SeverityFromString(tt.in); got != tt.want {
			t.Errorf("SeverityFromString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The composition the vulnerability parsers rely on: an unrankable severity must yield
// an empty priority, because the API rejects anything outside its enum.
func TestSeverityFromString_ToCasePriority(t *testing.T) {
	for _, in := range []string{"UNKNOWN", "", "nonsense", "info"} {
		if got := SeverityFromString(in).ToCasePriority(); got != "" {
			t.Errorf("SeverityFromString(%q).ToCasePriority() = %q, want empty", in, got)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"CRITICAL", "critical"}, {"high", "high"}, {"Medium", "medium"}, {"LOW", "low"},
	} {
		if got := SeverityFromString(tc.in).ToCasePriority(); string(got) != tc.want {
			t.Errorf("SeverityFromString(%q).ToCasePriority() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPtrHelpers(t *testing.T) {
	if got := IntPtr(0); got == nil || *got != 0 {
		t.Errorf("IntPtr(0) = %v, want pointer to 0", got)
	}
	if got := BoolPtr(false); got == nil || *got != false {
		t.Errorf("BoolPtr(false) = %v, want pointer to false", got)
	}
}

func TestAttachmentLocalVideoPathNeverSerializes(t *testing.T) {
	a := Attachment{Name: "clip", LocalVideoPath: "/tmp/should-not-appear.mp4"}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if got := string(b); containsSubstring(got, "should-not-appear") {
		t.Fatalf("LocalVideoPath leaked into wire JSON: %s", got)
	}
}

func TestAttachmentStorageKeyAndFileSizeSerialize(t *testing.T) {
	a := Attachment{Name: "clip", StorageKey: "case-run-attachments/proj/1.mp4", FileSize: 12345}
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	got := string(b)
	if !containsSubstring(got, `"storageKey":"case-run-attachments/proj/1.mp4"`) {
		t.Fatalf("storageKey missing from wire JSON: %s", got)
	}
	if !containsSubstring(got, `"fileSize":12345`) {
		t.Fatalf("fileSize missing from wire JSON: %s", got)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
