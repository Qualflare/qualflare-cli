package xctest

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

// testsFixture is real output (trimmed) captured from
// `xcresulttool get test-results tests --path Spike.xcresult --compact`
// against an actual .xcresult bundle built for this feature (one passing,
// one failing XCTest test) — not a guessed shape.
const testsFixture = `{
	"testNodes": [{
		"name": "QfSpike-Package",
		"nodeType": "Test Plan",
		"result": "Failed",
		"children": [{
			"name": "QfSpikeTests",
			"nodeType": "Unit test bundle",
			"result": "Failed",
			"children": [{
				"name": "QfSpikeTests",
				"nodeType": "Test Suite",
				"result": "Failed",
				"children": [
					{
						"name": "testAddFails()",
						"nodeIdentifier": "QfSpikeTests/testAddFails()",
						"nodeType": "Test Case",
						"result": "Failed",
						"durationInSeconds": 0.5228029489517212,
						"children": [{
							"name": "QfSpikeTests.swift:11: XCTAssertEqual failed: (\"4\") is not equal to (\"5\") - intentional failure for the spike",
							"nodeType": "Failure Message"
						}]
					},
					{
						"name": "testAddPasses()",
						"nodeIdentifier": "QfSpikeTests/testAddPasses()",
						"nodeType": "Test Case",
						"result": "Passed",
						"durationInSeconds": 0.000889897346496582
					}
				]
			}]
		}]
	}]
}`

// activitiesFixture (passing test) is real output captured from
// `xcresulttool get test-results activities --path Spike.xcresult
// --test-id "QfSpikeTests/testAddPasses()" --compact` against the same
// bundle — the test wrapped its assertion in
// `XCTContext.runActivity(named: "Adding two numbers")`.
const activitiesFixturePassing = `{
	"testIdentifier": "QfSpikeTests/testAddPasses()",
	"testName": "testAddPasses()",
	"testRuns": [{
		"activities": [{
			"title": "Adding two numbers",
			"startTime": 1786804419.93,
			"isAssociatedWithFailure": false
		}]
	}]
}`

// activitiesFixtureFailing is the same capture for testAddFails().
const activitiesFixtureFailing = `{
	"testIdentifier": "QfSpikeTests/testAddFails()",
	"testName": "testAddFails()",
	"testRuns": [{
		"activities": [{
			"title": "XCTAssertEqual failed: (\"4\") is not equal to (\"5\") - intentional failure for the spike",
			"startTime": 1786804419.408,
			"isAssociatedWithFailure": true,
			"childActivities": [{
				"title": "QfSpikeTests.testAddFails()",
				"isAssociatedWithFailure": false
			}]
		}]
	}]
}`

func TestBuildCasesFromTestNodes(t *testing.T) {
	var resp testsResponse
	if err := decodeXCResultJSON([]byte(testsFixture), &resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	cases := buildCasesFromTestNodes(resp.TestNodes)
	if len(cases) != 2 {
		t.Fatalf("cases = %d, want 2 (only Test Case nodes, not the Plan/bundle/Suite ancestors)", len(cases))
	}

	byID := map[string]domain.Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}

	failed, ok := byID["QfSpikeTests/testAddFails()"]
	if !ok {
		t.Fatal("missing testAddFails() case")
	}
	if failed.Status != domain.StatusFailed {
		t.Errorf("Status = %q, want failed", failed.Status)
	}
	if !strings.Contains(failed.Error, "XCTAssertEqual failed") {
		t.Errorf("Error = %q, want the Failure Message child's text", failed.Error)
	}

	passed, ok := byID["QfSpikeTests/testAddPasses()"]
	if !ok {
		t.Fatal("missing testAddPasses() case")
	}
	if passed.Status != domain.StatusPassed {
		t.Errorf("Status = %q, want passed", passed.Status)
	}
	if passed.Duration <= 0 {
		t.Errorf("Duration = %v, want a positive duration from durationInSeconds", passed.Duration)
	}
}

func TestStepsFromActivities_Passing(t *testing.T) {
	var resp activitiesResponse
	if err := decodeXCResultJSON([]byte(activitiesFixturePassing), &resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	steps := stepsFromActivities(resp.TestRuns)
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if steps[0].Name != "Adding two numbers" {
		t.Errorf("Name = %q, want the XCTContext.runActivity title", steps[0].Name)
	}
	if steps[0].Status != domain.StatusPassed {
		t.Errorf("Status = %q, want passed", steps[0].Status)
	}
}

// A failure activity's child activities are flattened as siblings (matching
// Playwright's existing convention for a nested step tree), and only the
// activity actually marked isAssociatedWithFailure reads as failed.
func TestStepsFromActivities_Failing(t *testing.T) {
	var resp activitiesResponse
	if err := decodeXCResultJSON([]byte(activitiesFixtureFailing), &resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	steps := stepsFromActivities(resp.TestRuns)
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2 (the failure activity + its flattened child)", len(steps))
	}
	if steps[0].Status != domain.StatusFailed {
		t.Errorf("steps[0].Status = %q, want failed", steps[0].Status)
	}
	if steps[1].Status != domain.StatusPassed {
		t.Errorf("steps[1].Status = %q, want passed (not itself marked as the failure)", steps[1].Status)
	}
}

func TestMapTestResult(t *testing.T) {
	tests := []struct {
		result string
		want   domain.Status
	}{
		{"Passed", domain.StatusPassed},
		{"Failed", domain.StatusFailed},
		{"Skipped", domain.StatusSkipped},
		// An expected failure (XCTExpectFailure) did fail as predicted — it
		// does not fail the overall test run, so it reads as passed.
		{"Expected Failure", domain.StatusPassed},
		{"unknown", domain.StatusError},
		{"something-new-a-future-xcresulttool-version-might-emit", domain.StatusError},
	}
	for _, tt := range tests {
		if got := mapTestResult(tt.result); got != tt.want {
			t.Errorf("mapTestResult(%q) = %q, want %q", tt.result, got, tt.want)
		}
	}
}
