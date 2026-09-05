// Package qualflare parses the reporters' own Collect JSON output directly —
// the exact shape @qualflare/cypress and @qualflare/cucumberjs already build
// and (in normal mode) POST to /api/v1/collect, written to a file instead
// via each reporter's `outputFile` config option.
//
// This exists for the sharded-CI merge workflow: each shard writes its own
// file, and `qualflare-cli upload --shard <files...>` merges them into one
// launch, reusing the shard-merge machinery already built for every other
// framework (report_service.go's tagShardsByFile) — no backend changes
// needed. See both reporters' docs/LIMITATIONS.md for the full recipe.
//
// The richest-fidelity ingestion of any parser in this factory: almost
// every field maps straight across, since the source IS the wire contract,
// not a third-party report format that needs translating.
package qualflare

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses one reporter's Collect JSON file into a domain.Suite.
type Parser struct{}

// New creates a new qualflare-json parser.
func New() *Parser {
	return &Parser{}
}

// Collect, Suite, Case, Step, and Attachment below mirror the reporters'
// own wire-contract types (shared/types.ts in both @qualflare/cypress and
// @qualflare/cucumberjs) closely enough to decode their JSON output —
// only the fields this parser actually maps onto domain.* are declared.
type Collect struct {
	Framework string `json:"framework"`
	// Platform and Browser are launch-level in the reporters' wire contract
	// (shared/types.ts): both @qualflare/cypress's collect-builder.ts and
	// @qualflare/cucumberjs's equivalent set them ONLY on the top-level Collect
	// object (from `config.platform`/`resolveBrowser(config, browserInfo)`) —
	// neither reporter ever populates a per-suite `browser` override, so this
	// is the one place in the JSON that actually carries them. Captured here
	// (stopgap for the data-loss described in buildSuite) and copied onto the
	// synthetic wrapper Suite's Properties in buildSuite, so
	// report_service.go's existing promoteConsistentSuiteProperties picks
	// "browser" (and, if ever consistent across merged shards, "platform")
	// back up into Launch.Properties automatically.
	//
	// Environment rides the same channel, for a sharper reason: it is not
	// cosmetic. Left undecoded, every reporter's `environment` option was
	// dropped on the floor and the launch silently went to the CLI's own
	// default ("development") — a wrong destination rather than a missing
	// label, and one nothing reported. It yields to --environment and
	// QF_ENVIRONMENT; see Config.SetEnvironmentFallback.
	//
	// The remaining Collect fields (branch, commit, language, milestone, CI
	// metadata, os) are deliberately still NOT decoded here — recovering those
	// needs a ports.Parser interface change to carry launch-level fields, out
	// of scope for this stopgap.
	Platform    string  `json:"platform,omitempty"`
	Browser     string  `json:"browser,omitempty"`
	Environment string  `json:"environment,omitempty"`
	Suites      []Suite `json:"suites"`
}

type Suite struct {
	Name     string `json:"name"`
	Duration int64  `json:"duration"` // nanoseconds
	Cases    []Case `json:"cases"`
}

type Case struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ClassName   string            `json:"className,omitempty"`
	Status      string            `json:"status"`
	Duration    int64             `json:"duration"` // nanoseconds
	RetryCount  *int              `json:"retryCount,omitempty"`
	IsFlaky     *bool             `json:"isFlaky,omitempty"`
	Attempts    []Attempt         `json:"attempts,omitempty"`
	ShardIndex  *int              `json:"shardIndex,omitempty"`
	Error       string            `json:"error,omitempty"`
	Priority    string            `json:"priority,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Steps       []Step            `json:"steps,omitempty"`
	Labels      []Label           `json:"labels,omitempty"`
	Links       []Link            `json:"links,omitempty"`
}

// Attempt mirrors api-service's launch.Attempt -- one execution of a retried
// test, written by the reporters' per-attempt history. Decoded and passed
// through unchanged: the server owns validation (attempt numbers below 1 are
// dropped, a single attempt persists nothing) and its own truncation caps, so
// re-implementing either here would only risk disagreeing with it.
type Attempt struct {
	Number    int        `json:"attempt"`
	Status    string     `json:"status"`
	Duration  int64      `json:"duration,omitempty"` // nanoseconds
	StartedAt *time.Time `json:"startedAt,omitempty"`
	UID       string     `json:"attemptId,omitempty"`
	Message   string     `json:"message,omitempty"`
	Trace     string     `json:"trace,omitempty"`
	Snippet   string     `json:"snippet,omitempty"`
	Line      *int       `json:"line,omitempty"`
	Stdout    []string   `json:"stdout,omitempty"`
	Stderr    []string   `json:"stderr,omitempty"`
}

// Label mirrors api-service's launch.Label -- Allure-style name/value
// metadata written by the reporters' qualflare.label() API.
type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Link mirrors api-service's launch.Link. Type must be one of
// issue/tms/custom server-side; passed through verbatim so an unknown value
// is rejected loudly there rather than silently rewritten here.
type Link struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url"`
}

type Step struct {
	Name        string      `json:"name"`
	Keyword     string      `json:"keyword,omitempty"`
	Status      string      `json:"status"`
	Duration    int64       `json:"duration"` // nanoseconds
	Error       string      `json:"error,omitempty"`
	Location    string      `json:"location,omitempty"`
	ParentIndex *int        `json:"parentIndex,omitempty"`
	Parameters  []Parameter `json:"parameters,omitempty"`
}

// Parameter mirrors api-service's launch.Parameter.
type Parameter struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Masked bool   `json:"masked,omitempty"`
}

type Attachment struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Content  string `json:"content,omitempty"` // Base64 encoded
	// LocalVideoPath and LocalTracePath are relative to the report file this
	// Attachment came from — resolved to an absolute path in ParsePath before
	// either reaches convertCase. See domain.Attachment.LocalPath's doc
	// comment.
	//
	// Two fields rather than one generic path because the wire contract is
	// append-only: reporters already published write `localVideoPath`, and
	// renaming it would strand every one of them. `localTracePath` is additive,
	// and an older CLI simply ignores it.
	LocalVideoPath string `json:"localVideoPath,omitempty"`
	LocalTracePath string `json:"localTracePath,omitempty"`
}

// Parse decodes one Collect JSON file into a single synthetic wrapper
// domain.Suite, flattening every real suite's cases into one list — see
// buildSuite for the flattening details. Called directly by tests exercising
// fields other than video; every real collect invocation goes through
// ParsePath instead (see its doc comment), since Parse has no filesystem
// path to resolve a LocalVideoPath against and leaves it as the raw relative
// string from the JSON.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var collect Collect
	if err := json.NewDecoder(reader).Decode(&collect); err != nil {
		return nil, err
	}
	return buildSuite(collect, "")
}

// ParsePath implements ports.PathAwareParser — report_service.go calls this
// instead of Parse for this framework specifically, because a video
// attachment's localVideoPath is relative to THIS file's own directory, not
// the CLI's cwd (necessary once a merge pulls files from multiple shard
// subdirectories together — see the design spec).
func (p *Parser) ParsePath(path string) (*domain.Suite, error) {
	// filepath.Dir alone doesn't make its result absolute — a relative path
	// argument (the normal case for CLI usage like `qualflare collect
	// report.json`) would otherwise leave sourceDir, and therefore
	// LocalVideoPath, relative too, contradicting the absolute-path contract
	// documented on domain.Attachment.LocalVideoPath.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var collect Collect
	if err := json.NewDecoder(f).Decode(&collect); err != nil {
		return nil, err
	}
	return buildSuite(collect, filepath.Dir(absPath))
}

// buildSuite decodes a Collect payload into a single synthetic wrapper
// domain.Suite, flattening every real suite's cases into one list —
// domain.Parser's interface returns exactly one *domain.Suite per file,
// the same constraint every other multi-suite native format in this
// factory already works within (see
// internal/adapters/parsers/e2e/cypress/cypress.go's identical
// flatten-to-wrapper-Suite pattern for Cypress's own multi-spec
// mochawesome JSON). Each case's real originating suite name is preserved
// via domain.Case.ClassName, falling back to the suite name when the case
// itself carries no className (cypress never sets one; cucumber-js's
// className is the feature/rule path, when present). sourceDir is the
// directory the report file lives in ("" when called via Parse's plain
// io.Reader path) — see convertCase for how it's used to resolve
// LocalVideoPath.
func buildSuite(collect Collect, sourceDir string) (*domain.Suite, error) {
	// The reporter that produced this file already resolved its own
	// framework (cypress/cucumber, both already-registered domain.Framework
	// values) — using ITS category, not this parser's own, is what makes a
	// cypress-mode file categorize as e2e and a cucumber-mode file
	// categorize as bdd once merged.
	suite := &domain.Suite{
		Name:      "Qualflare Test Results",
		Category:  domain.Framework(collect.Framework).GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Stopgap for the browser/platform data-loss this parser otherwise has: see
	// Collect.Platform/Collect.Browser's doc comment for why these are read from
	// the top-level Collect object. promoteConsistentSuiteProperties
	// (report_service.go) reads these same two keys off every merged suite and
	// promotes them to Launch.Properties when every suite that sets them agrees
	// — unchanged by this parser, it just needed something to read.
	if collect.Browser != "" || collect.Platform != "" || collect.Environment != "" {
		suite.Properties = make(map[string]string, 3)
		if collect.Browser != "" {
			suite.Properties["browser"] = collect.Browser
		}
		if collect.Platform != "" {
			suite.Properties["platform"] = collect.Platform
		}
		if collect.Environment != "" {
			suite.Properties["environment"] = collect.Environment
		}
	}

	var totalDurationNs int64
	for _, s := range collect.Suites {
		totalDurationNs += s.Duration
		for _, c := range s.Cases {
			suite.Cases = append(suite.Cases, convertCase(c, s.Name, sourceDir))
		}
	}
	suite.Duration = base.ParseDurationNs(totalDurationNs)

	// Derive Passed/Failed/Skipped/Errors/TotalTests from the cases just
	// built, matching every other parser in this factory (see cypress.go's
	// BUG-29 comment) — the source file already carries no separate
	// header counters to disagree with in the first place.
	suite.RecomputeCounts()

	return suite, nil
}

// resolveArtifactPath makes a report-relative artifact path absolute against
// the directory the report file itself came from. sourceDir is empty when Parse
// was called with a bare io.Reader (tests, and the non-ParsePath entry point),
// in which case the path is left exactly as written.
func resolveArtifactPath(rel, sourceDir string) string {
	if sourceDir == "" {
		return rel
	}
	return filepath.Join(sourceDir, rel)
}

func convertCase(c Case, suiteName string, sourceDir string) domain.Case {
	testCase := domain.Case{
		ID:         c.ID,
		Name:       c.Name,
		ClassName:  base.CoalesceString(c.ClassName, suiteName),
		Status:     mapStatus(c.Status),
		Duration:   base.ParseDurationNs(c.Duration),
		RetryCount: c.RetryCount,
		IsFlaky:    c.IsFlaky,
		Attempts:   mapAttempts(c.Attempts),
		ShardIndex: c.ShardIndex,
		Error:      c.Error,
		Priority:   domain.Severity(c.Priority),
		Tags:       c.Tags,
		Properties: c.Properties,
	}

	for _, a := range c.Attachments {
		attachment := domain.Attachment{
			Name:     a.Name,
			Path:     a.Path,
			MimeType: a.MimeType,
			Content:  a.Content,
		}
		// A trace and a video are mutually exclusive on one attachment; if a
		// report somehow carries both, the video wins, matching the field that
		// has existed longer.
		if rel, kind := a.LocalVideoPath, domain.ArtifactKindVideo; rel != "" {
			attachment.LocalPath, attachment.ArtifactKind = resolveArtifactPath(rel, sourceDir), kind
		} else if rel, kind := a.LocalTracePath, domain.ArtifactKindTrace; rel != "" {
			attachment.LocalPath, attachment.ArtifactKind = resolveArtifactPath(rel, sourceDir), kind
		}
		testCase.Attachments = append(testCase.Attachments, attachment)
	}

	for _, s := range c.Steps {
		step := domain.Step{
			Name:        s.Name,
			Keyword:     s.Keyword,
			Status:      mapStatus(s.Status),
			Duration:    base.ParseDurationNs(s.Duration),
			Error:       s.Error,
			Location:    s.Location,
			ParentIndex: s.ParentIndex,
		}
		for _, p := range s.Parameters {
			step.Parameters = append(step.Parameters, domain.Parameter{
				Name:   p.Name,
				Value:  p.Value,
				Masked: p.Masked,
			})
		}
		testCase.Steps = append(testCase.Steps, step)
	}

	// Labels/links are appended rather than assigned so an absent source
	// field stays nil instead of becoming an empty slice -- the server
	// distinguishes omitted from [] in its validators.
	for _, l := range c.Labels {
		testCase.Labels = append(testCase.Labels, domain.Label{Name: l.Name, Value: l.Value})
	}
	for _, l := range c.Links {
		testCase.Links = append(testCase.Links, domain.Link{Type: l.Type, Name: l.Name, URL: l.URL})
	}

	return testCase
}

// mapStatus maps the reporters' 7-value CaseStatus (shared/types.ts) onto
// domain.Status, which now carries all seven too — so timeout and aborted pass
// straight through instead of folding into failed/error as they used to. That
// fold was pure loss: both ends could always express the distinction, only this
// enum could not.
//
// `pending` is the one value still deliberately mapped to StatusSkipped, and
// for a semantic reason rather than a missing constant — mirroring cypress.go's
// BUG-08 fix. In Mocha, `pending` is what an `it.skip` fires, so it MEANS
// skipped; StatusPending out-ranks passed at the suite/launch level
// server-side, and treating it literally would flip an otherwise-green merged
// launch pending for one skipped test.
//
// The CTRF parser does NOT fold pending, because CTRF's enum carries skipped
// and pending as separate values and a document saying pending means pending.
// Same word, two meanings, decided by the source.
func mapStatus(status string) domain.Status {
	switch status {
	case "passed":
		return domain.StatusPassed
	case "failed":
		return domain.StatusFailed
	case "error":
		return domain.StatusError
	// Passed through rather than folded. The reporters' own CaseStatus union has
	// always carried these two, and the server stores them, so collapsing them
	// into failed/error here discarded a distinction both ends could express.
	case "timeout":
		return domain.StatusTimeout
	case "aborted":
		return domain.StatusAborted
	case "skipped", "pending":
		return domain.StatusSkipped
	default:
		// Every status this parser can ever actually receive is constrained
		// by the reporters' own TypeScript CaseStatus union — reaching here
		// means a schema drift between this parser and that contract, not a
		// normal outcome. Surface it loudly (red) rather than silently
		// rolling up green.
		return domain.StatusError
	}
}

// GetFramework returns the framework this parser handles.
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkQualflareJSON
}

// SupportedFileExtensions returns supported file extensions.
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}

// mapAttempts converts the report's attempt history into the domain model.
//
// Deliberately a straight copy with no filtering: the server already drops
// attempt numbers below 1 and discards a lone attempt, and duplicating those
// rules here would mean two places to keep in sync with one of them invisible.
// The one real conversion is duration, which arrives in nanoseconds — the
// wire's unit throughout — and must stay a time.Duration so it is not
// re-scaled downstream.
func mapAttempts(in []Attempt) []domain.Attempt {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.Attempt, 0, len(in))
	for _, a := range in {
		out = append(out, domain.Attempt{
			Number:    a.Number,
			Status:    mapStatus(a.Status),
			Duration:  base.ParseDurationNs(a.Duration),
			StartedAt: a.StartedAt,
			UID:       a.UID,
			Message:   a.Message,
			Trace:     a.Trace,
			Snippet:   a.Snippet,
			Line:      a.Line,
			Stdout:    a.Stdout,
			Stderr:    a.Stderr,
		})
	}
	return out
}
