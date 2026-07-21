// Package junitxml parses JUnit XML test results. It is shared by all
// JUnit-compatible parsers (junit, testng, maestro, xctest, espresso, etc.)
// so each framework only needs a thin wrapper that stamps its own Framework.
package junitxml

import (
	"encoding/xml"
	"fmt"
	"io"
	"time"

	"golang.org/x/net/html/charset"

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

// TestSuite represents a single <testsuite> element. A <testsuite> may itself
// contain nested <testsuite> children (Maven Surefire aggregate reports, several
// Android/iOS JUnit converters); TestSuites captures them so their cases are not
// silently dropped (CLI-H8).
type TestSuite struct {
	XMLName    xml.Name    `xml:"testsuite"`
	Name       string      `xml:"name,attr"`
	Tests      int         `xml:"tests,attr"`
	Failures   int         `xml:"failures,attr"`
	Errors     int         `xml:"errors,attr"`
	Skipped    int         `xml:"skipped,attr"`
	Time       string      `xml:"time,attr"`
	Timestamp  string      `xml:"timestamp,attr"`
	TestCases  []TestCase  `xml:"testcase"`
	TestSuites []TestSuite `xml:"testsuite"`
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
	// Honour a non-UTF-8 encoding declared in the XML prolog (ISO-8859-1,
	// UTF-16, ...) instead of hard-erroring on the whole upload (BUG-12).
	decoder.CharsetReader = charset.NewReaderLabel

	if err := decoder.Decode(&testSuites); err != nil {
		// Try parsing as a single <testsuite> root element
		if seeker, ok := reader.(io.Seeker); ok {
			if _, serr := seeker.Seek(0, 0); serr != nil {
				return nil, serr
			}
		}

		var singleSuite TestSuite
		decoder = xml.NewDecoder(reader)
		decoder.CharsetReader = charset.NewReaderLabel
		if derr := decoder.Decode(&singleSuite); derr != nil {
			return nil, derr
		}
		testSuites.TestSuites = []TestSuite{singleSuite}
	}

	if len(testSuites.TestSuites) == 0 {
		// Cases must be a non-nil slice: the server validates it as `required`,
		// and a nil slice marshals to `null`, 400-ing the whole multi-file
		// upload (SYNC-06). Category is stamped so an empty file still declares
		// its framework family.
		return &domain.Suite{
			Name:      "Empty Suite",
			Category:  framework.GetCategory(),
			Timestamp: time.Now().UTC(),
			Cases:     make([]domain.Case, 0),
		}, nil
	}

	return mergeSuites(testSuites, framework), nil
}

func mergeSuites(testSuites TestSuites, framework domain.Framework) *domain.Suite {
	suite := &domain.Suite{
		Name:     base.CoalesceString(testSuites.Name, "JUnit Test Results"),
		Category: framework.GetCategory(),
		Cases:    make([]domain.Case, 0),
	}

	var totalDuration time.Duration
	var suiteTimestamp time.Time

	for i := range testSuites.TestSuites {
		collectSuite(&testSuites.TestSuites[i], suite, &totalDuration, &suiteTimestamp)
	}

	suite.Duration = totalDuration
	if suiteTimestamp.IsZero() {
		suite.Timestamp = time.Now().UTC()
	} else {
		suite.Timestamp = suiteTimestamp
	}
	// Counters are derived from the (possibly deeply nested) cases so they can
	// never disagree with the case list.
	suite.RecomputeCounts()

	return suite
}

// collectSuite appends js's cases — and, recursively, the cases of any nested
// <testsuite> children — into dst. Without the recursion, tests inside nested
// suites (and their failures) are silently dropped (CLI-H8).
func collectSuite(js *TestSuite, dst *domain.Suite, totalDuration *time.Duration, ts *time.Time) {
	// Prefer the suite-level time attribute; fall back to the sum of this
	// suite's own case durations when it is absent or unparseable (BUG-13).
	if d, err := base.ParseDuration(js.Time); err == nil && d > 0 {
		*totalDuration += d
	} else {
		for _, tc := range js.TestCases {
			if cd, cerr := base.ParseDuration(tc.Time); cerr == nil {
				*totalDuration += cd
			}
		}
	}

	// First non-empty report timestamp wins, instead of stamping upload
	// wall-clock time onto every suite (BUG-14).
	if ts.IsZero() && js.Timestamp != "" {
		for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339, "2006-01-02T15:04:05.999999999"} {
			if parsed, err := time.Parse(layout, js.Timestamp); err == nil {
				*ts = parsed
				break
			}
		}
	}

	for _, tc := range js.TestCases {
		dst.Cases = append(dst.Cases, convertTestCase(tc))
	}
	for i := range js.TestSuites {
		collectSuite(&js.TestSuites[i], dst, totalDuration, ts)
	}
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
