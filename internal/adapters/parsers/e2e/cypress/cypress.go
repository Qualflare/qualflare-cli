package cypress

import (
	"encoding/json"
	"io"
	"time"

	"qualflare-cli/internal/core/domain"
)

// Parser parses Cypress Mochawesome JSON output
type Parser struct{}

// Mochawesome JSON structures (used by Cypress)
type Report struct {
	Stats   Stats    `json:"stats"`
	Results []Result `json:"results"`
}

type Stats struct {
	Suites            int     `json:"suites"`
	Tests             int     `json:"tests"`
	Passes            int     `json:"passes"`
	Pending           int     `json:"pending"`
	Failures          int     `json:"failures"`
	Start             string  `json:"start"`
	End               string  `json:"end"`
	Duration          int     `json:"duration"` // milliseconds
	TestsRegistered   int     `json:"testsRegistered"`
	PassPercent       float64 `json:"passPercent"`
	PendingPercent    float64 `json:"pendingPercent"`
	SkippedRegistered int     `json:"skippedRegistered"`
}

type Result struct {
	UUID        string   `json:"uuid"`
	Title       string   `json:"title"`
	FullFile    string   `json:"fullFile"`
	File        string   `json:"file"`
	BeforeHooks []Hook   `json:"beforeHooks"`
	AfterHooks  []Hook   `json:"afterHooks"`
	Tests       []Test   `json:"tests"`
	Suites      []Suite  `json:"suites"`
	Passes      []string `json:"passes"`
	Failures    []string `json:"failures"`
	Pending     []string `json:"pending"`
	Skipped     []string `json:"skipped"`
	Duration    int      `json:"duration"`
	Root        bool     `json:"root"`
	RootEmpty   bool     `json:"rootEmpty"`
}

type Suite struct {
	UUID        string  `json:"uuid"`
	Title       string  `json:"title"`
	FullFile    string  `json:"fullFile"`
	File        string  `json:"file"`
	Tests       []Test  `json:"tests"`
	Suites      []Suite `json:"suites"` // Nested suites
	Duration    int     `json:"duration"`
	BeforeHooks []Hook  `json:"beforeHooks"`
	AfterHooks  []Hook  `json:"afterHooks"`
}

type Test struct {
	Title      string   `json:"title"`
	FullTitle  string   `json:"fullTitle"`
	TimedOut   bool     `json:"timedOut"`
	Duration   int      `json:"duration"`
	State      string   `json:"state"` // passed, failed, pending
	Speed      string   `json:"speed"`
	Pass       bool     `json:"pass"`
	Fail       bool     `json:"fail"`
	Pending    bool     `json:"pending"`
	Retries    int      `json:"_retries,omitempty"`  // Number of retries
	Attempts   []Attempt `json:"attempts,omitempty"` // Individual attempts
	Context    string   `json:"context"`
	Code       string   `json:"code"`
	Err        Error    `json:"err"`
	UUID       string   `json:"uuid"`
	ParentUUID string   `json:"parentUUID"`
	IsHook     bool     `json:"isHook"`
	Skipped    bool     `json:"skipped"`
}

type Attempt struct {
	State    string `json:"state"`
	Duration int    `json:"duration"`
}

type Hook struct {
	Title     string `json:"title"`
	FullTitle string `json:"fullTitle"`
	TimedOut  bool   `json:"timedOut"`
	Duration  int    `json:"duration"`
	State     string `json:"state"`
	Speed     string `json:"speed"`
	Pass      bool   `json:"pass"`
	Fail      bool   `json:"fail"`
	Pending   bool   `json:"pending"`
	Err       Error  `json:"err"`
}

type Error struct {
	Message  string `json:"message"`
	Estack   string `json:"estack"`
	Diff     string `json:"diff"`
	ShowDiff bool   `json:"showDiff"`
	Actual   string `json:"actual"`
	Expected string `json:"expected"`
}

// New creates a new Cypress parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Cypress/Mochawesome JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:       "Cypress Test Results",
		Category:   domain.FrameworkCypress.GetCategory(),
		TotalTests: report.Stats.Tests,
		Passed:     report.Stats.Passes,
		Failed:     report.Stats.Failures,
		Skipped:    report.Stats.Pending + report.Stats.SkippedRegistered,
		Duration:   time.Duration(report.Stats.Duration) * time.Millisecond,
		Timestamp:  time.Now(),
		Cases:      make([]domain.Case, 0),
	}

	// Parse start time if available
	if report.Stats.Start != "" {
		if t, err := time.Parse(time.RFC3339, report.Stats.Start); err == nil {
			suite.Timestamp = t
		}
	}

	// Process all results
	for _, result := range report.Results {
		p.processResult(result, suite)
	}

	return suite, nil
}

// processResult processes a Mochawesome result
func (p *Parser) processResult(result Result, domainSuite *domain.Suite) {
	// Process tests directly in the result
	for _, test := range result.Tests {
		testCase := p.convertTest(test, result.FullFile)
		domainSuite.Cases = append(domainSuite.Cases, testCase)
	}

	// Process nested suites
	for _, s := range result.Suites {
		p.processSuite(s, domainSuite)
	}
}

// processSuite recursively processes Mochawesome suites
func (p *Parser) processSuite(s Suite, domainSuite *domain.Suite) {
	// Process tests in this suite
	for _, test := range s.Tests {
		testCase := p.convertTest(test, s.FullFile)
		domainSuite.Cases = append(domainSuite.Cases, testCase)
	}

	// Process nested suites
	for _, nested := range s.Suites {
		p.processSuite(nested, domainSuite)
	}
}

// convertTest converts a Mochawesome test to domain.Case
func (p *Parser) convertTest(test Test, file string) domain.Case {
	testCase := domain.Case{
		ID:        test.UUID,
		Name:      test.Title,
		ClassName: file,
		Duration:  time.Duration(test.Duration) * time.Millisecond,
	}

	// Calculate retry count from attempts or retries field
	if len(test.Attempts) > 0 {
		testCase.RetryCount = len(test.Attempts) - 1
	} else if test.Retries > 0 {
		testCase.RetryCount = test.Retries
	}

	// Determine flaky status (passed after retries)
	testCase.IsFlaky = testCase.RetryCount > 0 && test.State == "passed"

	// Determine status
	switch test.State {
	case "passed":
		testCase.Status = domain.StatusPassed
	case "failed":
		testCase.Status = domain.StatusFailed
		if test.Err.Message != "" {
			testCase.ErrorMessage = test.Err.Message
			testCase.StackTrace = test.Err.Estack
		}
	case "pending":
		testCase.Status = domain.StatusPending
	default:
		if test.Skipped {
			testCase.Status = domain.StatusSkipped
		} else if test.Pass {
			testCase.Status = domain.StatusPassed
		} else if test.Fail {
			testCase.Status = domain.StatusFailed
		} else {
			testCase.Status = domain.StatusSkipped
		}
	}

	// Handle timed out tests
	if test.TimedOut {
		testCase.Status = domain.StatusFailed
		if testCase.ErrorMessage == "" {
			testCase.ErrorMessage = "Test timed out"
		}
	}

	// Add properties
	testCase.Properties = map[string]string{
		"file":      file,
		"fullTitle": test.FullTitle,
	}
	if test.Speed != "" {
		testCase.Properties["speed"] = test.Speed
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkCypress
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
