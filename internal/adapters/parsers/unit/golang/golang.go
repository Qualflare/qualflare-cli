package golang

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Go test JSON output
type Parser struct{}

// TestEvent represents a single test event from `go test -json`
type TestEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test,omitempty"`
	Elapsed float64   `json:"Elapsed,omitempty"`
	Output  string    `json:"Output,omitempty"`
}

// New creates a new Go test parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Go test JSON output
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	scanner := bufio.NewScanner(reader)
	// Increase scanner buffer for long output lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	tests := make(map[string]*testState)
	// Package-level output and failures, tracked so a package-level failure that
	// no individual test captured still surfaces as an error case (BUG-15).
	pkgOutputs := make(map[string]*strings.Builder)
	pkgFailed := make(map[string]bool)

	suite := &domain.Suite{
		Name:      "Go Tests",
		Category:  domain.FrameworkGolang.GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	var totalDuration time.Duration

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "{") {
			continue
		}

		var event TestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		// Process package-level events
		if event.Test == "" {
			switch event.Action {
			case "pass", "fail":
				// BUG-16: accumulate each package's elapsed time. A `go test ./...`
				// run emits one pass/fail per package, so assigning kept only the
				// last package's duration.
				totalDuration += base.ParseDurationMs(event.Elapsed * 1000)
				if event.Action == "fail" {
					pkgFailed[event.Package] = true
				}
			case "output":
				if event.Output != "" {
					b := pkgOutputs[event.Package]
					if b == nil {
						b = &strings.Builder{}
						pkgOutputs[event.Package] = b
					}
					b.WriteString(event.Output)
				}
			}
			continue
		}

		testKey := event.Package + "/" + event.Test
		p.processEvent(tests, testKey, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading Go test output: %w", err)
	}

	if len(tests) == 0 && len(pkgFailed) == 0 {
		return nil, fmt.Errorf("no valid Go test JSON events found")
	}

	// Convert tests map to cases, then surface any package-level failure that no
	// test case captured (BUG-15).
	cases := buildCasesFromTests(tests)
	cases = appendPackageFailureCases(cases, pkgFailed, pkgOutputs)
	suite.Cases = cases

	suite.Duration = totalDuration

	// Derive Passed/Failed/Skipped/Errors/Total from the case statuses so the
	// counters can never disagree with the cases (replaces independent counting).
	suite.RecomputeCounts()

	return suite, nil
}

// buildCasesFromTests converts the tests map into domain cases.
func buildCasesFromTests(tests map[string]*testState) []domain.Case {
	cases := make([]domain.Case, 0, len(tests))

	for _, state := range tests {
		// BUG-35: Go reports a parent test and each of its subtests as separate
		// events, and the parent's Elapsed already includes the subtests. Counting
		// the parent as its own case double-counts results, so skip a parent that
		// has at least one child case standing on its own.
		if hasChild(tests, state) {
			continue
		}

		testCase := domain.Case{
			ID:        state.id,
			Name:      state.name,
			ClassName: state.pkg,
			Status:    state.status,
			Duration:  state.duration,
		}

		if state.status == "" {
			// BUG-15: a test that never reached a terminal event (panic/timeout —
			// only run+output) must surface as an error, not be silently dropped.
			testCase.Status = domain.StatusError
			out := strings.TrimSpace(state.output.String())
			testCase.Error = domain.FormatError("test did not reach a terminal event (panic or timeout)", out, "")
			cases = append(cases, testCase)
			continue
		}

		if state.failed {
			errMsg := strings.TrimSpace(state.output.String())
			stackTrace := ""
			// Try to extract stack trace
			output := state.output.String()
			if idx := strings.Index(output, "\n"); idx > 0 {
				errMsg = strings.TrimSpace(output[:idx])
				stackTrace = strings.TrimSpace(output[idx+1:])
			}
			testCase.Error = domain.FormatError(errMsg, stackTrace, "")
		}

		cases = append(cases, testCase)
	}

	return cases
}

// hasChild reports whether parent has at least one subtest reported as its own
// test (a case whose name is "parent/..." in the same package). Used to skip a
// parent test that would otherwise be double-counted with its subtests (BUG-35).
func hasChild(tests map[string]*testState, parent *testState) bool {
	prefix := parent.name + "/"
	for _, other := range tests {
		if other == parent {
			continue
		}
		if other.pkg == parent.pkg && strings.HasPrefix(other.name, prefix) {
			return true
		}
	}
	return false
}

// appendPackageFailureCases adds a synthetic error case for any package that
// reported a package-level failure which no individual test case captured
// (BUG-15) — e.g. a build failure, a TestMain failure, or a panic that took
// down the package before any test reached a terminal event.
func appendPackageFailureCases(cases []domain.Case, pkgFailed map[string]bool, pkgOutputs map[string]*strings.Builder) []domain.Case {
	if len(pkgFailed) == 0 {
		return cases
	}

	captured := make(map[string]bool)
	for _, c := range cases {
		if c.Status == domain.StatusFailed || c.Status == domain.StatusError {
			captured[c.ClassName] = true
		}
	}

	for pkg := range pkgFailed {
		if captured[pkg] {
			continue
		}
		output := ""
		if b := pkgOutputs[pkg]; b != nil {
			output = strings.TrimSpace(b.String())
		}
		cases = append(cases, domain.Case{
			ID:        pkg + " [package]",
			Name:      pkg + " [package failure]",
			ClassName: pkg,
			Status:    domain.StatusError,
			Error:     domain.FormatError("package-level failure not attributed to any test", output, ""),
		})
	}

	return cases
}

// testState tracks the state of a test during parsing
type testState struct {
	id       string
	name     string
	pkg      string
	status   domain.Status
	duration time.Duration
	failed   bool
	output   strings.Builder
}

// processEvent processes a single test event
func (p *Parser) processEvent(tests map[string]*testState, testKey string, event TestEvent) {
	switch event.Action {
	case "run":
		tests[testKey] = &testState{
			id:   testKey,
			name: event.Test,
			pkg:  event.Package,
		}

	case "pass":
		if test, exists := tests[testKey]; exists {
			test.status = domain.StatusPassed
			test.duration = base.ParseDurationMs(event.Elapsed * 1000)
		}

	case "fail":
		if test, exists := tests[testKey]; exists {
			test.status = domain.StatusFailed
			test.duration = base.ParseDurationMs(event.Elapsed * 1000)
			test.failed = true
		}

	case "skip":
		if test, exists := tests[testKey]; exists {
			test.status = domain.StatusSkipped
			test.duration = base.ParseDurationMs(event.Elapsed * 1000)
		}

	case "output":
		if test, exists := tests[testKey]; exists {
			// Collect output for error messages
			if event.Output != "" {
				test.output.WriteString(event.Output)
			}
		}
	}
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkGolang
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json", ".out"}
}
