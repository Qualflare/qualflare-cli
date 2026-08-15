package xctest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/shared/toolrun"
	"qualflare-cli/internal/core/domain"
)

// xcresultTimeout bounds the whole extraction (the base test listing plus
// one activities call per test) — xcresulttool is fast, but a large bundle
// with many tests makes many subprocess calls.
const xcresultTimeout = 2 * time.Minute

// testsResponse mirrors `xcresulttool get test-results tests`'s JSON output
// (schema version 0.1.0, verified against real output — see xcresult_test.go).
type testsResponse struct {
	TestNodes []testNode `json:"testNodes"`
}

type testNode struct {
	NodeIdentifier    string     `json:"nodeIdentifier"`
	NodeType          string     `json:"nodeType"`
	Name              string     `json:"name"`
	DurationInSeconds float64    `json:"durationInSeconds"`
	Result            string     `json:"result"`
	Children          []testNode `json:"children"`
}

// activitiesResponse mirrors `xcresulttool get test-results activities`'s
// JSON output for one test (verified against real output).
type activitiesResponse struct {
	TestRuns []testRunActivities `json:"testRuns"`
}

type testRunActivities struct {
	Activities []activityNode `json:"activities"`
}

type activityNode struct {
	Title                   string         `json:"title"`
	IsAssociatedWithFailure bool           `json:"isAssociatedWithFailure"`
	ChildActivities         []activityNode `json:"childActivities"`
}

// parseXCResultBundle extracts a Suite from a .xcresult directory bundle via
// xcresulttool. The base test list is the ground truth — a failure to run
// or decode it is a hard error, since (unlike Maestro's JUnit XML fallback)
// there is no other source of case-level data for a bare .xcresult input.
// Per-test activity/step enrichment is best-effort: a test whose activities
// call fails or doesn't decode is left with no Steps rather than failing
// the whole parse.
func parseXCResultBundle(bundlePath string) (*domain.Suite, error) {
	ctx, cancel := context.WithTimeout(context.Background(), xcresultTimeout)
	defer cancel()

	out, err := toolrun.Run(ctx, "xcrun", "xcresulttool", "get", "test-results", "tests", "--path", bundlePath, "--compact")
	if err != nil {
		return nil, fmt.Errorf("xcresulttool: %w", err)
	}

	var resp testsResponse
	if err := decodeXCResultJSON(out, &resp); err != nil {
		return nil, fmt.Errorf("xcresulttool: decode test list: %w", err)
	}

	cases := buildCasesFromTestNodes(resp.TestNodes)
	for i := range cases {
		steps, err := activitiesForTest(ctx, bundlePath, cases[i].ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: xctest: could not get activities for %s: %v; reporting case-level results only for it\n", cases[i].ID, err)
			continue
		}
		cases[i].Steps = steps
	}

	suite := &domain.Suite{
		Name:      "XCTest Results",
		Category:  domain.FrameworkXCTest.GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     cases,
	}
	suite.RecomputeCounts()
	return suite, nil
}

func activitiesForTest(ctx context.Context, bundlePath, testID string) ([]domain.Step, error) {
	if testID == "" {
		return nil, nil
	}
	out, err := toolrun.Run(ctx, "xcrun", "xcresulttool", "get", "test-results", "activities", "--path", bundlePath, "--test-id", testID, "--compact")
	if err != nil {
		return nil, err
	}
	var resp activitiesResponse
	if err := decodeXCResultJSON(out, &resp); err != nil {
		return nil, err
	}
	return stepsFromActivities(resp.TestRuns), nil
}

func decodeXCResultJSON(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// buildCasesFromTestNodes walks the test tree looking for "Test Case" leaves
// — everything above them (Test Plan / Unit test bundle / Test Suite) is
// structural grouping, not a reportable case. A failed Test Case's
// "Failure Message" children (siblings of no further Test Case nodes) become
// its Error text.
func buildCasesFromTestNodes(nodes []testNode) []domain.Case {
	var cases []domain.Case
	for _, n := range nodes {
		if n.NodeType == "Test Case" {
			cases = append(cases, caseFromTestNode(n))
			continue
		}
		cases = append(cases, buildCasesFromTestNodes(n.Children)...)
	}
	return cases
}

func caseFromTestNode(n testNode) domain.Case {
	c := domain.Case{
		ID:       n.NodeIdentifier,
		Name:     n.NodeIdentifier,
		Status:   mapTestResult(n.Result),
		Duration: time.Duration(n.DurationInSeconds * float64(time.Second)),
	}
	if className, _, ok := strings.Cut(n.NodeIdentifier, "/"); ok {
		c.ClassName = className
	}

	var messages []string
	for _, child := range n.Children {
		if child.NodeType == "Failure Message" {
			messages = append(messages, child.Name)
		}
	}
	if len(messages) > 0 {
		c.Error = strings.Join(messages, "\n")
	}

	return c
}

// mapTestResult maps xcresulttool's TestResult enum to domain.Status. An
// "Expected Failure" (XCTExpectFailure) failed exactly as predicted and does
// not fail the overall run, so it reads as passed. Anything unrecognized —
// "unknown", or a value a future xcresulttool schema version might add —
// fails closed as StatusError rather than silently passing.
func mapTestResult(result string) domain.Status {
	switch result {
	case "Passed", "Expected Failure":
		return domain.StatusPassed
	case "Failed":
		return domain.StatusFailed
	case "Skipped":
		return domain.StatusSkipped
	default:
		return domain.StatusError
	}
}

// stepsFromActivities flattens the activity tree into siblings (matching
// Playwright's existing convention for a nested step tree — domain.Step has
// no children of its own). isAssociatedWithFailure is the only per-activity
// signal xcresulttool provides, so status is necessarily two-state.
func stepsFromActivities(runs []testRunActivities) []domain.Step {
	steps := make([]domain.Step, 0, len(runs))
	for _, run := range runs {
		steps = append(steps, flattenActivities(run.Activities)...)
	}
	return steps
}

func flattenActivities(activities []activityNode) []domain.Step {
	steps := make([]domain.Step, 0, len(activities))
	for _, a := range activities {
		status := domain.StatusPassed
		if a.IsAssociatedWithFailure {
			status = domain.StatusFailed
		}
		steps = append(steps, domain.Step{Name: a.Title, Status: status})
		steps = append(steps, flattenActivities(a.ChildActivities)...)
	}
	return steps
}
