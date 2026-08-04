package newman

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Newman (Postman CLI) JSON output
type Parser struct{}

// Newman JSON structures
type Report struct {
	Collection Collection `json:"collection"`
	Run        Run        `json:"run"`
}

type Collection struct {
	Info Info `json:"info"`
}

type Info struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Run struct {
	Stats      Stats       `json:"stats"`
	Timings    Timings     `json:"timings"`
	Executions []Execution `json:"executions"`
	Failures   []Failure   `json:"failures"`
}

type Stats struct {
	Iterations  StatItem `json:"iterations"`
	Items       StatItem `json:"items"`
	Scripts     StatItem `json:"scripts"`
	Prerequests StatItem `json:"prerequests"`
	Requests    StatItem `json:"requests"`
	Tests       StatItem `json:"tests"`
	Assertions  StatItem `json:"assertions"`
	TestScripts StatItem `json:"testScripts"`
}

type StatItem struct {
	Total   int `json:"total"`
	Pending int `json:"pending"`
	Failed  int `json:"failed"`
}

type Timings struct {
	ResponseAverage int   `json:"responseAverage"`
	ResponseMin     int   `json:"responseMin"`
	ResponseMax     int   `json:"responseMax"`
	ResponseSD      int   `json:"responseSD"`
	DnsMean         int   `json:"dnsMean"`
	FirstByteMean   int   `json:"firstByteMean"`
	Started         int64 `json:"started"`
	Completed       int64 `json:"completed"`
}

type Execution struct {
	Item       Item        `json:"item"`
	Request    Request     `json:"request"`
	Response   Response    `json:"response"`
	Assertions []Assertion `json:"assertions"`
}

type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Request struct {
	URL    interface{} `json:"url"` // Can be string or object
	Method string      `json:"method"`
}

type Response struct {
	Code         int    `json:"code"`
	Status       string `json:"status"`
	ResponseTime int    `json:"responseTime"`
	ResponseSize int    `json:"responseSize"`
}

type Assertion struct {
	Assertion string `json:"assertion"`
	Skipped   bool   `json:"skipped"`
	Error     *Error `json:"error,omitempty"`
}

type Failure struct {
	Error  Error       `json:"error"`
	At     string      `json:"at"`
	Source Item        `json:"source"`
	Parent Item        `json:"parent"`
	Cursor interface{} `json:"cursor"`
}

type Error struct {
	Name    string `json:"name"`
	Index   int    `json:"index"`
	Test    string `json:"test"`
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

// New creates a new Newman parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Newman JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	duration := time.Duration(0)
	if report.Run.Timings.Completed > 0 && report.Run.Timings.Started > 0 {
		duration = time.Duration(report.Run.Timings.Completed-report.Run.Timings.Started) * time.Millisecond
	}

	suite := &domain.Suite{
		Name:      base.CoalesceString(report.Collection.Info.Name, "Newman Test Results"),
		Category:  domain.FrameworkNewman.GetCategory(),
		Duration:  duration,
		Timestamp: time.UnixMilli(report.Run.Timings.Started),
		// Assertions is the assertion-level total from the stats header; unlike the
		// pass/fail counters it is orthogonal to case status, so it stays as-is.
		Assertions: report.Run.Stats.Assertions.Total,
		Cases:      make([]domain.Case, 0),
	}

	// Create a map of failures for quick lookup
	failureMap := make(map[string][]Failure)
	for _, f := range report.Run.Failures {
		key := f.Source.ID
		failureMap[key] = append(failureMap[key], f)
	}

	// Process executions
	for _, exec := range report.Run.Executions {
		testCase := p.convertExecution(exec, failureMap)
		suite.Cases = append(suite.Cases, testCase)
	}

	// Counters (rule 3): derive Passed/Failed/Skipped/Errors and TotalTests from
	// the per-request case statuses rather than the assertion-level stats header,
	// so the suite totals can never disagree with the cases (the source of truth
	// the server recomputes from). Assertions is left as set above.
	suite.RecomputeCounts()

	return suite, nil
}

// convertExecution converts a Newman execution to domain.Case
func (p *Parser) convertExecution(exec Execution, failureMap map[string][]Failure) domain.Case {
	testCase := domain.Case{
		ID:       exec.Item.ID,
		Name:     exec.Item.Name,
		Duration: time.Duration(exec.Response.ResponseTime) * time.Millisecond,
	}

	allPassed, anyRan, errorMsgs := evaluateAssertions(exec.Assertions)

	// The run-level failure map is a second source of failures, keyed by item ID. The
	// key being present is what marks the request failed, even with no messages.
	var stackTrace string
	if failures, ok := failureMap[exec.Item.ID]; ok {
		allPassed = false
		for _, f := range failures {
			errorMsgs = append(errorMsgs, f.Error.Message)
			if stackTrace == "" {
				stackTrace = f.Error.Stack
			}
		}
	}

	switch {
	case !allPassed:
		testCase.Status = domain.StatusFailed
		if len(errorMsgs) > 0 {
			testCase.Error = domain.FormatError(errorMsgs[0], stackTrace, "")
		}
	case len(exec.Assertions) > 0 && !anyRan:
		// BUG-06: assertions were present but every one was skipped, so the request
		// was never actually verified — mark it skipped instead of passing it. A
		// request with genuinely zero assertions still falls through to passed.
		testCase.Status = domain.StatusSkipped
	default:
		testCase.Status = domain.StatusPassed
	}

	// Add properties
	testCase.Properties = map[string]string{
		"method":       exec.Request.Method,
		"responseCode": fmt.Sprintf("%d", exec.Response.Code),
		"responseTime": fmt.Sprintf("%dms", exec.Response.ResponseTime),
	}

	return testCase
}

// evaluateAssertions summarises a request's assertions: whether none failed, whether any
// actually ran, and the messages the failures carried.
//
// BUG-06: anyRan tracks NON-skipped assertions specifically. The original flag was only
// ever set inside this loop, which made the later "all skipped" guard unreachable and let
// a request whose assertions were every one skipped fall through to passed.
func evaluateAssertions(assertions []Assertion) (allPassed, anyRan bool, errorMsgs []string) {
	allPassed = true
	for _, assertion := range assertions {
		if assertion.Skipped {
			continue
		}
		anyRan = true
		if assertion.Error != nil {
			allPassed = false
			errorMsgs = append(errorMsgs, assertion.Error.Message)
		}
	}
	return allPassed, anyRan, errorMsgs
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkNewman
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
