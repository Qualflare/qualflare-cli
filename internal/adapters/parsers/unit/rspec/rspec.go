package rspec

import (
	"encoding/json"
	"io"
	"strconv"
	"time"

	"qualflare-cli/internal/core/domain"
)

// Parser parses RSpec JSON formatter output
type Parser struct{}

// RSpec JSON structures
type Report struct {
	Version     string    `json:"version"`
	Seed        int       `json:"seed"`
	Examples    []Example `json:"examples"`
	Summary     Summary   `json:"summary"`
	SummaryLine string    `json:"summary_line"`
}

type Example struct {
	ID              string     `json:"id"`
	Description     string     `json:"description"`
	FullDescription string     `json:"full_description"`
	Status          string     `json:"status"`
	FilePath        string     `json:"file_path"`
	LineNumber      int        `json:"line_number"`
	RunTime         float64    `json:"run_time"`
	PendingMessage  string     `json:"pending_message,omitempty"`
	Exception       *Exception `json:"exception,omitempty"`
}

type Exception struct {
	Class     string   `json:"class"`
	Message   string   `json:"message"`
	Backtrace []string `json:"backtrace"`
}

type Summary struct {
	Duration                     float64 `json:"duration"`
	ExampleCount                 int     `json:"example_count"`
	FailureCount                 int     `json:"failure_count"`
	PendingCount                 int     `json:"pending_count"`
	ErrorsOutsideOfExamplesCount int     `json:"errors_outside_of_examples_count"`
}

// New creates a new RSpec parser
func New() *Parser {
	return &Parser{}
}

// Parse parses RSpec JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:       "RSpec Test Results",
		TotalTests: report.Summary.ExampleCount,
		Failed:     report.Summary.FailureCount,
		Skipped:    report.Summary.PendingCount,
		Duration:   time.Duration(report.Summary.Duration * float64(time.Second)),
		Timestamp:  time.Now(),
		Cases:      make([]domain.Case, 0, len(report.Examples)),
	}

	suite.Passed = suite.TotalTests - suite.Failed - suite.Skipped

	for _, example := range report.Examples {
		testCase := p.convertExample(example)
		suite.Cases = append(suite.Cases, testCase)
	}

	return suite, nil
}

// convertExample converts an RSpec example to domain.Case
func (p *Parser) convertExample(example Example) domain.Case {
	testCase := domain.Case{
		ID:        example.ID,
		Name:      example.Description,
		ClassName: example.FilePath,
		Duration:  time.Duration(example.RunTime * float64(time.Second)),
	}

	// Determine status
	switch example.Status {
	case "passed":
		testCase.Status = domain.StatusPassed
	case "failed":
		testCase.Status = domain.StatusFailed
		if example.Exception != nil {
			testCase.ErrorMessage = example.Exception.Message
			testCase.ErrorType = example.Exception.Class
			if len(example.Exception.Backtrace) > 0 {
				testCase.StackTrace = example.Exception.Backtrace[0]
				for i := 1; i < len(example.Exception.Backtrace) && i < 10; i++ {
					testCase.StackTrace += "\n" + example.Exception.Backtrace[i]
				}
			}
		}
	case "pending":
		testCase.Status = domain.StatusPending
		testCase.ErrorMessage = example.PendingMessage
	default:
		testCase.Status = domain.StatusSkipped
	}

	// Add properties
	testCase.Properties = map[string]string{
		"file":        example.FilePath,
		"line_number": strconv.Itoa(example.LineNumber),
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkRSpec
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
