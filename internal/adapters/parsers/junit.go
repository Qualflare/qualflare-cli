package parsers

import (
	"encoding/xml"
	"io"
	"qualflare-cli/internal/core/domain"
	"strconv"
	"time"
)

type JUnitParser struct{}

type JUnitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	TestSuites []JUnitTestSuite `xml:"testsuite"`
}

type JUnitTestSuite struct {
	XMLName   xml.Name        `xml:"testsuite"`
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Skipped   int             `xml:"skipped,attr"`
	Time      string          `xml:"time,attr"`
	TestCases []JUnitTestCase `xml:"testcase"`
}

type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Error     *JUnitError   `xml:"error,omitempty"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
}

type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type JUnitError struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type JUnitSkipped struct {
	Message string `xml:"message,attr"`
}

func NewJUnitParser() *JUnitParser {
	return &JUnitParser{}
}

func (p *JUnitParser) Parse(reader io.Reader) (*domain.Suite, error) {
	var testSuites JUnitTestSuites
	decoder := xml.NewDecoder(reader)

	if err := decoder.Decode(&testSuites); err != nil {
		// Try parsing as a single test suite
		if _, err := reader.(io.Seeker).Seek(0, 0); err != nil {
			return nil, err
		}

		var singleSuite JUnitTestSuite
		decoder = xml.NewDecoder(reader)
		if err := decoder.Decode(&singleSuite); err != nil {
			return nil, err
		}
		testSuites.TestSuites = []JUnitTestSuite{singleSuite}
	}

	// Convert to domain model (taking the first test suite for simplicity)
	if len(testSuites.TestSuites) == 0 {
		return &domain.Suite{}, nil
	}

	junitSuite := testSuites.TestSuites[0]
	suite := &domain.Suite{
		Name:       junitSuite.Name,
		TotalTests: junitSuite.Tests,
		Failed:     junitSuite.Failures + junitSuite.Errors,
		Skipped:    junitSuite.Skipped,
		Timestamp:  time.Now(),
		Cases:      make([]domain.Case, 0, len(junitSuite.TestCases)),
	}

	suite.Passed = suite.TotalTests - suite.Failed - suite.Skipped

	if duration, err := parseTime(junitSuite.Time); err == nil {
		suite.Duration = duration
	}

	for _, testCase := range junitSuite.TestCases {
		test := domain.Case{
			ID:   testCase.Classname + "." + testCase.Name,
			Name: testCase.Name,
		}

		if duration, err := parseTime(testCase.Time); err == nil {
			test.Duration = duration
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

		suite.Cases = append(suite.Cases, test)
	}

	return suite, nil
}

func (p *JUnitParser) GetFramework() domain.Framework {
	return domain.FrameworkJUnit
}

func (p *JUnitParser) SupportedFileExtensions() []string {
	return []string{".xml"}
}

func parseTime(timeStr string) (time.Duration, error) {
	if timeStr == "" {
		return 0, nil
	}

	seconds, err := strconv.ParseFloat(timeStr, 64)
	if err != nil {
		return 0, err
	}

	return time.Duration(seconds * float64(time.Second)), nil
}
