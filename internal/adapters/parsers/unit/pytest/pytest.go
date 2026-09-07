package pytest

import (
	"bytes"
	"encoding/xml"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses pytest XML output
type Parser struct{}

// TestSuites is pytest's ACTUAL root element.
//
// pytest has wrapped its JUnit output in <testsuites> since 6.0 (2020); only
// before that was the root a bare <testsuite>. This parser decoded the old shape
// alone, so a report from any supported pytest version failed the whole upload
// with "expected element type <testsuite> but have <testsuites>" -- and content
// detection routes any JUnit-ish XML containing the word "pytest" here, so
// `pytest --junitxml=results.xml && qf <id> collect results.xml` could not work
// at all. Every test in this package used a hand-written bare-root fixture,
// which is why it went unnoticed.
type TestSuites struct {
	XMLName    xml.Name    `xml:"testsuites"`
	Name       string      `xml:"name,attr"`
	Time       string      `xml:"time,attr"`
	TestSuites []TestSuite `xml:"testsuite"`
}

// Python pytest-xml structures
type TestSuite struct {
	XMLName  xml.Name `xml:"testsuite"`
	Name     string   `xml:"name,attr"`
	Tests    int      `xml:"tests,attr"`
	Failures int      `xml:"failures,attr"`
	Errors   int      `xml:"errors,attr"`
	// BUG-38: pytest emits the header attribute `skipped`, not `skips`. Reading
	// the nonexistent `skips` left this 0 and inflated the passed count. Counters
	// are now derived from the cases (RecomputeCounts) instead of this header,
	// but keep the correct tag for anyone reading the raw value. Skips remains a
	// fallback for ancient pytest that emitted `skips`.
	Skipped    int        `xml:"skipped,attr"`
	Skips      int        `xml:"skips,attr"`
	Time       string     `xml:"time,attr"`
	Timestamp  string     `xml:"timestamp,attr"`
	TestCases  []TestCase `xml:"testcase"`
	Properties []Property `xml:"properties>property"`
	// A <testsuite> may itself contain nested <testsuite> children -- merged
	// multi-run reports and several xdist/CI aggregators produce them. Captured
	// so their cases are not silently dropped, matching junitxml (CLI-H8).
	TestSuites []TestSuite `xml:"testsuite"`
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

// Parse parses pytest XML content.
//
// Both root shapes are accepted: the <testsuites> wrapper every supported pytest
// emits, and the bare <testsuite> of pytest < 6.0. The content is read into
// memory first so the second attempt starts from the beginning regardless of
// whether the reader happens to be an io.Seeker -- a non-seekable reader would
// otherwise resume mid-document and fail for a second, misleading reason.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	var root TestSuites
	if derr := xml.NewDecoder(bytes.NewReader(content)).Decode(&root); derr != nil {
		var single TestSuite
		if serr := xml.NewDecoder(bytes.NewReader(content)).Decode(&single); serr != nil {
			// Report the wrapper error: it names the root actually found, which
			// is the more useful of the two for a malformed file.
			return nil, derr
		}
		root.TestSuites = []TestSuite{single}
	}

	suite := &domain.Suite{
		Name:      suiteName(root),
		Category:  domain.FrameworkPython.GetCategory(),
		Timestamp: time.Now().UTC(),
		// Non-nil: the server validates `cases` as required, and a nil slice
		// marshals to null and 400s the whole upload.
		Cases: make([]domain.Case, 0),
	}

	// Prefer the wrapper's own time when it declares one; otherwise total the
	// suites, so a multi-suite report reports the whole run rather than its
	// first part.
	if duration, derr := base.ParseDuration(root.Time); derr == nil && root.Time != "" {
		suite.Duration = duration
	} else {
		var total time.Duration
		for i := range root.TestSuites {
			if d, terr := base.ParseDuration(root.TestSuites[i].Time); terr == nil {
				total += d
			}
		}
		suite.Duration = total
	}

	for i := range root.TestSuites {
		p.collect(&root.TestSuites[i], suite)
	}

	// BUG-38: derive Passed/Failed/Skipped/Errors/TotalTests from the actual case
	// statuses so a skipped (or failed) case can never be counted as passed.
	suite.RecomputeCounts()

	return suite, nil
}

// suiteName keeps the single-suite name that reports carried before <testsuites>
// was understood, so an existing launch's suite does not silently rename itself.
func suiteName(root TestSuites) string {
	if len(root.TestSuites) == 1 && root.TestSuites[0].Name != "" {
		return root.TestSuites[0].Name
	}
	if root.Name != "" {
		return root.Name
	}
	return "pytest"
}

// collect flattens one <testsuite> and everything nested beneath it into dst.
func (p *Parser) collect(ts *TestSuite, dst *domain.Suite) {
	if len(ts.Properties) > 0 {
		if dst.Properties == nil {
			dst.Properties = make(map[string]string)
		}
		for _, prop := range ts.Properties {
			dst.Properties[prop.Name] = prop.Value
		}
	}

	for _, tc := range ts.TestCases {
		dst.Cases = append(dst.Cases, p.convertTestCase(tc))
	}

	for i := range ts.TestSuites {
		p.collect(&ts.TestSuites[i], dst)
	}
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

	// Fallback shard mechanism (mechanism C): pytest-xdist can't emit Playwright's
	// native workerIndex (mechanism A), but a conftest using record_property("shard", ...)
	// can still report which shard ran a case via a plain <property name="shard" value="..."/>.
	// Mirrors junitxml.convertTestCase exactly: applied only when nothing has already set
	// ShardIndex (no earlier path here does today — the guard keeps a future native signal
	// winning over this generic one), and a value that is non-numeric, negative, or too
	// large for the server's 32-bit shard_index column is skipped silently, leaving
	// ShardIndex nil rather than emitting a number the server cannot store.
	if testCase.ShardIndex == nil {
		if v, ok := testCase.Properties["shard"]; ok {
			if n, valid := base.ParseShardIndex(v); valid {
				testCase.ShardIndex = domain.IntPtr(n)
			}
		}
	}

	// Add file and line info
	if tc.File != "" {
		testCase.Properties["file"] = tc.File
	}
	if tc.Line != "" {
		testCase.Properties["line"] = tc.Line
	}

	// Determine status
	var errMsg, stackTrace, errType string
	switch {
	case tc.Failure != nil:
		testCase.Status = domain.StatusFailed
		errMsg = tc.Failure.Message
		stackTrace = tc.Failure.Text
		errType = tc.Failure.Type
	case tc.Error != nil:
		testCase.Status = domain.StatusError
		errMsg = tc.Error.Message
		stackTrace = tc.Error.Text
		errType = tc.Error.Type
	case tc.Skipped != nil:
		testCase.Status = domain.StatusSkipped
		errMsg = tc.Skipped.Message
	default:
		testCase.Status = domain.StatusPassed
	}

	if errMsg != "" || stackTrace != "" || errType != "" {
		testCase.Error = domain.FormatError(errMsg, stackTrace, errType)
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
