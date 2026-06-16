package domain

import "testing"

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
