package jest

import (
	"encoding/json"
	"io"
	"time"

	"qualflare-cli/internal/core/domain"
)

// Parser parses Jest/Vitest JSON output
type Parser struct{}

// Jest JSON structures
type Report struct {
	NumTotalTests      int          `json:"numTotalTests"`
	NumPassedTests     int          `json:"numPassedTests"`
	NumFailedTests     int          `json:"numFailedTests"`
	NumPendingTests    int          `json:"numPendingTests"`
	NumTodoTests       int          `json:"numTodoTests"`
	NumTotalTestSuites int          `json:"numTotalTestSuites"`
	StartTime          int64        `json:"startTime"`
	Success            bool         `json:"success"`
	TestResults        []TestResult `json:"testResults"`
}

type TestResult struct {
	Name             string      `json:"name"`
	Status           string      `json:"status"`
	StartTime        int64       `json:"startTime"`
	EndTime          int64       `json:"endTime"`
	AssertionResults []Assertion `json:"assertionResults"`
	Message          string      `json:"message"`
}

type Assertion struct {
	AncestorTitles  []string  `json:"ancestorTitles"`
	FullName        string    `json:"fullName"`
	Status          string    `json:"status"`
	Title           string    `json:"title"`
	Duration        *int      `json:"duration"`
	FailureMessages []string  `json:"failureMessages"`
	FailureDetails  []any     `json:"failureDetails"`
	Location        *Location `json:"location"`
}

type Location struct {
	Column int `json:"column"`
	Line   int `json:"line"`
}

// New creates a new Jest parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Jest JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:       "Jest Test Results",
		TotalTests: report.NumTotalTests,
		Passed:     report.NumPassedTests,
		Failed:     report.NumFailedTests,
		Skipped:    report.NumPendingTests + report.NumTodoTests,
		Timestamp:  time.UnixMilli(report.StartTime),
		Cases:      make([]domain.Case, 0),
	}

	var totalDuration time.Duration

	for _, testResult := range report.TestResults {
		// Calculate duration from start/end time
		suiteDuration := time.Duration(testResult.EndTime-testResult.StartTime) * time.Millisecond
		totalDuration += suiteDuration

		for _, assertion := range testResult.AssertionResults {
			testCase := p.convertAssertion(assertion, testResult.Name)
			suite.Cases = append(suite.Cases, testCase)
		}
	}

	suite.Duration = totalDuration
	suite.TotalTests = len(suite.Cases)

	return suite, nil
}

// convertAssertion converts a Jest assertion to domain.Case
func (p *Parser) convertAssertion(assertion Assertion, fileName string) domain.Case {
	testCase := domain.Case{
		ID:        assertion.FullName,
		Name:      assertion.Title,
		ClassName: fileName,
	}

	// Parse duration
	if assertion.Duration != nil {
		testCase.Duration = time.Duration(*assertion.Duration) * time.Millisecond
	}

	// Determine status
	switch assertion.Status {
	case "passed":
		testCase.Status = domain.StatusPassed
	case "failed":
		testCase.Status = domain.StatusFailed
		if len(assertion.FailureMessages) > 0 {
			testCase.ErrorMessage = assertion.FailureMessages[0]
			if len(assertion.FailureMessages) > 1 {
				testCase.StackTrace = assertion.FailureMessages[1]
			}
		}
	case "pending", "todo", "skipped":
		testCase.Status = domain.StatusSkipped
	default:
		testCase.Status = domain.StatusPassed
	}

	// Add ancestor titles as tags
	if len(assertion.AncestorTitles) > 0 {
		testCase.Tags = assertion.AncestorTitles
	}

	// Add location as property
	if assertion.Location != nil {
		testCase.Properties = map[string]string{
			"file": fileName,
		}
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkJest
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
