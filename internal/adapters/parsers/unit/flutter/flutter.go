package flutter

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses the JSON event stream of `flutter test --machine` and
// `flutter test --file-reporter json:<file>` (package:test's JSON reporter).
type Parser struct{}

// New creates a new Flutter test parser
func New() *Parser {
	return &Parser{}
}

// Case property keys; the structural ones are allowlisted by the collect
// pipeline, and system-out is the name the CTRF parser uses.
const (
	propFile      = "file"
	propLine      = "line"
	propSystemOut = "system-out"
	propPlatform  = "platform"
)

const (
	unfinishedMessage = "test did not finish (the run ended before this test reported a result)"
	// maxLineBytes bounds one stream line; exception blocks and -v output can be long.
	maxLineBytes = 16 * 1024 * 1024
)

// event is the union of the reporter's event types; only the fields the parser
// reads are declared.
type event struct {
	Type        string     `json:"type"`
	Time        int64      `json:"time"`
	Suite       *suiteInfo `json:"suite"`
	Test        *testInfo  `json:"test"`
	TestID      int        `json:"testID"`
	Result      string     `json:"result"`
	Skipped     bool       `json:"skipped"`
	Hidden      bool       `json:"hidden"`
	Error       string     `json:"error"`
	StackTrace  string     `json:"stackTrace"`
	IsFailure   bool       `json:"isFailure"`
	MessageType string     `json:"messageType"`
	Message     string     `json:"message"`
}

type suiteInfo struct {
	ID       int    `json:"id"`
	Platform string `json:"platform"`
	Path     string `json:"path"`
}

type testInfo struct {
	ID       int          `json:"id"`
	Name     string       `json:"name"`
	SuiteID  int          `json:"suiteID"`
	Metadata testMetadata `json:"metadata"`
	Line     *int         `json:"line"`
	URL      *string      `json:"url"`
	RootLine *int         `json:"root_line"`
	RootURL  *string      `json:"root_url"`
}

type testMetadata struct {
	Skip       bool    `json:"skip"`
	SkipReason *string `json:"skipReason"`
}

// testError is one `error` event, kept in the order it arrived.
type testError struct {
	message, stack string
	isFailure      bool
}

// testState is everything the stream says about one test. file is the raw
// (absolute) path, made relative once every suite path is known. prints holds
// the raw print messages.
type testState struct {
	id         int
	name, file string
	line       *int
	start, end int64
	result     string
	skipped    bool
	hidden     bool
	done       bool
	skipReason string
	errors     []testError
	prints     []string
	retries    []retryMark // one per `Retry:` print, in order
}

// retryMark records how many errors and prints a test had when a `Retry:` line
// closed one of its failed attempts.
type retryMark struct{ errors, prints int }

// Parse parses Flutter JSON reporter output. A file holding several runs
// (the reporter's `start` event resets the ID space) yields one suite.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var (
		all        []*testState // every test of every run, in start order
		tests      = map[int]*testState{}
		suitePaths = map[int]string{}
		allPaths   []string
		platform   string
		sawEvent   bool
		runStart   int64
		lastTime   int64
		total      time.Duration
	)
	endRun := func() {
		if lastTime > runStart {
			total += time.Duration(lastTime-runStart) * time.Millisecond
		}
		runStart, lastTime = 0, 0
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip anything that is not a JSON object: blank lines, build output,
		// the [{"event":"test.startedProcess"...}] arrays, -v logs.
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type == "" {
			continue
		}
		sawEvent = true
		if ev.Time > lastTime {
			lastTime = ev.Time
		}

		switch ev.Type {
		case "start":
			// A new run: its test IDs restart at 0, so drop the ID-keyed state.
			endRun()
			tests = map[int]*testState{}
			suitePaths = map[int]string{}
			runStart = ev.Time

		case "suite":
			if ev.Suite == nil {
				continue
			}
			suitePaths[ev.Suite.ID] = ev.Suite.Path
			allPaths = append(allPaths, ev.Suite.Path)
			if platform == "" {
				platform = ev.Suite.Platform
			}

		case "testStart":
			if ev.Test == nil {
				continue
			}
			st := newTestState(ev.Test, suitePaths[ev.Test.SuiteID], ev.Time)
			tests[st.id] = st
			all = append(all, st)

		case "testDone":
			if st := tests[ev.TestID]; st != nil {
				st.done = true
				st.end = ev.Time
				st.result = ev.Result
				st.skipped = ev.Skipped
				st.hidden = ev.Hidden
			}

		case "error":
			if st := tests[ev.TestID]; st != nil {
				st.errors = append(st.errors, testError{message: ev.Error, stack: ev.StackTrace, isFailure: ev.IsFailure})
			}

		case "print":
			st := tests[ev.TestID]
			if st == nil || ev.MessageType != "print" {
				continue
			}
			if ev.Message == retryPrefix+st.name {
				st.retries = append(st.retries, retryMark{len(st.errors), len(st.prints)})
			} else {
				st.prints = append(st.prints, ev.Message)
			}
		}
	}
	endRun()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading Flutter test output: %w", err)
	}
	if !sawEvent {
		return nil, errors.New("no valid Flutter test JSON events found")
	}

	root := projectRoot(allPaths)
	suite := &domain.Suite{
		Name:      "Flutter Tests",
		Category:  domain.FrameworkFlutter.GetCategory(),
		Timestamp: time.Now().UTC(),
		Duration:  total,
		Cases:     make([]domain.Case, 0, len(all)),
	}
	if platform != "" {
		suite.Properties = map[string]string{propPlatform: platform}
	}
	for _, st := range all {
		if st.hidden {
			continue
		}
		suite.Cases = append(suite.Cases, buildCase(st, root))
	}
	suite.RecomputeCounts()
	return suite, nil
}

// newTestState captures a testStart. testWidgets reports its location inside
// flutter_test, with the user's own in root_url/root_line, so those win; a
// location that is not a file (package:...) falls back to the suite's path.
func newTestState(t *testInfo, suitePath string, start int64) *testState {
	st := &testState{id: t.ID, name: t.Name, file: suitePath, start: start}
	if t.Metadata.SkipReason != nil {
		st.skipReason = *t.Metadata.SkipReason
	}
	switch {
	case t.RootURL != nil && strings.HasPrefix(*t.RootURL, "file://"):
		st.file, st.line = *t.RootURL, t.RootLine
	case t.URL != nil && strings.HasPrefix(*t.URL, "file://"):
		st.file, st.line = *t.URL, t.Line
	}
	return st
}

// buildCase turns a finished (or unfinished) test into a domain case.
func buildCase(st *testState, root string) domain.Case {
	file := relativePath(st.file, root)
	name := st.name
	// A failed load is named after the file; its raw name carries an absolute path.
	if rest, ok := strings.CutPrefix(name, "loading "); ok {
		name = "loading " + relativePath(rest, root)
	}

	c := domain.Case{
		ID:        file + "#" + name,
		Name:      name,
		ClassName: file,
		Properties: map[string]string{
			propFile: file,
		},
	}
	if st.line != nil {
		c.Properties[propLine] = strconv.Itoa(*st.line)
	}

	slices := splitAttempts(st)
	final := slices[len(slices)-1]
	finalOut := final.resolve(st.result)

	// system-out is every attempt's output, exception blocks removed.
	var outLines []string
	for i, s := range slices {
		if i == len(slices)-1 {
			outLines = append(outLines, finalOut.out...)
		} else {
			outLines = append(outLines, s.resolve("").out...)
		}
	}
	if out := strings.Join(base.ClampOutput(outLines), "\n"); out != "" {
		c.Properties[propSystemOut] = out
	}

	if !st.done {
		c.Status = domain.StatusError
		c.Error = unfinishedMessage
		return c
	}
	if d := st.end - st.start; d > 0 {
		c.Duration = time.Duration(d) * time.Millisecond
	}

	switch {
	case st.skipped:
		c.Status = domain.StatusSkipped
		c.Error = st.skipReason
	case st.result == "success":
		c.Status = domain.StatusPassed
	case st.result != "success":
		c.Status = finalOut.status
		c.Error = finalOut.text
	}
	if len(slices) > 1 {
		c.Attempts = buildAttempts(slices, c.Status, finalOut)
		c.RetryCount = domain.IntPtr(len(slices) - 1)
		c.IsFlaky = domain.BoolPtr(c.Status == domain.StatusPassed)
	}
	return c
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkFlutter
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json", ".jsonl"}
}
