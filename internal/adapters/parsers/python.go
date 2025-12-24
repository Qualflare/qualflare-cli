package parsers

import (
	"encoding/xml"
	"io"
	"qualflare-cli/internal/core/domain"
	"time"
)

type PythonParser struct{}

// Python pytest-xml output has similar structure to JUnit but with some differences
type PythonTestSuite struct {
	XMLName    xml.Name         `xml:"testsuite"`
	Name       string           `xml:"name,attr"`
	Tests      int              `xml:"tests,attr"`
	Failures   int              `xml:"failures,attr"`
	Errors     int              `xml:"errors,attr"`
	Skips      int              `xml:"skips,attr"`
	Time       string           `xml:"time,attr"`
	TestCases  []PythonTestCase `xml:"testcase"`
	Properties []PythonProperty `xml:"properties>property"`
}

type PythonTestCase struct {
	Name       string           `xml:"name,attr"`
	Classname  string           `xml:"classname,attr"`
	File       string           `xml:"file,attr"`
	Line       string           `xml:"line,attr"`
	Time       string           `xml:"time,attr"`
	Failure    *PythonFailure   `xml:"failure,omitempty"`
	Error      *PythonError     `xml:"error,omitempty"`
	Skipped    *PythonSkipped   `xml:"skipped,omitempty"`
	SystemOut  string           `xml:"system-out,omitempty"`
	SystemErr  string           `xml:"system-err,omitempty"`
	Properties []PythonProperty `xml:"properties>property"`
}

type PythonFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type PythonError struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type PythonSkipped struct {
	Type    string `xml:"type,attr"`
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type PythonProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func NewPythonParser() *PythonParser {
	return &PythonParser{}
}

func (p *PythonParser) Parse(reader io.Reader) (*domain.Suite, error) {
	var testSuite PythonTestSuite
	decoder := xml.NewDecoder(reader)

	if err := decoder.Decode(&testSuite); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:       testSuite.Name,
		TotalTests: testSuite.Tests,
		Failed:     testSuite.Failures + testSuite.Errors,
		Skipped:    testSuite.Skips,
		Timestamp:  time.Now(),
		Cases:      make([]domain.Case, 0, len(testSuite.TestCases)),
	}

	suite.Passed = suite.TotalTests - suite.Failed - suite.Skipped

	if duration, err := parseTime(testSuite.Time); err == nil {
		suite.Duration = duration
	}

	for _, testCase := range testSuite.TestCases {
		test := domain.Case{
			ID:   testCase.Classname + "::" + testCase.Name,
			Name: testCase.Name,
		}

		if duration, err := parseTime(testCase.Time); err == nil {
			test.Duration = duration
		}

		// Add properties
		if len(testCase.Properties) > 0 {
			test.Properties = make(map[string]string)
			for _, prop := range testCase.Properties {
				test.Properties[prop.Name] = prop.Value
			}
		}

		// Add file and line info
		if test.Properties == nil {
			test.Properties = make(map[string]string)
		}
		if testCase.File != "" {
			test.Properties["file"] = testCase.File
		}
		if testCase.Line != "" {
			test.Properties["line"] = testCase.Line
		}

		// Determine test status
		if testCase.Failure != nil {
			test.Status = domain.TestStatusFailed
			test.ErrorMessage = testCase.Failure.Message
			test.StackTrace = testCase.Failure.Text
		} else if testCase.Error != nil {
			test.Status = domain.TestStatusError
			test.ErrorMessage = testCase.Error.Message
			test.StackTrace = testCase.Error.Text
		} else if testCase.Skipped != nil {
			test.Status = domain.TestStatusSkipped
			test.ErrorMessage = testCase.Skipped.Message
		} else {
			test.Status = domain.TestStatusPassed
		}

		// Add system output if available
		if testCase.SystemOut != "" {
			if test.Properties == nil {
				test.Properties = make(map[string]string)
			}
			test.Properties["system-out"] = testCase.SystemOut
		}
		if testCase.SystemErr != "" {
			if test.Properties == nil {
				test.Properties = make(map[string]string)
			}
			test.Properties["system-err"] = testCase.SystemErr
		}

		suite.Cases = append(suite.Cases, test)
	}

	return suite, nil
}

func (p *PythonParser) GetFramework() domain.Framework {
	return domain.FrameworkPython
}

func (p *PythonParser) SupportedFileExtensions() []string {
	return []string{".xml"}
}
