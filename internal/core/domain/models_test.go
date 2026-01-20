package domain

import "testing"

func TestCaseWithRetryCount(t *testing.T) {
	testCase := Case{
		ID:         "test-1",
		Name:       "Test With Retry",
		RetryCount: 3, // Test was retried 3 times before passing
		Status:     StatusPassed,
	}

	if testCase.RetryCount != 3 {
		t.Errorf("expected RetryCount 3, got %d", testCase.RetryCount)
	}
}

func TestCaseDefaultRetryCount(t *testing.T) {
	testCase := Case{
		ID:     "test-2",
		Name:   "Test Without Retry",
		Status: StatusPassed,
	}

	if testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount 0, got %d", testCase.RetryCount)
	}
}

func TestCaseWithIsFlaky(t *testing.T) {
	testCase := Case{
		ID:         "test-3",
		Name:       "Flaky Test",
		RetryCount: 2,
		IsFlaky:    true,
		Status:     StatusPassed,
	}

	if !testCase.IsFlaky {
		t.Errorf("expected IsFlaky true, got false")
	}
}

func TestCaseDefaultIsFlaky(t *testing.T) {
	testCase := Case{
		ID:     "test-4",
		Name:   "Stable Test",
		Status: StatusPassed,
	}

	if testCase.IsFlaky {
		t.Errorf("expected default IsFlaky false, got true")
	}
}
