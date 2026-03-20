package phpunit

import (
	"encoding/xml"
	"io"
	"strconv"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses PHPUnit XML output
type Parser struct{}

// PHPUnit XML structures (similar to JUnit but with some differences)
type TestSuites struct {
	XMLName    xml.Name    `xml:"testsuites"`
	TestSuites []TestSuite `xml:"testsuite"`
}

type TestSuite struct {
	XMLName    xml.Name    `xml:"testsuite"`
	Name       string      `xml:"name,attr"`
	File       string      `xml:"file,attr"`
	Tests      int         `xml:"tests,attr"`
	Assertions int         `xml:"assertions,attr"`
	Errors     int         `xml:"errors,attr"`
	Failures   int         `xml:"failures,attr"`
	Skipped    int         `xml:"skipped,attr"`
	Time       string      `xml:"time,attr"`
	TestCases  []TestCase  `xml:"testcase"`
	TestSuites []TestSuite `xml:"testsuite"` // Nested test suites
}

type TestCase struct {
	Name       string   `xml:"name,attr"`
	Class      string   `xml:"class,attr"`
	Classname  string   `xml:"classname,attr"`
	File       string   `xml:"file,attr"`
	Line       int      `xml:"line,attr"`
	Assertions int      `xml:"assertions,attr"`
	Time       string   `xml:"time,attr"`
	Failure    *Failure `xml:"failure,omitempty"`
	Error      *Error   `xml:"error,omitempty"`
	Skipped    *Skipped `xml:"skipped,omitempty"`
	SystemOut  string   `xml:"system-out,omitempty"`
	SystemErr  string   `xml:"system-err,omitempty"`
}

type Failure struct {
	Type    string `xml:"type,attr"`
	Message string `xml:",chardata"`
}

type Error struct {
	Type    string `xml:"type,attr"`
	Message string `xml:",chardata"`
}

type Skipped struct {
	Message string `xml:",chardata"`
}

// New creates a new PHPUnit parser
func New() *Parser {
	return &Parser{}
}

// Parse parses PHPUnit XML content
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

	suite := &domain.Suite{
		Name:      "PHPUnit Test Results",
		Category:  domain.FrameworkPHPUnit.GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Process all test suites recursively
	p.processSuites(testSuites.TestSuites, suite)

	suite.TotalTests = len(suite.Cases)

	return suite, nil
}

// processSuites recursively processes test suites
func (p *Parser) processSuites(suites []TestSuite, domainSuite *domain.Suite) {
	for _, s := range suites {
		// Process test cases in this suite
		for _, tc := range s.TestCases {
			testCase := p.convertTestCase(tc)
			domainSuite.Cases = append(domainSuite.Cases, testCase)

			// Update counters
			switch testCase.Status {
			case domain.StatusPassed:
				domainSuite.Passed++
			case domain.StatusFailed, domain.StatusError:
				domainSuite.Failed++
			case domain.StatusSkipped:
				domainSuite.Skipped++
			}
		}

		// Parse duration
		if duration, err := base.ParseDuration(s.Time); err == nil {
			domainSuite.Duration += duration
		}

		// Process nested test suites
		if len(s.TestSuites) > 0 {
			p.processSuites(s.TestSuites, domainSuite)
		}
	}
}

// convertTestCase converts a PHPUnit test case to domain.Case
func (p *Parser) convertTestCase(tc TestCase) domain.Case {
	testCase := domain.Case{
		ID:        tc.Class + "::" + tc.Name,
		Name:      tc.Name,
		ClassName: base.CoalesceString(tc.Classname, tc.Class),
	}

	// Parse duration
	if duration, err := base.ParseDuration(tc.Time); err == nil {
		testCase.Duration = duration
	}

	// Determine status
	if tc.Failure != nil {
		testCase.Status = domain.StatusFailed
		testCase.ErrorMessage = tc.Failure.Message
		testCase.ErrorType = tc.Failure.Type
	} else if tc.Error != nil {
		testCase.Status = domain.StatusError
		testCase.ErrorMessage = tc.Error.Message
		testCase.ErrorType = tc.Error.Type
	} else if tc.Skipped != nil {
		testCase.Status = domain.StatusSkipped
		testCase.ErrorMessage = tc.Skipped.Message
	} else {
		testCase.Status = domain.StatusPassed
	}

	// Add properties
	if tc.File != "" {
		testCase.Properties = map[string]string{
			"file": tc.File,
		}
		if tc.Line > 0 {
			testCase.Properties["line"] = strconv.Itoa(tc.Line)
		}
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkPHPUnit
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".xml"}
}
