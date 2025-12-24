package junit

import (
	"encoding/xml"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses JUnit XML test results
type Parser struct{}

// JUnit XML structures
type TestSuites struct {
	XMLName    xml.Name    `xml:"testsuites"`
	Name       string      `xml:"name,attr"`
	Tests      int         `xml:"tests,attr"`
	Failures   int         `xml:"failures,attr"`
	Errors     int         `xml:"errors,attr"`
	Skipped    int         `xml:"skipped,attr"`
	Time       string      `xml:"time,attr"`
	TestSuites []TestSuite `xml:"testsuite"`
}

type TestSuite struct {
	XMLName   xml.Name   `xml:"testsuite"`
	Name      string     `xml:"name,attr"`
	Tests     int        `xml:"tests,attr"`
	Failures  int        `xml:"failures,attr"`
	Errors    int        `xml:"errors,attr"`
	Skipped   int        `xml:"skipped,attr"`
	Time      string     `xml:"time,attr"`
	Timestamp string     `xml:"timestamp,attr"`
	TestCases []TestCase `xml:"testcase"`
}

type TestCase struct {
	Name      string   `xml:"name,attr"`
	Classname string   `xml:"classname,attr"`
	Time      string   `xml:"time,attr"`
	Failure   *Failure `xml:"failure,omitempty"`
	Error     *Error   `xml:"error,omitempty"`
	Skipped   *Skipped `xml:"skipped,omitempty"`
	SystemOut string   `xml:"system-out,omitempty"`
	SystemErr string   `xml:"system-err,omitempty"`
}

type Failure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type Error struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type Skipped struct {
	Message string `xml:"message,attr"`
}

// New creates a new JUnit parser
func New() *Parser {
	return &Parser{}
}

// Parse parses JUnit XML content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var testSuites TestSuites
	decoder := xml.NewDecoder(reader)

	if err := decoder.Decode(&testSuites); err != nil {
		// Try parsing as a single test suite
		if seeker, ok := reader.(io.Seeker); ok {
			if _, err := seeker.Seek(0, 0); err != nil {
				return nil, err
			}
		}

		var singleSuite TestSuite
		decoder = xml.NewDecoder(reader)
		if err := decoder.Decode(&singleSuite); err != nil {
			return nil, err
		}
		testSuites.TestSuites = []TestSuite{singleSuite}
	}

	// If we have multiple test suites, aggregate them
	if len(testSuites.TestSuites) == 0 {
		return &domain.Suite{
			Name:      "Empty Suite",
			Timestamp: time.Now(),
		}, nil
	}

	// For multiple suites, we could either merge them or return the first one
	// Here we'll merge all suites into one
	return p.mergeSuites(testSuites), nil
}

// mergeSuites merges multiple JUnit test suites into a single domain.Suite
func (p *Parser) mergeSuites(testSuites TestSuites) *domain.Suite {
	suite := &domain.Suite{
		Name:      base.CoalesceString(testSuites.Name, "JUnit Test Results"),
		Timestamp: time.Now(),
		Cases:     make([]domain.Case, 0),
	}

	var totalDuration time.Duration

	for _, junitSuite := range testSuites.TestSuites {
		// Parse suite duration
		if duration, err := base.ParseDuration(junitSuite.Time); err == nil {
			totalDuration += duration
		}

		// Convert test cases
		for _, tc := range junitSuite.TestCases {
			testCase := p.convertTestCase(tc, junitSuite.Name)
			suite.Cases = append(suite.Cases, testCase)

			// Update counters
			switch testCase.Status {
			case domain.StatusPassed:
				suite.Passed++
			case domain.StatusFailed:
				suite.Failed++
			case domain.StatusError:
				suite.Failed++ // Count errors as failures
			case domain.StatusSkipped:
				suite.Skipped++
			}
		}
	}

	suite.TotalTests = len(suite.Cases)
	suite.Duration = totalDuration

	return suite
}

// convertTestCase converts a JUnit test case to domain.Case
func (p *Parser) convertTestCase(tc TestCase, suiteName string) domain.Case {
	testCase := domain.Case{
		ID:        tc.Classname + "." + tc.Name,
		Name:      tc.Name,
		ClassName: tc.Classname,
	}

	// Parse duration
	if duration, err := base.ParseDuration(tc.Time); err == nil {
		testCase.Duration = duration
	}

	// Determine status
	if tc.Failure != nil {
		testCase.Status = domain.StatusFailed
		testCase.ErrorMessage = tc.Failure.Message
		testCase.StackTrace = tc.Failure.Text
		testCase.ErrorType = tc.Failure.Type
	} else if tc.Error != nil {
		testCase.Status = domain.StatusError
		testCase.ErrorMessage = tc.Error.Message
		testCase.StackTrace = tc.Error.Text
		testCase.ErrorType = tc.Error.Type
	} else if tc.Skipped != nil {
		testCase.Status = domain.StatusSkipped
		testCase.ErrorMessage = tc.Skipped.Message
	} else {
		testCase.Status = domain.StatusPassed
	}

	// Add system output as properties if present
	if tc.SystemOut != "" || tc.SystemErr != "" {
		testCase.Properties = make(map[string]string)
		if tc.SystemOut != "" {
			testCase.Properties["system-out"] = tc.SystemOut
		}
		if tc.SystemErr != "" {
			testCase.Properties["system-err"] = tc.SystemErr
		}
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkJUnit
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".xml"}
}
