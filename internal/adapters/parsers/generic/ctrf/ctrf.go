package ctrf

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// suitePathSeparator joins a CTRF suite path into a class name. The exact array
// is preserved in a case property, because a segment may itself contain this
// separator and joining is therefore not reversible.
const suitePathSeparator = " > "

// Property keys for CTRF fields with no first-class home in the CLI's wire
// model. Nothing in a report is silently discarded, even when it cannot be
// modelled.
const (
	propTool        = "ctrfTool"
	propToolVersion = "ctrfToolVersion"
	propRunID       = "ctrfRunId"
	propReportID    = "ctrfReportId"
	propSpecVersion = "ctrfSpecVersion"
	propGeneratedBy = "ctrfGeneratedBy"
	propStatus      = "ctrfStatus"
	propRawStatus   = "ctrfRawStatus"
	propSnippet     = "ctrfSnippet"
	propExecutionID = "ctrfExecutionId"
	propStop        = "ctrfStop"
	propSuitePath   = "ctrfSuitePath"
	propParamPrefix = "ctrfParam."
	propOSPlatform  = "ctrfOsPlatform"
	propOSRelease   = "ctrfOsRelease"
	propOSVersion   = "ctrfOsVersion"
	propTestEnv     = "ctrfTestEnvironment"
	propBranch      = "ctrfBranchName"
	propCommit      = "ctrfCommit"
	propBuildNumber = "ctrfBuildNumber"
	propBuildURL    = "ctrfBuildUrl"
	propShardID     = "ctrfShardId"
)

// Structural property keys the collect pipeline already allowlists, so they
// survive --no-capture-output the same way every other parser's do.
const (
	propFile      = "file"
	propLine      = "line"
	propType      = "type"
	propBrowser   = "browser"
	propThreadID  = "threadId"
	propDevice    = "device"
	propRetryCnt  = "retryCount"
	propSystemOut = "system-out"
	propSystemErr = "system-err"
)

// Caps mirroring api-service's launch.MaxCaseAttempts and the MaxAttempt*Runes
// group. The server truncates on write regardless, so these exist to keep a
// pathological retry history from eating the /collect body limit on the way
// there, not to enforce correctness.
const (
	maxCaseAttempts        = 50
	maxAttemptMessageRunes = 8192
	maxAttemptTraceRunes   = 32768
	maxAttemptSnippetRunes = 4096
	maxAttemptOutputRunes  = 16384
	maxAttemptOutputLines  = 200
	maxAttemptUIDRunes     = 255
)

// Parser reads Common Test Report Format documents.
type Parser struct{}

// New creates a new CTRF parser.
func New() *Parser { return &Parser{} }

// GetFramework returns the framework identifier.
func (p *Parser) GetFramework() domain.Framework { return domain.FrameworkCTRF }

// SupportedFileExtensions returns the file extensions this parser handles.
func (p *Parser) SupportedFileExtensions() []string { return []string{".json"} }

// Parse reads a CTRF document and returns one Suite.
//
// One suite per file is what ports.Parser allows, so a document's own suite
// paths become each case's ClassName rather than separate suites — the same
// shape the native and Playwright parsers already produce.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read CTRF report: %w", err)
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("failed to parse CTRF report: %w", err)
	}

	// A document naming a DIFFERENT format is refused; one naming none is
	// accepted, because that is the legacy shape most reporters still emit.
	if report.ReportFormat != "" && !strings.EqualFold(strings.TrimSpace(report.ReportFormat), "CTRF") {
		return nil, fmt.Errorf("not a CTRF report: reportFormat = %q", report.ReportFormat)
	}
	if report.Results == nil {
		return nil, errors.New("not a CTRF report: no results object")
	}

	return buildSuite(&report), nil
}

func buildSuite(report *Report) *domain.Suite {
	results := report.Results
	env := results.Environment
	if env == nil {
		env = &Environment{}
	}
	toolName, toolVersion := "", ""
	if results.Tool != nil {
		toolName = strings.TrimSpace(results.Tool.Name)
		toolVersion = strings.TrimSpace(results.Tool.Version)
	}

	suite := &domain.Suite{
		Name: base.CoalesceString(
			strings.TrimSpace(env.ReportName),
			suiteNameFromTool(toolName),
			"CTRF Test Results",
		),
		// The PRODUCING tool's category, not a synthetic "ctrf" one. A report
		// from playwright-ctrf-json-reporter then shows up as a Playwright
		// suite, indistinguishable from a native Playwright upload.
		Category:   categoryForTool(toolName),
		Properties: map[string]string{},
	}

	setProp(suite.Properties, propTool, toolName)
	// The producing framework, when the tool name resolves to one. This is what
	// Launch.Framework should carry — "ctrf" is how the report travelled, not
	// what ran the tests. Unresolvable tools set nothing, so the caller can tell
	// "produced by something we do not model" from "produced by cypress".
	if f, ok := frameworkForTool(toolName); ok {
		setProp(suite.Properties, domain.PropSourceFramework, string(f))
	}
	setProp(suite.Properties, propToolVersion, toolVersion)
	setProp(suite.Properties, propRunID, report.RunID)
	setProp(suite.Properties, propReportID, report.ReportID)
	setProp(suite.Properties, propSpecVersion, report.SpecVersion)
	setProp(suite.Properties, propGeneratedBy, report.GeneratedBy)
	setProp(suite.Properties, propOSPlatform, env.OSPlatform)
	setProp(suite.Properties, propOSRelease, env.OSRelease)
	setProp(suite.Properties, propOSVersion, env.OSVersion)
	setProp(suite.Properties, propTestEnv, env.TestEnvironment)
	setProp(suite.Properties, propBranch, env.BranchName)
	setProp(suite.Properties, propCommit, env.Commit)
	setProp(suite.Properties, propBuildNumber, env.BuildNumber.String())
	setProp(suite.Properties, propBuildURL, env.BuildURL)
	setProp(suite.Properties, propShardID, env.ShardID)

	suite.Cases = make([]domain.Case, 0, len(results.Tests))
	for i := range results.Tests {
		suite.Cases = append(suite.Cases, convertTest(&results.Tests[i], env))
	}

	applyTiming(suite, results)
	applyBrowser(suite, results.Tests)

	// Counts come from the CASES, never from results.summary. A summary that
	// disagrees with tests[] would otherwise be able to roll a red run green,
	// and a client-supplied total is exactly the kind of value that drifts.
	suite.RecomputeCounts()

	for _, c := range suite.Cases {
		if c.IsFlaky != nil && *c.IsFlaky {
			suite.Flaky++
		}
		if c.RetryCount != nil {
			suite.Retries += *c.RetryCount
		}
	}
	return suite
}

func suiteNameFromTool(toolName string) string {
	if toolName == "" {
		return ""
	}
	return toolName + " (CTRF)"
}

func applyTiming(suite *domain.Suite, results *Results) {
	if s := results.Summary; s != nil {
		if s.Start.IsSet() {
			suite.Timestamp = time.UnixMilli(s.Start.Int64()).UTC()
		}
		switch {
		case s.Duration.IsSet():
			suite.Duration = time.Duration(s.Duration.Int64()) * time.Millisecond
		case s.Start.IsSet() && s.Stop.IsSet():
			suite.Duration = time.Duration(s.Stop.Int64()-s.Start.Int64()) * time.Millisecond
		}
	}
	if suite.Timestamp.IsZero() {
		suite.Timestamp = time.Now().UTC()
	}
	if suite.Duration <= 0 {
		for _, c := range suite.Cases {
			suite.Duration += c.Duration
		}
	}
}

// applyBrowser promotes a browser to the suite only when every test that names
// one agrees, and writes it sorted so the value is deterministic — the collect
// pipeline promotes matching suite properties to the launch by string equality,
// which unsorted map iteration would break intermittently.
func applyBrowser(suite *domain.Suite, tests []Test) {
	seen := map[string]struct{}{}
	for i := range tests {
		if b := strings.TrimSpace(tests[i].Browser); b != "" {
			seen[b] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return
	}
	names := make([]string, 0, len(seen))
	for b := range seen {
		names = append(names, b)
	}
	sort.Strings(names)
	suite.Properties[propBrowser] = strings.Join(names, ", ")
}

func convertTest(t *Test, env *Environment) domain.Case {
	segments := nonEmpty(t.Suite)
	suitePath := strings.Join(segments, suitePathSeparator)

	status := mapStatus(t.Status, t.RawStatus)
	c := domain.Case{
		ID:         caseIdentity(t, suitePath),
		Name:       strings.TrimSpace(t.Name),
		ClassName:  base.CoalesceString(suitePath, strings.TrimSpace(t.FilePath)),
		Status:     status,
		Duration:   testDuration(t),
		Error:      mergeError(t.Message, t.Trace),
		Tags:       t.Tags,
		Labels:     labelsToDomain(t.Labels),
		Steps:      stepsToDomain(t.Steps),
		Properties: map[string]string{},
	}

	if t.Start.IsSet() {
		started := time.UnixMilli(t.Start.Int64()).UTC()
		c.StartedAt = &started
	}
	// A CTRF shard is report-wide, so it applies to every case in the file.
	// ParseShardIndex rejects negative and out-of-range values, leaving nil
	// ("not reported") rather than claiming worker 0.
	if idx, ok := base.ParseShardIndex(env.ShardID); ok {
		c.ShardIndex = &idx
	}

	applyCaseProperties(&c, t, status, segments)
	applyRetries(&c, t, status)
	c.Attachments = attachmentsToDomain(t)

	return c
}

// caseIdentity resolves the stable identity flaky history keys on.
//
// testId is preferred because the spec defines it as stable across runs, so a
// renamed test keeps its history. The synthesized fallback is derived from the
// suite path and name, making it stable across runs but not across renames —
// i.e. it degrades exactly to the name-based matching that predates test ids.
// The `ctrf:` prefix keeps a synthesized identity distinguishable from a real
// one.
func caseIdentity(t *Test, suitePath string) string {
	if id := strings.TrimSpace(t.TestID); id != "" {
		return id
	}
	if id := strings.TrimSpace(t.ID); id != "" {
		return id
	}
	sum := sha256.Sum256([]byte(suitePath + "\x00" + strings.TrimSpace(t.Name)))
	return "ctrf:" + hex.EncodeToString(sum[:])[:32]
}

// testDuration converts CTRF's integer milliseconds to a Go duration, falling
// back to stop-start. Sub-millisecond precision is unavailable because CTRF has
// none; that loss is in the format, not here.
func testDuration(t *Test) time.Duration {
	if t.Duration.IsSet() {
		if ms := t.Duration.Int64(); ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
		return 0
	}
	if t.Start.IsSet() && t.Stop.IsSet() {
		if ms := t.Stop.Int64() - t.Start.Int64(); ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return 0
}

// mergeError folds CTRF's separate message and trace into the single Error field
// the wire model has. Lossy, which is why the snippet is kept separately as a
// property rather than being concatenated in too.
func mergeError(message, trace string) string {
	message, trace = strings.TrimSpace(message), strings.TrimSpace(trace)
	switch {
	case trace == "":
		return message
	case message == "":
		return trace
	case strings.HasPrefix(trace, message):
		// Several reporters put the message at the head of the trace; joining
		// them would print it twice.
		return trace
	default:
		return message + "\n\n" + trace
	}
}

func applyCaseProperties(c *domain.Case, t *Test, status domain.Status, segments []string) {
	p := c.Properties

	// Both are recorded so the mapping stays reversible: the five-value CTRF
	// status and the producer's own pre-normalization value.
	if s := strings.TrimSpace(t.Status); s != "" && s != string(status) {
		setProp(p, propStatus, s)
	}
	setProp(p, propRawStatus, t.RawStatus)
	setProp(p, propSnippet, t.Snippet)
	setProp(p, propExecutionID, t.ExecutionID)
	setProp(p, propFile, t.FilePath)
	setProp(p, propType, t.Type)
	setProp(p, propBrowser, t.Browser)
	setProp(p, propThreadID, t.ThreadID)
	setProp(p, propDevice, t.Device)

	if t.Line.IsSet() {
		setProp(p, propLine, t.Line.String())
	}
	if t.Stop.IsSet() {
		setProp(p, propStop, t.Stop.String())
	}
	if len(segments) > 1 {
		// The exact array, because joining on " > " is not reversible when a
		// segment itself contains the separator.
		if raw, err := json.Marshal(segments); err == nil {
			setProp(p, propSuitePath, string(raw))
		}
	}
	// Joined the same way the JUnit parser writes captured output, so
	// --no-capture-output strips them with no extra code.
	if len(t.Stdout) > 0 {
		setProp(p, propSystemOut, strings.Join(t.Stdout, "\n"))
	}
	if len(t.Stderr) > 0 {
		setProp(p, propSystemErr, strings.Join(t.Stderr, "\n"))
	}
	// CTRF puts parameters on the TEST; the wire model puts them on the STEP,
	// so they have no first-class home and land as individual properties.
	for name, raw := range t.Parameters {
		if s, ok := scalarToString(raw); ok {
			setProp(p, propParamPrefix+name, s)
		}
	}
}

// applyRetries maps CTRF's retry model onto RetryCount, IsFlaky and Attempts.
//
// len(retryAttempts) is the observed truth when it disagrees with `retries`:
// deployed reporters pin an older ctrf release than the spec, so the two can
// legitimately differ.
func applyRetries(c *domain.Case, t *Test, status domain.Status) {
	count := len(t.RetryAttempts)
	if count == 0 && t.Retries.IsSet() {
		count = int(t.Retries.Int64())
	}
	if count < 0 {
		count = 0
	}
	if count > 0 || t.Retries.IsSet() {
		c.RetryCount = &count
		setProp(c.Properties, propRetryCnt, strconv.Itoa(count))
	}

	switch {
	case t.Flaky != nil:
		c.IsFlaky = t.Flaky
	case count > 0:
		// CTRF's own definition, which is normative and narrow: flaky only when
		// the FINAL status is passed. "Failed after retries" is not flaky.
		flaky := status == domain.StatusPassed
		c.IsFlaky = &flaky
	}

	c.Attempts = attemptsToDomain(t, status)
}

// attemptsToDomain builds the per-attempt history the server persists, which is
// NOT the shape CTRF reports.
//
// CTRF's retryAttempts holds only the executions before the last one, because the
// test object itself IS the final attempt. The wire model wants one uniform
// 1-based ascending list with the final attempt as its last element, so the test
// is appended here -- api-service's launch.Case.Attempts names this mapper as the
// place that off-by-one is resolved.
//
// Returns nil below two attempts: the server persists nothing for a lone attempt,
// since it carries no status or duration the case run does not already hold.
func attemptsToDomain(t *Test, status domain.Status) []domain.Attempt {
	if len(t.RetryAttempts) == 0 {
		return nil
	}

	out := make([]domain.Attempt, 0, len(t.RetryAttempts)+1)
	for i, a := range t.RetryAttempts {
		number := i + 1
		if a.Attempt.IsSet() && a.Attempt.Int64() > 0 {
			number = int(a.Attempt.Int64())
		}
		att := domain.Attempt{
			Number:   number,
			Status:   mapStatus(a.Status, a.RawStatus),
			Duration: attemptDuration(a.Duration),
			UID:      base.TruncateString(a.AttemptID, maxAttemptUIDRunes),
			Message:  base.TruncateString(a.Message, maxAttemptMessageRunes),
			Trace:    base.TruncateString(a.Trace, maxAttemptTraceRunes),
			Snippet:  base.TruncateString(a.Snippet, maxAttemptSnippetRunes),
			Line:     a.Line,
			Stdout:   clampOutput(a.Stdout),
			Stderr:   clampOutput(a.Stderr),
		}
		if a.Start.IsSet() {
			started := time.UnixMilli(a.Start.Int64()).UTC()
			att.StartedAt = &started
		}
		out = append(out, att)
	}

	// Append the test as the final attempt -- unless the producer already did.
	// `retries` counts RETRIES, so a spec-compliant report has exactly `retries`
	// entries here; one more than that means the final attempt is already present
	// and appending would duplicate it.
	alreadyFinal := t.Retries.IsSet() && len(t.RetryAttempts) == int(t.Retries.Int64())+1
	if !alreadyFinal {
		final := domain.Attempt{
			Number:   out[len(out)-1].Number + 1,
			Status:   status,
			Duration: testDuration(t),
			Message:  base.TruncateString(t.Message, maxAttemptMessageRunes),
			Trace:    base.TruncateString(t.Trace, maxAttemptTraceRunes),
			Snippet:  base.TruncateString(t.Snippet, maxAttemptSnippetRunes),
			Stdout:   clampOutput(t.Stdout),
			Stderr:   clampOutput(t.Stderr),
		}
		if t.Start.IsSet() {
			started := time.UnixMilli(t.Start.Int64()).UTC()
			final.StartedAt = &started
		}
		out = append(out, final)
	}

	if len(out) < 2 {
		return nil
	}
	return clampAttempts(out)
}

// attemptDuration converts CTRF's integer milliseconds, like testDuration. A
// negative or unset value becomes zero rather than a nonsense duration.
func attemptDuration(n Number) time.Duration {
	if !n.IsSet() {
		return 0
	}
	if ms := n.Int64(); ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 0
}

// clampAttempts bounds the list to what one case run persists, keeping the FINAL
// attempt: it carries the outcome, so a plain truncation would discard the only
// element that explains the case's own status.
func clampAttempts(in []domain.Attempt) []domain.Attempt {
	if len(in) <= maxCaseAttempts {
		return in
	}
	out := make([]domain.Attempt, 0, maxCaseAttempts)
	out = append(out, in[:maxCaseAttempts-1]...)
	return append(out, in[len(in)-1])
}

// clampOutput bounds captured output by lines first, then by runes, matching how
// the server truncates on write.
func clampOutput(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > maxAttemptOutputLines {
		lines = lines[:maxAttemptOutputLines]
	}
	out := make([]string, 0, len(lines))
	total := 0
	for _, l := range lines {
		if total >= maxAttemptOutputRunes {
			break
		}
		l = base.TruncateString(l, maxAttemptOutputRunes-total)
		total += len([]rune(l))
		out = append(out, l)
	}
	return out
}

// labelsToDomain flattens CTRF's labels object into name/value pairs. An ARRAY
// value becomes one pair per element, which is exactly why Case.Labels is a
// slice and not a map.
func labelsToDomain(in map[string]json.RawMessage) []domain.Label {
	if len(in) == 0 {
		return nil
	}
	names := make([]string, 0, len(in))
	for k := range in {
		names = append(names, k)
	}
	sort.Strings(names)

	out := make([]domain.Label, 0, len(in))
	for _, name := range names {
		raw := in[name]
		if s, ok := scalarToString(raw); ok {
			out = append(out, domain.Label{Name: name, Value: s})
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			continue
		}
		for _, item := range items {
			if s, ok := scalarToString(item); ok {
				out = append(out, domain.Label{Name: name, Value: s})
			}
		}
	}
	return out
}

// stepsToDomain maps CTRF steps, which carry only a name and a status. Duration,
// keyword, location, parameters and nesting are all absent from the format, so
// imported steps are flat and untimed — the largest single fidelity loss here.
func stepsToDomain(in []Step) []domain.Step {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.Step, 0, len(in))
	for _, s := range in {
		out = append(out, domain.Step{
			Name:   strings.TrimSpace(s.Name),
			Status: mapStatus(s.Status, ""),
		})
	}
	return out
}

// attachmentsToDomain maps CTRF attachments and the inline screenshot.
//
// CTRF attachments carry NO bytes: `path` is opaque and may be a URL or a
// filesystem path. It is passed through untouched and never read — the same
// contract domain.Attachment.Path already documents. The single exception is
// `screenshot`, which is genuine inline content.
func attachmentsToDomain(t *Test) []domain.Attachment {
	out := make([]domain.Attachment, 0, len(t.Attachments)+1)

	if raw := strings.TrimSpace(t.Screenshot); raw != "" {
		if looksLikeReference(raw) {
			// The spec says a screenshot MUST NOT be a URL or a path. Producers
			// violate it, so a reference is demoted rather than stored as
			// undecodable bytes.
			out = append(out, domain.Attachment{Name: "screenshot", Path: raw})
		} else {
			out = append(out, domain.Attachment{
				Name:     "screenshot",
				MimeType: sniffImageMime(raw),
				Content:  raw,
			})
		}
	}
	for _, a := range t.Attachments {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			// Name is required server-side; an attachment we cannot name is
			// dropped rather than failing the upload.
			continue
		}
		out = append(out, domain.Attachment{
			Name:     name,
			Path:     strings.TrimSpace(a.Path),
			MimeType: strings.TrimSpace(a.ContentType),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// looksLikeReference reports whether a supposedly-base64 value is really a URL
// or a filesystem path. Base64 alphabets contain none of these markers.
func looksLikeReference(v string) bool {
	if strings.Contains(v, "://") {
		return true
	}
	if strings.HasPrefix(v, "/") || strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../") {
		return true
	}
	return len(v) > 2 && v[1] == ':' && (v[2] == '\\' || v[2] == '/')
}

func sniffImageMime(b64 string) string {
	switch {
	case strings.HasPrefix(b64, "iVBORw0KGgo"):
		return "image/png"
	case strings.HasPrefix(b64, "/9j/"):
		return "image/jpeg"
	case strings.HasPrefix(b64, "R0lGOD"):
		return "image/gif"
	case strings.HasPrefix(b64, "UklGR"):
		return "image/webp"
	default:
		return ""
	}
}

func nonEmpty(in Strings) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func setProp(p map[string]string, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		p[key] = v
	}
}
