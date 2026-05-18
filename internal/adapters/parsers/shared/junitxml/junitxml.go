// Package junitxml parses JUnit XML test results. It is shared by all
// JUnit-compatible parsers (junit, testng, maestro, xctest, espresso, etc.)
// so each framework only needs a thin wrapper that stamps its own Framework.
package junitxml

import (
	"encoding/xml"
	"fmt"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// TestSuites is the root element of a JUnit XML report.
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

// TestSuite represents a single <testsuite> element.
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

// TestCase represents a single <testcase> element.
type TestCase struct {
	Name       string     `xml:"name,attr"`
	Classname  string     `xml:"classname,attr"`
	Time       string     `xml:"time,attr"`
	Failure    *Failure   `xml:"failure,omitempty"`
	Error      *Error     `xml:"error,omitempty"`
	Skipped    *Skipped   `xml:"skipped,omitempty"`
	Properties []Property `xml:"properties>property"`
	SystemOut  string     `xml:"system-out,omitempty"`
	SystemErr  string     `xml:"system-err,omitempty"`
}

// Property is a key/value pair inside <properties>.
type Property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Failure holds a <failure> child element.
type Failure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

// Error holds an <error> child element.
type Error struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

// Skipped holds a <skipped> child element.
type Skipped struct {
	Message string `xml:"message,attr"`
}

// Parse decodes JUnit XML from reader and stamps it with the given framework.
// This is the single implementation shared by all JUnit-compatible parsers.
func Parse(reader io.Reader, framework domain.Framework) (*domain.Suite, error) {
	var testSuites TestSuites
	decoder := xml.NewDecoder(reader)

	if err := decoder.Decode(&testSuites); err != nil {
		// Try parsing as a single <testsuite> root element
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

	if len(testSuites.TestSuites) == 0 {
		return &domain.Suite{
			Name:      "Empty Suite",
			Timestamp: time.Now().UTC(),
		}, nil
	}

	return mergeSuites(testSuites, framework), nil
}

func mergeSuites(testSuites TestSuites, framework domain.Framework) *domain.Suite {
	suite := &domain.Suite{
		Name:      base.CoalesceString(testSuites.Name, "JUnit Test Results"),
		Category:  framework.GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	var totalDuration time.Duration

	for _, junitSuite := range testSuites.TestSuites {
		if duration, err := base.ParseDuration(junitSuite.Time); err == nil {
			totalDuration += duration
		}

		for _, tc := range junitSuite.TestCases {
			testCase := convertTestCase(tc)
			suite.Cases = append(suite.Cases, testCase)

			switch testCase.Status {
			case domain.StatusPassed:
				suite.Passed++
			case domain.StatusFailed:
				suite.Failed++
			case domain.StatusError:
				suite.Failed++
			case domain.StatusSkipped:
				suite.Skipped++
			}
		}
	}

	suite.TotalTests = len(suite.Cases)
	suite.Duration = totalDuration

	return suite
}

func convertTestCase(tc TestCase) domain.Case {
	testCase := domain.Case{
		ID:        tc.Classname + "." + tc.Name,
		Name:      tc.Name,
		ClassName: tc.Classname,
	}

	if duration, err := base.ParseDuration(tc.Time); err == nil {
		testCase.Duration = duration
	}

	var retryCount int
	for _, prop := range tc.Properties {
		if prop.Name == "retries" || prop.Name == "retryCount" {
			if _, err := fmt.Sscanf(prop.Value, "%d", &retryCount); err == nil {
				testCase.RetryCount = domain.IntPtr(retryCount)
			}
		}
	}

	var errMsg, stackTrace, errType string
	if tc.Failure != nil {
		testCase.Status = domain.StatusFailed
		errMsg = tc.Failure.Message
		stackTrace = tc.Failure.Text
		errType = tc.Failure.Type
	} else if tc.Error != nil {
		testCase.Status = domain.StatusError
		errMsg = tc.Error.Message
		stackTrace = tc.Error.Text
		errType = tc.Error.Type
	} else if tc.Skipped != nil {
		testCase.Status = domain.StatusSkipped
		errMsg = tc.Skipped.Message
	} else {
		testCase.Status = domain.StatusPassed
		if testCase.RetryCount != nil {
			testCase.IsFlaky = domain.BoolPtr(retryCount > 0)
		}
	}

	if errMsg != "" || stackTrace != "" || errType != "" {
		testCase.Error = domain.FormatError(errMsg, stackTrace, errType)
	}

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
