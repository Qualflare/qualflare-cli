package playwright

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Playwright JSON reporter output
type Parser struct{}

// Playwright JSON structures
type Report struct {
	Config Config  `json:"config"`
	Suites []Suite `json:"suites"`
	Errors []Error `json:"errors"`
	Stats  Stats   `json:"stats"`
}

type Config struct {
	ConfigFile string `json:"configFile"`
	RootDir    string `json:"rootDir"`
}

type Suite struct {
	Title  string  `json:"title"`
	File   string  `json:"file"`
	Line   int     `json:"line"`
	Column int     `json:"column"`
	Specs  []Spec  `json:"specs"`
	Suites []Suite `json:"suites"` // Nested suites
}

type Spec struct {
	Title  string   `json:"title"`
	OK     bool     `json:"ok"`
	Tags   []string `json:"tags"`
	Tests  []Test   `json:"tests"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
	Column int      `json:"column"`
}

type Test struct {
	Timeout        int          `json:"timeout"`
	Annotations    []Annotation `json:"annotations"`
	ExpectedStatus string       `json:"expectedStatus"`
	ProjectID      string       `json:"projectId"`
	ProjectName    string       `json:"projectName"`
	Results        []Result     `json:"results"`
	Status         string       `json:"status"`
}

type Annotation struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type Result struct {
	// WorkerIndex is a pointer so an absent (or null) "workerIndex" key decodes to nil
	// rather than to the zero value 0, which is a real worker index. Reports written by
	// older reporters — or hand-merged/trimmed blobs — omit the key entirely, and
	// stamping those cases with shard 0 would both invent shard data that was never
	// reported and break the invariant that ShardIndex == nil means "no shard data".
	WorkerIndex *int         `json:"workerIndex"`
	Status      string       `json:"status"`
	Duration    int          `json:"duration"` // milliseconds
	Error       *TestError   `json:"error"`
	Attachments []Attachment `json:"attachments"`
	Retry       int          `json:"retry"`
	StartTime   string       `json:"startTime"`
	Steps       []Step       `json:"steps"`
}

// Step is Playwright's own nested step-tree node.
type Step struct {
	Title    string     `json:"title"`
	Duration int        `json:"duration"` // milliseconds
	Error    *TestError `json:"error"`
	Steps    []Step     `json:"steps"` // nested sub-steps
}

type TestError struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Path        string `json:"path"`
}

type Error struct {
	Message string `json:"message"`
}

type Stats struct {
	StartTime string `json:"startTime"`
	// Duration is a float in real output (e.g. 2921.147) — unlike every
	// other duration in this report, which is always a whole millisecond
	// integer. Confirmed against a real `playwright test --reporter=json`
	// run; likely performance.now()-derived rather than Date.now()-derived.
	Duration   float64 `json:"duration"`
	Expected   int     `json:"expected"`
	Skipped    int     `json:"skipped"`
	Unexpected int     `json:"unexpected"`
	Flaky      int     `json:"flaky"`
}

// New creates a new Playwright parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Playwright JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      "Playwright Test Results",
		Category:  domain.FrameworkPlaywright.GetCategory(),
		Duration:  time.Duration(report.Stats.Duration) * time.Millisecond,
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Parse start time if available
	if report.Stats.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, report.Stats.StartTime); err == nil {
			suite.Timestamp = t
		}
	}

	// Set flaky count from stats
	suite.Flaky = report.Stats.Flaky

	// Process all suites and track retries
	browsers := make(map[string]bool)
	p.processSuites(report.Suites, suite, "", browsers)

	// Store browser in properties for Launch to use. Sorted: ranging a Go map
	// has randomized iteration order, so an unsorted join would produce a
	// different string each parse for the exact same input — which silently
	// breaks report_service.go's promoteConsistentSuiteProperties, an
	// exact-string-equality check across suites that needs the same browser
	// set to always join the same way.
	if len(browsers) > 0 {
		browserList := make([]string, 0, len(browsers))
		for b := range browsers {
			browserList = append(browserList, b)
		}
		sort.Strings(browserList)
		if suite.Properties == nil {
			suite.Properties = make(map[string]string)
		}
		if len(browserList) == 1 {
			suite.Properties["browser"] = browserList[0]
		} else {
			// Multiple browsers - join them
			suite.Properties["browser"] = strings.Join(browserList, ", ")
		}
	}

	// Rule #3: derive Passed/Failed/Skipped/Errors and TotalTests from the actual
	// case statuses so the counters can never disagree with the cases (and so
	// StatusError cases — interrupted/unknown — are counted, not dropped).
	// Flaky (from stats) and Retries (accumulated above) are left untouched.
	suite.RecomputeCounts()

	return suite, nil
}

// processSuites recursively processes Playwright suites
func (p *Parser) processSuites(suites []Suite, domainSuite *domain.Suite, prefix string, browsers map[string]bool) {
	for _, s := range suites {
		currentPrefix := s.Title
		if prefix != "" {
			currentPrefix = prefix + " > " + s.Title
		}

		for _, spec := range s.Specs {
			p.collectSpecCases(spec, s.File, currentPrefix, domainSuite, browsers)
		}

		// Recursing on an empty slice is a no-op, so no length guard is needed.
		p.processSuites(s.Suites, domainSuite, currentPrefix, browsers)
	}
}

// collectSpecCases appends one case per test in spec, collecting the browser each ran
// under and accumulating the retry count (every result beyond the first is a retry).
func (p *Parser) collectSpecCases(spec Spec, file, prefix string, dst *domain.Suite, browsers map[string]bool) {
	for _, test := range spec.Tests {
		dst.Cases = append(dst.Cases, p.convertTest(spec, test, file, prefix))

		if test.ProjectName != "" {
			browsers[test.ProjectName] = true
		}
		if len(test.Results) > 1 {
			dst.Retries += len(test.Results) - 1
		}
	}
}

// convertTest converts a Playwright test to domain.Case
func (p *Parser) convertTest(spec Spec, test Test, file string, prefix string) domain.Case {
	fullName := spec.Title
	if prefix != "" {
		fullName = prefix + " > " + spec.Title
	}

	testCase := domain.Case{
		ID:        fullName,
		Name:      spec.Title,
		ClassName: file,
		Tags:      spec.Tags,
	}

	// The last result is authoritative: it is the outcome after any retries.
	if len(test.Results) > 0 {
		lastResult := test.Results[len(test.Results)-1]
		testCase.Duration = time.Duration(lastResult.Duration) * time.Millisecond

		// A test that eventually passed after at least one retry is flaky.
		retryCount := len(test.Results) - 1
		testCase.RetryCount = domain.IntPtr(retryCount)
		testCase.IsFlaky = domain.BoolPtr(retryCount > 0 && lastResult.Status == "passed")

		testCase.Status, testCase.Error = statusFromResult(lastResult)

		// BUG-09: test.fail() marks a test as an expected failure via
		// ExpectedStatus. When the observed result matches the expected status the
		// test behaved as designed, so record it as passed while preserving the
		// captured failure message. Only applies when ExpectedStatus is populated.
		if test.ExpectedStatus != "" && lastResult.Status == test.ExpectedStatus &&
			testCase.Status == domain.StatusFailed {
			testCase.Status = domain.StatusPassed
		}

		testCase.Attachments = convertAttachments(lastResult.Attachments)
		testCase.Steps = convertSteps(lastResult.Steps)

		// Mechanism A: Playwright's own per-worker index. Only set when the report
		// actually carried one — an absent workerIndex leaves ShardIndex nil ("no shard
		// data") instead of claiming worker 0 — and only when the value fits the
		// server's 32-bit signed shard_index column.
		//
		// The bound is not redundant with JSON decoding: `int` is 64 bits on every
		// platform this CLI ships for, so "workerIndex": 2147483648 or -3 decodes
		// cleanly and would reach the wire unbounded, risking rejection of the whole
		// launch over one optional hint. Out-of-range is skipped silently, exactly as
		// the junitxml/pytest `shard` property fallback does (base.ParseShardIndex).
		if lastResult.WorkerIndex != nil && base.ValidShardIndex(*lastResult.WorkerIndex) {
			testCase.ShardIndex = domain.IntPtr(*lastResult.WorkerIndex)
		}
		if lastResult.StartTime != "" {
			if t, err := time.Parse(time.RFC3339, lastResult.StartTime); err == nil {
				testCase.StartedAt = &t
			}
		}
	} else {
		testCase.Status = statusFromTestLevel(test.Status)
	}

	// Add properties
	testCase.Properties = map[string]string{
		"file":    file,
		"project": test.ProjectName,
	}

	// Add annotations as tags
	for _, ann := range test.Annotations {
		if ann.Type != "" {
			testCase.Tags = append(testCase.Tags, ann.Type)
		}
	}

	return testCase
}

// statusFromResult maps a Playwright result status to a domain status, returning the
// formatted error alongside it. CLI-H4: "interrupted" (emitted on --max-failures or
// SIGINT) is an aborted test rather than a pass, and any status this build does not
// recognise must land fail-visible too — neither may ever roll up green.
func statusFromResult(r Result) (domain.Status, string) {
	switch r.Status {
	case "passed":
		return domain.StatusPassed, ""
	case "failed", "timedOut":
		var msg string
		if r.Error != nil {
			msg = domain.FormatError(r.Error.Message, r.Error.Stack, "")
		}
		return domain.StatusFailed, msg
	case "skipped":
		return domain.StatusSkipped, ""
	default:
		return domain.StatusError, ""
	}
}

// statusFromTestLevel maps the test-level status used when a test produced no results
// at all. Same CLI-H4 rule: an unrecognised status is an error, not a pass.
func statusFromTestLevel(status string) domain.Status {
	switch status {
	case "expected":
		return domain.StatusPassed
	case "unexpected":
		return domain.StatusFailed
	case "skipped":
		return domain.StatusSkipped
	default:
		return domain.StatusError
	}
}

// convertSteps flattens Playwright's step tree into domain.Step entries.
// Confirmed against a real `playwright test --reporter=json` run: the step
// tree already contains only user-authored test.step() boundaries — a bare,
// unwrapped action (e.g. a page.goto() or expect() call outside any
// test.step()) produces no tree entry at all, so there is no engine noise
// to filter here (an earlier version of this code assumed a "category"
// field distinguished "test.step" boundaries from engine actions like
// "pw:api"/"hook" mixed into the same array; real output never populates
// that field). A test that never calls test.step() produces nil, unchanged
// from prior behavior. Nested test.step() calls are flattened into sibling
// entries in tree order, since domain.Step is flat.
func convertSteps(steps []Step) []domain.Step {
	out := make([]domain.Step, 0, len(steps))
	for _, s := range steps {
		step := domain.Step{
			Name:     s.Title,
			Duration: time.Duration(s.Duration) * time.Millisecond,
		}
		if s.Error != nil {
			step.Status = domain.StatusFailed
			step.Error = domain.FormatError(s.Error.Message, s.Error.Stack, "")
		} else {
			step.Status = domain.StatusPassed
		}
		out = append(out, step)
		out = append(out, convertSteps(s.Steps)...)
	}
	return out
}

// convertAttachments maps Playwright's contentType onto the domain's MimeType. Returns
// nil rather than an empty slice when there are none.
func convertAttachments(atts []Attachment) []domain.Attachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]domain.Attachment, 0, len(atts))
	for _, att := range atts {
		out = append(out, domain.Attachment{
			Name:     att.Name,
			Path:     att.Path,
			MimeType: att.ContentType,
		})
	}
	return out
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkPlaywright
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
