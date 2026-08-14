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
	*totalDuration += suiteDuration(js)

	// First non-empty report timestamp wins, instead of stamping upload
	// wall-clock time onto every suite (BUG-14).
	if ts.IsZero() && js.Timestamp != "" {
		if parsed, ok := parseSuiteTimestamp(js.Timestamp); ok {
			*ts = parsed
		}
	}

	for _, tc := range js.TestCases {
		dst.Cases = append(dst.Cases, convertTestCase(tc))
	}
	for i := range js.TestSuites {
		collectSuite(&js.TestSuites[i], dst, totalDuration, ts)
	}
}

// suiteDuration prefers the suite-level time attribute, falling back to the sum of this
// suite's own case durations when it is absent, unparseable, or zero (BUG-13) — without
// the fallback a whole suite reports as 0s.
func suiteDuration(js *TestSuite) time.Duration {
	if d, err := base.ParseDuration(js.Time); err == nil && d > 0 {
		return d
	}
	var sum time.Duration
	for _, tc := range js.TestCases {
		if cd, cerr := base.ParseDuration(tc.Time); cerr == nil {
			sum += cd
		}
	}
	return sum
}

// parseSuiteTimestamp tries the layouts JUnit-family writers actually emit. Reports
// whether any matched, so the caller can leave the timestamp unset rather than storing
// a zero time.
func parseSuiteTimestamp(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02T15:04:05", time.RFC3339, "2006-01-02T15:04:05.999999999"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
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

	testCase.RetryCount = extractRetryCount(tc.Properties)

	status, errMsg, stackTrace, errType := caseOutcome(tc)
	testCase.Status = status
	// Only a case that eventually went green is flaky; a still-failing one is just
	// failing, however many times it was retried.
	if status == domain.StatusPassed && testCase.RetryCount != nil {
		testCase.IsFlaky = domain.BoolPtr(*testCase.RetryCount > 0)
	}

	if errMsg != "" || stackTrace != "" || errType != "" {
		testCase.Error = domain.FormatError(errMsg, stackTrace, errType)
	}

	testCase.Properties = mergeCaseProperties(tc)
	// Fallback shard mechanism (mechanism C): a JUnit-family runner that can't emit
	// Playwright's native workerIndex (mechanism A) can still report which shard ran a
	// case via a plain <property name="shard" value="..."/> — e.g. pytest's
	// record_property. Only applied when mechanism A hasn't already set ShardIndex, so a
	// more specific/native signal always wins over this generic one. A value that is
	// non-numeric, negative, or too large for the server's 32-bit shard_index column
	// is skipped silently (ShardIndex stays nil) rather than passed through, so one
	// bogus property can never poison the whole launch upload.
	if testCase.ShardIndex == nil {
		if v, ok := testCase.Properties["shard"]; ok {
			if n, valid := base.ParseShardIndex(v); valid {
				testCase.ShardIndex = domain.IntPtr(n)
			}
		}
	}

	return testCase
}

// mergeCaseProperties combines every <properties><property> pair with captured
// stdout/stderr into one map, so generic JUnit properties (pytest's
// record_property, or any other runner's custom <property> extension) reach
// domain.Case.Properties instead of being silently dropped — the same
// passthrough Suite.Properties already gets. The well-known capture keys
// always win over a same-named <property>, so a rogue property can't spoof
// captured output.
func mergeCaseProperties(tc TestCase) map[string]string {
	var out map[string]string
	for _, p := range tc.Properties {
		if p.Name == "" {
			continue
		}
		if out == nil {
			out = make(map[string]string, len(tc.Properties))
		}
		out[p.Name] = p.Value
	}
	if captured := capturedOutput(tc); captured != nil {
		if out == nil {
			out = make(map[string]string, len(captured))
		}
		for k, v := range captured {
			out[k] = v
		}
	}
	return out
}

// extractRetryCount reads the retry count from the runner-specific property names.
// Returns nil when absent or unparseable, so "no retry information" stays
// distinguishable from "zero retries". The last parseable entry wins, matching how a
// report that repeats the property is resolved.
func extractRetryCount(props []Property) *int {
	var out *int
	for _, prop := range props {
		if prop.Name != "retries" && prop.Name != "retryCount" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(prop.Value, "%d", &n); err == nil {
			out = domain.IntPtr(n)
		}
	}
	return out
}

// caseOutcome resolves the status and the error detail that goes with it. The order is
// the contract: a case carrying several outcome elements is reported by the most severe,
// so a report claiming both "failed" and "skipped" never rolls up as skipped.
func caseOutcome(tc TestCase) (status domain.Status, message, stackTrace, errType string) {
	switch {
	case tc.Failure != nil:
		return domain.StatusFailed, tc.Failure.Message, tc.Failure.Text, tc.Failure.Type
	case tc.Error != nil:
		return domain.StatusError, tc.Error.Message, tc.Error.Text, tc.Error.Type
	case tc.Skipped != nil:
		return domain.StatusSkipped, tc.Skipped.Message, "", ""
	default:
		return domain.StatusPassed, "", "", ""
	}
}

// capturedOutput returns the captured streams, or nil when there are none — a nil map
// keeps "nothing captured" distinct from "captured an empty string", and is what SEC-04
// strips when --no-capture-output is set.
func capturedOutput(tc TestCase) map[string]string {
	if tc.SystemOut == "" && tc.SystemErr == "" {
		return nil
	}
	props := make(map[string]string, 2)
	if tc.SystemOut != "" {
		props["system-out"] = tc.SystemOut
	}
	if tc.SystemErr != "" {
		props["system-err"] = tc.SystemErr
	}
	return props
}
