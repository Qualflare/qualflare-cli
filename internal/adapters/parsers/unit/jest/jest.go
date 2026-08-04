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
	NumTotalTests      int `json:"numTotalTests"`
	NumPassedTests     int `json:"numPassedTests"`
	NumFailedTests     int `json:"numFailedTests"`
	NumPendingTests    int `json:"numPendingTests"`
	NumTodoTests       int `json:"numTodoTests"`
	NumTotalTestSuites int `json:"numTotalTestSuites"`
	// NumRuntimeErrorTestSuites counts suites that threw at import/collection
	// time (CLI-H9) — these produce a testResult with no assertionResults.
	NumRuntimeErrorTestSuites int          `json:"numRuntimeErrorTestSuites"`
	StartTime                 int64        `json:"startTime"`
	Success                   bool         `json:"success"`
	TestResults               []TestResult `json:"testResults"`
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

	// BUG-37: only trust startTime when it is present; otherwise a missing
	// startTime decodes to 0 and time.UnixMilli(0) stamps the suite at the Unix
	// epoch (1970). Fall back to now instead.
	timestamp := time.Now().UTC()
	if report.StartTime > 0 {
		timestamp = time.UnixMilli(report.StartTime)
	}

	suite := &domain.Suite{
		Name:      "Jest Test Results",
		Category:  domain.FrameworkJest.GetCategory(),
		Timestamp: timestamp,
		Cases:     make([]domain.Case, 0),
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

		// CLI-H9: a suite that crashes at import/collection time yields a
		// testResult with a failed/error status and NO assertionResults. Without
		// synthesizing a case here, the crash produces zero cases and the launch
		// rolls up green — a failing run reported as PASSED.
		if len(testResult.AssertionResults) == 0 &&
			(testResult.Status == "failed" || testResult.Status == "error") {
			suite.Cases = append(suite.Cases, domain.Case{
				ID:        testResult.Name,
				Name:      testResult.Name,
				ClassName: testResult.Name,
				Status:    domain.StatusFailed,
				Duration:  suiteDuration,
				Error:     domain.FormatError(testResult.Message, "", ""),
			})
		}
	}

	// CLI-H9: if the report as a whole signals failure (unsuccessful run or a
	// runtime-error suite) but we still produced no cases, emit a synthetic
	// failed case so the launch cannot roll up green on an empty suite.
	if len(suite.Cases) == 0 && (!report.Success || report.NumRuntimeErrorTestSuites > 0) {
		suite.Cases = append(suite.Cases, domain.Case{
			ID:     "jest-runtime-error",
			Name:   "Jest run reported failure with no test cases",
			Status: domain.StatusFailed,
			Error:  "Jest reported an unsuccessful run (possible suite runtime error) but produced no test cases",
		})
	}

	suite.Duration = totalDuration
	// Counters must derive from the case statuses, never the report header, so a
	// crashed/failed run can never disagree with the cases (see RecomputeCounts).
	suite.RecomputeCounts()

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
			errMsg := assertion.FailureMessages[0]
			stackTrace := ""
			if len(assertion.FailureMessages) > 1 {
				stackTrace = assertion.FailureMessages[1]
			}
			testCase.Error = domain.FormatError(errMsg, stackTrace, "")
		}
	case "pending", "todo", "skipped", "disabled":
		// BUG-36: "disabled" is a skip, not a pass.
		testCase.Status = domain.StatusSkipped
	default:
		// BUG-36: an unknown assertion status must fail-visible, never pass.
		testCase.Status = domain.StatusError
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
