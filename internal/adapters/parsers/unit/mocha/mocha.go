package mocha

import (
	"encoding/json"
	"io"
	"time"

	"qualflare-cli/internal/core/domain"
)

// Parser parses Mocha JSON reporter output
type Parser struct{}

// Mocha JSON structures
type Report struct {
	Stats    Stats  `json:"stats"`
	Tests    []Test `json:"tests"`
	Pending  []Test `json:"pending"`
	Failures []Test `json:"failures"`
	Passes   []Test `json:"passes"`
}

type Stats struct {
	Suites   int    `json:"suites"`
	Tests    int    `json:"tests"`
	Passes   int    `json:"passes"`
	Pending  int    `json:"pending"`
	Failures int    `json:"failures"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Duration int    `json:"duration"` // milliseconds
}

type Test struct {
	Title        string `json:"title"`
	FullTitle    string `json:"fullTitle"`
	File         string `json:"file"`
	Duration     int    `json:"duration"` // milliseconds
	CurrentRetry int    `json:"currentRetry"`
	Speed        string `json:"speed"`
	Err          Error  `json:"err"`
}

type Error struct {
	Message  string `json:"message"`
	Stack    string `json:"stack"`
	Actual   string `json:"actual"`
	Expected string `json:"expected"`
	Operator string `json:"operator"`
}

// New creates a new Mocha parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Mocha JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:       "Mocha Test Results",
		Category:   domain.FrameworkMocha.GetCategory(),
		TotalTests: report.Stats.Tests,
		Passed:     report.Stats.Passes,
		Failed:     report.Stats.Failures,
		Skipped:    report.Stats.Pending,
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

	// Add passing tests
	for _, test := range report.Passes {
		testCase := p.convertTest(test, domain.StatusPassed)
		suite.Cases = append(suite.Cases, testCase)
	}

	// Add failing tests
	for _, test := range report.Failures {
		testCase := p.convertTest(test, domain.StatusFailed)
		suite.Cases = append(suite.Cases, testCase)
	}

	// Add pending tests
	for _, test := range report.Pending {
		testCase := p.convertTest(test, domain.StatusSkipped)
		suite.Cases = append(suite.Cases, testCase)
	}

	return suite, nil
}

// convertTest converts a Mocha test to domain.Case
func (p *Parser) convertTest(test Test, status domain.Status) domain.Case {
	testCase := domain.Case{
		ID:        test.FullTitle,
		Name:      test.Title,
		ClassName: test.File,
		Status:    status,
		Duration:  time.Duration(test.Duration) * time.Millisecond,
	}

	// Add error details for failed tests
	if status == domain.StatusFailed && test.Err.Message != "" {
		testCase.ErrorMessage = test.Err.Message
		testCase.StackTrace = test.Err.Stack
	}

	// Add file as property
	if test.File != "" {
		testCase.Properties = map[string]string{
			"file":  test.File,
			"speed": test.Speed,
		}
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkMocha
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
