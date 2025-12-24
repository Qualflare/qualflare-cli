package pytest

import (
	"encoding/xml"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses pytest XML output
type Parser struct{}

// Python pytest-xml structures
type TestSuite struct {
	XMLName    xml.Name   `xml:"testsuite"`
	Name       string     `xml:"name,attr"`
	Tests      int        `xml:"tests,attr"`
	Failures   int        `xml:"failures,attr"`
	Errors     int        `xml:"errors,attr"`
	Skips      int        `xml:"skips,attr"`
	Time       string     `xml:"time,attr"`
	Timestamp  string     `xml:"timestamp,attr"`
	TestCases  []TestCase `xml:"testcase"`
	Properties []Property `xml:"properties>property"`
}

type TestCase struct {
	Name       string     `xml:"name,attr"`
	Classname  string     `xml:"classname,attr"`
	File       string     `xml:"file,attr"`
	Line       string     `xml:"line,attr"`
	Time       string     `xml:"time,attr"`
	Failure    *Failure   `xml:"failure,omitempty"`
	Error      *Error     `xml:"error,omitempty"`
	Skipped    *Skipped   `xml:"skipped,omitempty"`
	SystemOut  string     `xml:"system-out,omitempty"`
	SystemErr  string     `xml:"system-err,omitempty"`
	Properties []Property `xml:"properties>property"`
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
	Type    string `xml:"type,attr"`
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type Property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// New creates a new Python parser
func New() *Parser {
	return &Parser{}
}

// Parse parses pytest XML content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var testSuite TestSuite
	decoder := xml.NewDecoder(reader)

	if err := decoder.Decode(&testSuite); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:       testSuite.Name,
		Category:   domain.FrameworkPython.GetCategory(),
		TotalTests: testSuite.Tests,
		Failed:     testSuite.Failures + testSuite.Errors,
		Skipped:    testSuite.Skips,
		Timestamp:  time.Now(),
		Cases:      make([]domain.Case, 0, len(testSuite.TestCases)),
	}

	suite.Passed = suite.TotalTests - suite.Failed - suite.Skipped

	if duration, err := base.ParseDuration(testSuite.Time); err == nil {
		suite.Duration = duration
	}

	// Add suite properties
	if len(testSuite.Properties) > 0 {
		suite.Properties = make(map[string]string)
		for _, prop := range testSuite.Properties {
			suite.Properties[prop.Name] = prop.Value
		}
	}

	for _, tc := range testSuite.TestCases {
		testCase := p.convertTestCase(tc)
		suite.Cases = append(suite.Cases, testCase)
	}

	return suite, nil
}

// convertTestCase converts a Python test case to domain.Case
func (p *Parser) convertTestCase(tc TestCase) domain.Case {
	testCase := domain.Case{
		ID:        tc.Classname + "::" + tc.Name,
		Name:      tc.Name,
		ClassName: tc.Classname,
	}

	if duration, err := base.ParseDuration(tc.Time); err == nil {
		testCase.Duration = duration
	}

	// Initialize properties
	testCase.Properties = make(map[string]string)

	// Add test case properties
	for _, prop := range tc.Properties {
		testCase.Properties[prop.Name] = prop.Value
	}

	// Add file and line info
	if tc.File != "" {
		testCase.Properties["file"] = tc.File
	}
	if tc.Line != "" {
		testCase.Properties["line"] = tc.Line
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

	// Add system output
	if tc.SystemOut != "" {
		testCase.Properties["system-out"] = tc.SystemOut
	}
	if tc.SystemErr != "" {
		testCase.Properties["system-err"] = tc.SystemErr
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkPython
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".xml"}
}
