package selenium

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Selenium/WebDriver JSON output
// This supports common Selenium test report formats
type Parser struct{}

// Selenium JSON structures (generic WebDriver format)
type Report struct {
	StartTime string  `json:"startTime"`
	EndTime   string  `json:"endTime"`
	Duration  float64 `json:"duration"` // seconds
	Total     int     `json:"total"`
	Passed    int     `json:"passed"`
	Failed    int     `json:"failed"`
	Skipped   int     `json:"skipped"`
	Suites    []Suite `json:"suites"`
	Browser   string  `json:"browser"`
	Platform  string  `json:"platform"`
	Version   string  `json:"version"`
}

type Suite struct {
	Name      string  `json:"name"`
	ClassName string  `json:"className"`
	Tests     []Test  `json:"tests"`
	Duration  float64 `json:"duration"`
	StartTime string  `json:"startTime"`
	EndTime   string  `json:"endTime"`
}

type Test struct {
	Name        string   `json:"name"`
	ClassName   string   `json:"className"`
	MethodName  string   `json:"methodName"`
	Status      string   `json:"status"`
	Duration    float64  `json:"duration"`
	StartTime   string   `json:"startTime"`
	EndTime     string   `json:"endTime"`
	Error       *Error   `json:"error,omitempty"`
	Browser     string   `json:"browser"`
	Screenshots []string `json:"screenshots,omitempty"`
	Logs        []Log    `json:"logs,omitempty"`
}

type Error struct {
	Message    string `json:"message"`
	StackTrace string `json:"stackTrace"`
	Type       string `json:"type"`
}

type Log struct {
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// New creates a new Selenium parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Selenium JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      "Selenium Test Results",
		Category:  domain.FrameworkSelenium.GetCategory(),
		Duration:  time.Duration(report.Duration * float64(time.Second)),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Parse start time if available
	if report.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, report.StartTime); err == nil {
			suite.Timestamp = t
		}
	}

	// Store browser/platform in properties for Launch to use
	suite.Properties = make(map[string]string)
	if report.Browser != "" {
		suite.Properties["browser"] = report.Browser
	}
	if report.Platform != "" {
		suite.Properties["platform"] = report.Platform
	}

	// Process all suites
	for _, s := range report.Suites {
		for _, test := range s.Tests {
			testCase := p.convertTest(test, s.Name)
			suite.Cases = append(suite.Cases, testCase)
		}
	}

	// CLI-H5 / rule #3: derive all counters from the actual case statuses instead
	// of trusting the report header, so a broken/errored case can never roll up
	// green (the header could claim passed=N while a case is StatusError).
	suite.RecomputeCounts()

	return suite, nil
}

// convertTest converts a Selenium test to domain.Case
func (p *Parser) convertTest(test Test, suiteName string) domain.Case {
	testCase := domain.Case{
		ID:        test.ClassName + "." + test.MethodName,
		Name:      test.Name,
		ClassName: base.CoalesceString(test.ClassName, suiteName),
		Duration:  time.Duration(test.Duration * float64(time.Second)),
	}
	if test.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, test.StartTime); err == nil {
			testCase.StartedAt = &t
		}
	}

	// Determine status
	switch test.Status {
	case "passed", "pass", "PASSED", "success":
		testCase.Status = domain.StatusPassed
	case "failed", "fail", "FAILED", "failure":
		testCase.Status = domain.StatusFailed
	case "skipped", "skip", "SKIPPED", "pending":
		testCase.Status = domain.StatusSkipped
	// CLI-H5: Allure-style "broken"/"timeout"/"aborted" mean the test errored
	// (setup/WebDriver failure), not passed.
	case "error", "ERROR", "broken", "timeout", "aborted":
		testCase.Status = domain.StatusError
	default:
		// CLI-H5: an unknown status must fail-visible as Error, never silently
		// pass (the old default:StatusPassed uploaded broken tests as GREEN).
		testCase.Status = domain.StatusError
	}

	// CLI-H5: attach the error object for ANY non-passed status (broken/error/
	// failed) — previously it lived only inside the failed/error branches, so a
	// "broken" test's stack trace was stripped on upload.
	if testCase.Status != domain.StatusPassed && test.Error != nil {
		testCase.Error = domain.FormatError(test.Error.Message, test.Error.StackTrace, test.Error.Type)
	}

	// Add properties
	testCase.Properties = map[string]string{
		"methodName": test.MethodName,
	}
	if test.Browser != "" {
		testCase.Properties["browser"] = test.Browser
	}

	// Convert screenshots to attachments
	if len(test.Screenshots) > 0 {
		testCase.Attachments = make([]domain.Attachment, 0, len(test.Screenshots))
		for i, ss := range test.Screenshots {
			testCase.Attachments = append(testCase.Attachments, domain.Attachment{
				Name:     fmt.Sprintf("screenshot-%d", i+1),
				Path:     ss,
				MimeType: "image/png",
			})
		}
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkSelenium
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
