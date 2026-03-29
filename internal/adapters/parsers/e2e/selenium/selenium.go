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
		Name:       "Selenium Test Results",
		Category:   domain.FrameworkSelenium.GetCategory(),
		TotalTests: report.Total,
		Passed:     report.Passed,
		Failed:     report.Failed,
		Skipped:    report.Skipped,
		Duration:   time.Duration(report.Duration * float64(time.Second)),
		Timestamp:  time.Now().UTC(),
		Cases:      make([]domain.Case, 0),
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

	// Update totals if they weren't in the report
	if suite.TotalTests == 0 {
		suite.TotalTests = len(suite.Cases)
		for _, c := range suite.Cases {
			switch c.Status {
			case domain.StatusPassed:
				suite.Passed++
			case domain.StatusFailed:
				suite.Failed++
			case domain.StatusSkipped:
				suite.Skipped++
			}
		}
	}

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

	// Determine status
	switch test.Status {
	case "passed", "pass", "PASSED", "success":
		testCase.Status = domain.StatusPassed
	case "failed", "fail", "FAILED", "failure":
		testCase.Status = domain.StatusFailed
		if test.Error != nil {
			testCase.Error = domain.FormatError(test.Error.Message, test.Error.StackTrace, test.Error.Type)
		}
	case "skipped", "skip", "SKIPPED", "pending":
		testCase.Status = domain.StatusSkipped
	case "error", "ERROR":
		testCase.Status = domain.StatusError
		if test.Error != nil {
			testCase.Error = domain.FormatError(test.Error.Message, test.Error.StackTrace, test.Error.Type)
		}
	default:
		testCase.Status = domain.StatusPassed
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
