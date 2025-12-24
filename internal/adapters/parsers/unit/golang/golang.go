package golang

import (
	"bufio"
	"encoding/json"
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
	suite := &domain.Suite{
		Name:      "Go Tests",
		Category:  domain.FrameworkGolang.GetCategory(),
		Timestamp: time.Now(),
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
			if event.Action == "pass" || event.Action == "fail" {
				totalDuration = base.ParseDurationMs(event.Elapsed * 1000)
			}
			continue
		}

		testKey := event.Package + "/" + event.Test
		p.processEvent(tests, testKey, event)
	}

	// Convert tests map to cases
	for _, state := range tests {
		if state.status == "" {
			continue // Skip tests that never completed
		}

		testCase := domain.Case{
			ID:        state.id,
			Name:      state.name,
			ClassName: state.pkg,
			Status:    state.status,
			Duration:  state.duration,
		}

		if state.failed {
			testCase.ErrorMessage = strings.TrimSpace(state.output.String())
			// Try to extract stack trace
			output := state.output.String()
			if idx := strings.Index(output, "\n"); idx > 0 {
				testCase.ErrorMessage = strings.TrimSpace(output[:idx])
				testCase.StackTrace = strings.TrimSpace(output[idx+1:])
			}
		}

		suite.Cases = append(suite.Cases, testCase)

		// Update counters
		switch testCase.Status {
		case domain.StatusPassed:
			suite.Passed++
		case domain.StatusFailed:
			suite.Failed++
		case domain.StatusSkipped:
			suite.Skipped++
		}
	}

	suite.TotalTests = len(suite.Cases)
	suite.Duration = totalDuration

	return suite, scanner.Err()
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
