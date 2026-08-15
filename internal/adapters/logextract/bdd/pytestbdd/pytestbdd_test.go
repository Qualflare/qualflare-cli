package pytestbdd

import (
	"testing"

	"qualflare-cli/internal/core/domain"
)

const twoScenarios = `Feature: User authentication
Scenario: Successful login
  Given the user is on the login page
  When the user enters valid credentials
  Then the user is redirected to the dashboard
PASSED                                                    [ 50%]

Scenario: Failed login
  Given the user is on the login page
  When the user enters invalid credentials
  Then an error message is shown
FAILED                                                    [100%]

=================================== FAILURES ===================================
______________________________ Failed login ______________________________
AssertionError: expected error banner, found none
`

func TestExtractCases_TwoScenarios(t *testing.T) {
	e := New()
	cases, err := e.ExtractCases([]byte(twoScenarios))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d: %+v", len(cases), cases)
	}

	first := cases[0]
	if first.Name != "Successful login" || first.Status != domain.StatusPassed {
		t.Fatalf("unexpected first case: %+v", first)
	}
	if len(first.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d: %+v", len(first.Steps), first.Steps)
	}
	if first.Steps[0].Keyword != "Given" || first.Steps[0].Name != "the user is on the login page" {
		t.Errorf("unexpected first step: %+v", first.Steps[0])
	}
	if first.Steps[1].Keyword != "When" {
		t.Errorf("unexpected second step keyword: %+v", first.Steps[1])
	}
	if first.Steps[2].Keyword != "Then" {
		t.Errorf("unexpected third step keyword: %+v", first.Steps[2])
	}
	for _, s := range first.Steps {
		if s.Status != domain.StatusPassed {
			t.Errorf("expected all steps of a passed scenario to be passed, got %+v", s)
		}
	}

	second := cases[1]
	if second.Name != "Failed login" || second.Status != domain.StatusFailed {
		t.Fatalf("unexpected second case: %+v", second)
	}
	if len(second.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(second.Steps))
	}
	for _, s := range second.Steps {
		if s.Status != domain.StatusFailed {
			t.Errorf("expected all steps of a failed scenario to be failed, got %+v", s)
		}
	}
}

func TestExtractCases_ScenarioOutlineExpandsToSeparateCases(t *testing.T) {
	input := `Feature: Login attempts
Scenario Outline: Login with credentials
  Given a user with username "alice"
  When they log in
PASSED                                                    [ 33%]

Scenario Outline: Login with credentials
  Given a user with username "bob"
  When they log in
PASSED                                                    [ 66%]

Scenario Outline: Login with credentials
  Given a user with username "eve"
  When they log in
FAILED                                                    [100%]
`
	e := New()
	cases, err := e.ExtractCases([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 3 {
		t.Fatalf("expected 3 expanded cases, got %d", len(cases))
	}
	if cases[2].Status != domain.StatusFailed {
		t.Errorf("expected third expansion to be failed, got %v", cases[2].Status)
	}
}

func TestExtractCases_NoGivenWhenThenLinesReturnsNoCases(t *testing.T) {
	e := New()
	cases, err := e.ExtractCases([]byte("plain pytest output with no BDD narration\n2 passed in 0.01s\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 0 {
		t.Errorf("expected zero cases for non-BDD output, got %d", len(cases))
	}
}

func TestDetect(t *testing.T) {
	e := New()
	if !e.Detect([]byte(twoScenarios)) {
		t.Error("expected Detect to recognize pytest-bdd output")
	}
	if e.Detect([]byte("plain pytest output\n2 passed in 0.01s\n")) {
		t.Error("expected Detect to reject non-BDD output")
	}
}

func TestGetFramework(t *testing.T) {
	if New().GetFramework() != domain.FrameworkPython {
		t.Errorf("expected FrameworkPython")
	}
}
