// Package allure parses Allure 2 results (an allure-results/ directory).
//
// Allure is a FORMAT, not a test framework: every framework's Allure adapter
// (allure-pytest, allure-junit5, allure-playwright, allure-cucumber-jvm, …)
// writes the same files into one directory —
//
//	<uuid>-result.json      one per test EXECUTION (a retry is a second file)
//	<uuid>-container.json   fixtures (befores/afters) and grouping
//	<uuid>-attachment.<ext> files referenced by results and steps
//	executor.json, categories.json, environment.properties
//
// so this parser takes the whole directory (ports.PathAwareParser) rather than
// one file at a time: retries can only be recognised across files.
//
// Out of scope for now: containers. A failing fixture already makes Allure
// mark the affected test "broken", which maps to StatusError, so statuses stay
// right; only the fixture's own steps are not shown.
package allure

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"qualflare-cli/internal/core/domain"
)

const (
	resultSuffix  = "-result.json"
	suiteSep      = " > "
	defaultSuite  = "Allure results"
	maskedValue   = "[masked]"
	maxInlineSize = 1 << 20 // non-media attachments up to 1 MiB travel inline

	propParamPrefix = "allureParam."
	propFullName    = "allureFullName"
)

// result is the subset of an Allure 2 *-result.json this parser reads.
type result struct {
	UUID          string        `json:"uuid"`
	HistoryID     string        `json:"historyId"`
	TestCaseID    string        `json:"testCaseId"`
	FullName      string        `json:"fullName"`
	Name          string        `json:"name"`
	Status        string        `json:"status"`
	StatusDetails statusDetails `json:"statusDetails"`
	Start         int64         `json:"start"`
	Stop          int64         `json:"stop"`
	Labels        []label       `json:"labels"`
	Links         []link        `json:"links"`
	Parameters    []parameter   `json:"parameters"`
	Steps         []step        `json:"steps"`
	Attachments   []attachment  `json:"attachments"`
}

type statusDetails struct {
	Message string `json:"message"`
	Trace   string `json:"trace"`
	Flaky   bool   `json:"flaky"`
}

type label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type link struct {
	Type string `json:"type"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type parameter struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Excluded bool   `json:"excluded"`
	Mode     string `json:"mode"` // "", "default", "masked", "hidden"
}

type step struct {
	Name          string        `json:"name"`
	Status        string        `json:"status"`
	StatusDetails statusDetails `json:"statusDetails"`
	Start         int64         `json:"start"`
	Stop          int64         `json:"stop"`
	Parameters    []parameter   `json:"parameters"`
	Steps         []step        `json:"steps"`
	Attachments   []attachment  `json:"attachments"`
}

type attachment struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Type   string `json:"type"`
}

// Parser implements ports.PathAwareParser for Allure results.
type Parser struct{}

// New creates an Allure results parser.
func New() *Parser { return &Parser{} }

// GetFramework returns the framework identifier.
func (p *Parser) GetFramework() domain.Framework { return domain.FrameworkAllure }

// SupportedFileExtensions returns the file extensions this parser handles.
func (p *Parser) SupportedFileExtensions() []string { return []string{".json"} }

// IsResultsDir reports whether path is a directory holding Allure results.
func IsResultsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	matches, _ := filepath.Glob(filepath.Join(path, "*"+resultSuffix))
	return len(matches) > 0
}

// IsResultDocument reports whether a decoded JSON object looks like one Allure
// result: the fields no other supported format carries together.
func IsResultDocument(obj map[string]interface{}) bool {
	_, uuid := obj["uuid"]
	_, history := obj["historyId"]
	_, status := obj["status"]
	_, start := obj["start"]
	_, stop := obj["stop"]
	return uuid && history && status && start && stop
}

// Parse reads a single *-result.json. Attachments are not resolved: a bare
// reader has no directory to resolve them against. Pass the directory instead.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var r result
	if err := json.NewDecoder(reader).Decode(&r); err != nil {
		return nil, fmt.Errorf("allure: invalid result JSON: %w", err)
	}
	if r.Status == "" {
		return nil, errors.New("allure: document has no status; not an Allure result")
	}
	return buildSuite([]result{r}, ""), nil
}

// ParsePath parses an allure-results directory, or one result file.
func (p *Parser) ParsePath(path string) (*domain.Suite, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return p.Parse(f)
	}

	files, err := filepath.Glob(filepath.Join(path, "*"+resultSuffix))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("allure: %s contains no *%s files", path, resultSuffix)
	}
	sort.Strings(files)

	results := make([]result, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("allure: %w", err)
		}
		var r result
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("allure: %s: %w", filepath.Base(f), err)
		}
		results = append(results, r)
	}

	dir, err := filepath.Abs(path)
	if err != nil {
		dir = path
	}
	return buildSuite(results, dir), nil
}

// buildSuite turns results into one Suite. dir resolves attachments; "" skips them.
func buildSuite(results []result, dir string) *domain.Suite {
	groups, order := groupByTest(results)

	suite := &domain.Suite{Properties: map[string]string{}}
	var first, last int64
	producers := map[domain.Framework]struct{}{}
	topSuites := map[string]struct{}{}

	for _, key := range order {
		attempts := groups[key]
		final := attempts[len(attempts)-1]
		c := buildCase(final, dir)

		if len(attempts) > 1 {
			retries := len(attempts) - 1
			c.RetryCount = &retries
			suite.Retries += retries
			for i, a := range attempts {
				started := time.UnixMilli(a.Start)
				c.Attempts = append(c.Attempts, domain.Attempt{
					Number:    i + 1,
					Status:    mapStatus(a.Status),
					Duration:  duration(a.Start, a.Stop),
					StartedAt: &started,
					UID:       a.UUID,
					Message:   a.StatusDetails.Message,
					Trace:     a.StatusDetails.Trace,
				})
			}
		}
		if isFlaky(attempts) {
			flaky := true
			c.IsFlaky = &flaky
			suite.Flaky++
		}
		suite.Cases = append(suite.Cases, c)

		for _, a := range attempts {
			if first == 0 || (a.Start > 0 && a.Start < first) {
				first = a.Start
			}
			if a.Stop > last {
				last = a.Stop
			}
		}
		if f, ok := frameworkForLabel(labelValue(final.Labels, "framework")); ok {
			producers[f] = struct{}{}
		}
		if top := firstNonEmpty(labelValue(final.Labels, "parentSuite"), labelValue(final.Labels, "suite")); top != "" {
			topSuites[top] = struct{}{}
		}
	}

	suite.Name = defaultSuite
	if len(topSuites) == 1 {
		for name := range topSuites {
			suite.Name = name
		}
	}
	suite.Category = domain.CategoryGeneric
	if len(producers) == 1 {
		for f := range producers {
			suite.Properties[domain.PropSourceFramework] = string(f)
			suite.Category = f.GetCategory()
		}
	}
	if first > 0 {
		suite.Timestamp = time.UnixMilli(first)
		suite.Duration = duration(first, last)
	}
	suite.RecomputeCounts()
	return suite
}

// groupByTest groups executions of the same test (historyId, falling back to
// testCaseId, fullName, then uuid), each group ordered oldest first, and returns
// the group keys in first-seen order so the output is stable.
func groupByTest(results []result) (map[string][]result, []string) {
	groups := map[string][]result{}
	var order []string
	for _, r := range results {
		key := firstNonEmpty(r.HistoryID, r.TestCaseID, r.FullName, r.UUID)
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], r)
	}
	for _, key := range order {
		g := groups[key]
		sort.SliceStable(g, func(i, j int) bool {
			if g[i].Stop != g[j].Stop {
				return g[i].Stop < g[j].Stop
			}
			return g[i].Start < g[j].Start
		})
	}
	return groups, order
}

// isFlaky: the final attempt passed after an earlier one did not, or the
// adapter flagged the result flaky itself.
func isFlaky(attempts []result) bool {
	final := attempts[len(attempts)-1]
	if final.StatusDetails.Flaky {
		return true
	}
	if mapStatus(final.Status) != domain.StatusPassed {
		return false
	}
	for _, a := range attempts[:len(attempts)-1] {
		if mapStatus(a.Status) != domain.StatusPassed {
			return true
		}
	}
	return false
}

func buildCase(r result, dir string) domain.Case {
	started := time.UnixMilli(r.Start)
	c := domain.Case{
		ID:         firstNonEmpty(r.TestCaseID, r.HistoryID, r.UUID),
		Name:       firstNonEmpty(r.Name, r.FullName, r.UUID),
		ClassName:  suitePath(r),
		Status:     mapStatus(r.Status),
		Duration:   duration(r.Start, r.Stop),
		Error:      errorText(r.StatusDetails),
		Priority:   mapSeverity(labelValue(r.Labels, "severity")),
		Properties: map[string]string{},
	}
	if r.Start > 0 {
		c.StartedAt = &started
	}
	if r.FullName != "" {
		c.Properties[propFullName] = r.FullName
	}
	for _, l := range r.Labels {
		c.Labels = append(c.Labels, domain.Label{Name: l.Name, Value: l.Value})
		if l.Name == "tag" && l.Value != "" {
			c.Tags = append(c.Tags, l.Value)
		}
	}
	for _, l := range r.Links {
		if l.URL != "" {
			c.Links = append(c.Links, domain.Link{Type: l.Type, Name: l.Name, URL: l.URL})
		}
	}
	for _, p := range visibleParams(r.Parameters) {
		v := p.Value
		if p.Masked {
			v = maskedValue
		}
		c.Properties[propParamPrefix+p.Name] = v
	}
	c.Steps = flattenSteps(r.Steps, nil, nil)
	c.Attachments = resolveAttachments(collectAttachments(r), dir)
	return c
}

// suitePath is the Allure suite hierarchy, which Allure Report shows as nested
// groups. It goes in ClassName because a PathAwareParser returns one Suite.
func suitePath(r result) string {
	var parts []string
	for _, name := range []string{"parentSuite", "suite", "subSuite"} {
		if v := labelValue(r.Labels, name); v != "" {
			parts = append(parts, v)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, suiteSep)
	}
	return labelValue(r.Labels, "testClass")
}

func flattenSteps(steps []step, parent *int, out []domain.Step) []domain.Step {
	for _, s := range steps {
		idx := len(out)
		var p *int
		if parent != nil {
			v := *parent
			p = &v
		}
		out = append(out, domain.Step{
			Name:        s.Name,
			Status:      mapStatus(s.Status),
			Duration:    duration(s.Start, s.Stop),
			Error:       s.StatusDetails.Message,
			ParentIndex: p,
			Parameters:  visibleParams(s.Parameters),
		})
		out = flattenSteps(s.Steps, &idx, out)
	}
	return out
}

// visibleParams applies Allure's parameter modes: hidden parameters are dropped,
// masked ones keep their name but not their value. "excluded" only affects
// Allure's historyId calculation, so the parameter is still shown.
func visibleParams(params []parameter) []domain.Parameter {
	var out []domain.Parameter
	for _, p := range params {
		switch p.Mode {
		case "hidden":
			continue
		case "masked":
			out = append(out, domain.Parameter{Name: p.Name, Masked: true})
		default:
			out = append(out, domain.Parameter{Name: p.Name, Value: p.Value})
		}
	}
	return out
}

func collectAttachments(r result) []attachment {
	out := append([]attachment(nil), r.Attachments...)
	var walk func([]step)
	walk = func(steps []step) {
		for _, s := range steps {
			out = append(out, s.Attachments...)
			walk(s.Steps)
		}
	}
	walk(r.Steps)
	return out
}

// resolveAttachments maps Allure attachments to files in dir. Images, videos and
// Playwright traces go through the artifact upload pass (LocalPath +
// ArtifactKind, gated by --upload-artifacts); other small files travel inline.
// A missing or unsafe source is dropped rather than sent as a dangling reference.
func resolveAttachments(atts []attachment, dir string) []domain.Attachment {
	if dir == "" {
		return nil
	}
	var out []domain.Attachment
	for _, a := range atts {
		if a.Source == "" || a.Source != filepath.Base(a.Source) {
			continue // a source must be a plain file name inside the results dir
		}
		full := filepath.Join(dir, a.Source)
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			continue
		}
		att := domain.Attachment{Name: firstNonEmpty(a.Name, a.Source), MimeType: a.Type}
		if kind := artifactKind(a.Type, a.Source); kind != "" {
			att.LocalPath, att.ArtifactKind = full, kind
		} else {
			if info.Size() > maxInlineSize {
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			att.Content = base64.StdEncoding.EncodeToString(data)
		}
		out = append(out, att)
	}
	return out
}

func artifactKind(mime, source string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return domain.ArtifactKindImage
	case strings.HasPrefix(mime, "video/"):
		return domain.ArtifactKindVideo
	case mime == "application/zip" && strings.Contains(strings.ToLower(source), "trace"):
		return domain.ArtifactKindTrace
	}
	return ""
}

// mapStatus maps Allure's statuses. "broken" means the test could not run
// properly (an exception outside an assertion): an error, not a failure.
func mapStatus(s string) domain.Status {
	switch strings.ToLower(s) {
	case "passed":
		return domain.StatusPassed
	case "failed":
		return domain.StatusFailed
	case "skipped":
		return domain.StatusSkipped
	default: // broken, unknown, anything new
		return domain.StatusError
	}
}

// mapSeverity maps Allure's five severities onto Qualflare's.
func mapSeverity(s string) domain.Severity {
	switch strings.ToLower(s) {
	case "blocker":
		return domain.SeverityCritical
	case "critical":
		return domain.SeverityCritical
	case "normal":
		return domain.SeverityMedium
	case "minor":
		return domain.SeverityLow
	case "trivial":
		return domain.SeverityInfo
	}
	return ""
}

// frameworkForLabel maps an Allure "framework" label onto the framework that
// produced the results, reporting whether it could. Like the CTRF parser it
// returns false rather than guessing, so the suite falls back to generic.
func frameworkForLabel(v string) (domain.Framework, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "pytest", "pytest-bdd":
		return domain.FrameworkPython, true
	case "junit4", "junit5", "junit-platform", "junit":
		return domain.FrameworkJUnit, true
	case "testng":
		return domain.FrameworkTestNG, true
	case "playwright":
		return domain.FrameworkPlaywright, true
	case "cypress":
		return domain.FrameworkCypress, true
	case "jest":
		return domain.FrameworkJest, true
	case "vitest":
		return domain.FrameworkVitest, true
	case "mocha":
		return domain.FrameworkMocha, true
	case "cucumberjvm", "cucumber-jvm", "cucumberjs", "cucumber-js", "cucumber":
		return domain.FrameworkCucumber, true
	case "webdriverio", "wdio":
		return domain.FrameworkWebdriverIO, true
	case "phpunit":
		return domain.FrameworkPHPUnit, true
	case "rspec":
		return domain.FrameworkRSpec, true
	case "karate":
		return domain.FrameworkKarate, true
	}
	return "", false
}

func errorText(d statusDetails) string {
	msg, trace := strings.TrimSpace(d.Message), strings.TrimSpace(d.Trace)
	switch {
	case msg != "" && trace != "":
		return msg + "\n" + trace
	case msg != "":
		return msg
	}
	return trace
}

func labelValue(labels []label, name string) string {
	for _, l := range labels {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

func duration(start, stop int64) time.Duration {
	if stop <= start {
		return 0
	}
	return time.Duration(stop-start) * time.Millisecond
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
