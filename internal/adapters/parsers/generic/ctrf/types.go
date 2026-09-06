// Package ctrf parses the Common Test Report Format (https://ctrf.io), a
// tool-agnostic JSON interchange format for test results.
//
// # Why this is a generic parser
//
// CTRF sits alongside generic/junit, not with the tool-specific parsers: it is
// an interchange format with roughly fifteen producers and no single owning
// tool, exactly like JUnit XML. It is NOT in native/ — that directory is for
// Qualflare's own wire shape, where the source IS the contract and needs no
// translation.
//
// What it unlocks is mostly reach. CTRF has first-party reporters for wdio,
// jasmine, nightwatch, codeceptjs and the .NET runners (MSTest, NUnit, xUnit),
// none of which Qualflare supports directly today — .NET in particular can
// currently only arrive as generic JUnit XML.
//
// # Decoding is deliberately lenient
//
// The normative artifact is the JSON Schema in github.com/ctrf-io/ctrf, not the
// website, and it reads `Version: 0.0.0 · Status: Working Draft` with no tags
// and no releases. The published docs are STALE against it: they show a shape
// with no `reportFormat` and no `specVersion`, and type `buildNumber` as a
// string where the schema says integer. Both shapes are in the wild, so both
// must decode.
//
// The spec also says consumers MUST reject unknown properties. We decline: a
// strictly conformant consumer rejects most files that actually exist. Anything
// unparseable degrades to a zero value rather than failing the file.
package ctrf

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Report is a CTRF document. Only the fields this parser consumes are declared;
// unknown keys are ignored by encoding/json, which is the leniency described
// above.
type Report struct {
	// ReportFormat is required by the current schema and absent from the legacy
	// shape. Validated only when present, so both parse.
	ReportFormat string `json:"reportFormat"`
	// SpecVersion is decoded and then IGNORED for behaviour: every producer in
	// the wild hardcodes "0.0.0", so gating on it would be gating on a constant.
	SpecVersion string   `json:"specVersion"`
	ReportID    string   `json:"reportId"`
	RunID       string   `json:"runId"`
	Timestamp   string   `json:"timestamp"`
	GeneratedBy string   `json:"generatedBy"`
	Results     *Results `json:"results"`
}

type Results struct {
	Tool        *Tool        `json:"tool"`
	Summary     *Summary     `json:"summary"`
	Environment *Environment `json:"environment"`
	Tests       []Test       `json:"tests"`
}

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Summary is decoded for its timing window only. The counters are deliberately
// NOT used: Suite.RecomputeCounts derives every total from the cases, so a
// summary that disagrees with tests[] cannot roll a red run green.
type Summary struct {
	Start    Number `json:"start"`
	Stop     Number `json:"stop"`
	Duration Number `json:"duration"`
	Flaky    int    `json:"flaky"`
}

type Environment struct {
	ReportName      string `json:"reportName"`
	OSPlatform      string `json:"osPlatform"`
	OSRelease       string `json:"osRelease"`
	OSVersion       string `json:"osVersion"`
	TestEnvironment string `json:"testEnvironment"`
	BranchName      string `json:"branchName"`
	Commit          string `json:"commit"`
	BuildNumber     Number `json:"buildNumber"`
	BuildURL        string `json:"buildUrl"`
	ShardID         string `json:"shardId"`
}

type Test struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Duration Number `json:"duration"`

	// TestID is stable across runs and supersedes the legacy uuid-only ID. It
	// is the right source for the identity flaky history keys on.
	ID          string `json:"id"`
	TestID      string `json:"testId"`
	ExecutionID string `json:"executionId"`

	// Start and Stop are epoch MILLISECONDS.
	Start Number `json:"start"`
	Stop  Number `json:"stop"`

	// Suite is the hierarchy path, top-level first. CTRF has no suite OBJECT at
	// all; this array is the entire hierarchy model.
	Suite Strings `json:"suite"`

	Message   string `json:"message"`
	Trace     string `json:"trace"`
	Snippet   string `json:"snippet"`
	Line      Number `json:"line"`
	RawStatus string `json:"rawStatus"`

	Tags   Strings                    `json:"tags"`
	Labels map[string]json.RawMessage `json:"labels"`

	Type     string `json:"type"`
	FilePath string `json:"filePath"`

	Retries Number `json:"retries"`
	Flaky   *bool  `json:"flaky"`
	// RetryAttempts is CTRF's per-attempt history. Per the spec it holds only the
	// attempts BEFORE the last one -- the test object itself is the final attempt --
	// so a mapper has to append the test before this becomes the wire model's
	// `attempts`. See applyRetries.
	//
	// Its length is also the observed retry count when that disagrees with
	// `retries`: deployed reporters pin an older ctrf release than the spec, so
	// the two can legitimately differ.
	RetryAttempts []RetryAttempt `json:"retryAttempts"`

	Stdout Strings `json:"stdout"`
	Stderr Strings `json:"stderr"`

	ThreadID string `json:"threadId"`
	Browser  string `json:"browser"`
	Device   string `json:"device"`

	// Screenshot is base64 image CONTENT. The spec says it MUST NOT be a URL or
	// a path; producers violate that, so it is sniffed before being trusted.
	Screenshot  string                     `json:"screenshot"`
	Attachments []Attachment               `json:"attachments"`
	Parameters  map[string]json.RawMessage `json:"parameters"`
	Steps       []Step                     `json:"steps"`
}

// Attachment is a REFERENCE, never content: the schema has no bytes field.
// Path is explicitly opaque — a URL or a filesystem path, indistinguishable.
type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Path        string `json:"path"`
}

// Step carries ONLY a name and a status. No timing, no keyword, no nesting.
type Step struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Number tolerates a JSON number, a numeric string, or null.
//
// This exists because the CTRF ecosystem contradicts itself: buildNumber is
// `integer` in the schema and a string in every published example, and durations
// are `integer` milliseconds but arrive as floats from reporters wrapping
// float-timed runtimes. A value that is present but unusable decodes as unset
// rather than failing the file.
type Number struct {
	set   bool
	value float64
	text  string
}

func (n Number) IsSet() bool    { return n.set }
func (n Number) Int64() int64   { return int64(n.value) }
func (n Number) Float() float64 { return n.value }

// String returns the original text when the value arrived as a string,
// otherwise the number without a trailing ".0" for whole values.
func (n Number) String() string {
	if !n.set {
		return ""
	}
	if n.text != "" {
		return n.text
	}
	if n.value == float64(int64(n.value)) {
		return strconv.FormatInt(int64(n.value), 10)
	}
	return strconv.FormatFloat(n.value, 'f', -1, 64)
}

func (n *Number) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	// Swallowing the decode error is the entire point of this type: a field
	// that is present but unusable leaves the Number unset, so ONE malformed
	// value cannot reject a whole report. Returning the error would make this
	// decoder strict, which is exactly what the package comment says not to do.
	if b[0] == '"' {
		var s string
		if json.Unmarshal(b, &s) != nil {
			return nil //nolint:nilerr // unusable value -> unset, never an error
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil //nolint:nilerr // a non-numeric string is a producer bug, not a bad file
		}
		n.set, n.value, n.text = true, v, s
		return nil
	}
	var v float64
	if json.Unmarshal(b, &v) != nil {
		return nil //nolint:nilerr // unusable value -> unset, never an error
	}
	n.set, n.value = true, v
	return nil
}

// Strings is a permissive []string.
//
// The schema types suite, tags, stdout and stderr as string arrays, but a bare
// string appears in real reporter output for all four — most often stdout and
// stderr, where the wrapped runtime hands the reporter one blob rather than
// lines. A heterogeneous array also occurs where user-supplied tag values are
// forwarded unconverted.
// RetryAttempt is one earlier execution of a test, as CTRF reports it.
//
// CTRF models an attempt with the same field names as a test, so the subset
// mirrored here is the subset the wire model's Attempt can carry. Anything else
// a producer emits is ignored rather than guessed at.
type RetryAttempt struct {
	Attempt   Number  `json:"attempt"`
	Status    string  `json:"status"`
	RawStatus string  `json:"rawStatus"`
	Duration  Number  `json:"duration"`
	Start     Number  `json:"start"`
	AttemptID string  `json:"attemptId"`
	Message   string  `json:"message"`
	Trace     string  `json:"trace"`
	Snippet   string  `json:"snippet"`
	Line      *int    `json:"line"`
	Stdout    Strings `json:"stdout"`
	Stderr    Strings `json:"stderr"`
}

type Strings []string

func (l *Strings) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	if b[0] == '[' {
		var raw []json.RawMessage
		if json.Unmarshal(b, &raw) != nil {
			return nil //nolint:nilerr // unusable value -> empty, never an error
		}
		out := make(Strings, 0, len(raw))
		for _, item := range raw {
			if s, ok := scalarToString(item); ok {
				out = append(out, s)
			}
		}
		*l = out
		return nil
	}
	if s, ok := scalarToString(b); ok {
		*l = Strings{s}
	}
	return nil
}

// scalarToString renders a JSON scalar as a string, reporting false for null and
// for composites so those are skipped rather than stringified into noise.
func scalarToString(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", false
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	case '{', '[':
		return "", false
	case 't':
		return "true", true
	case 'f':
		return "false", true
	default:
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			return "", false
		}
		if f == float64(int64(f)) {
			return strconv.FormatInt(int64(f), 10), true
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
}
