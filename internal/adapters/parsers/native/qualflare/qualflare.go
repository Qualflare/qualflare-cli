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
	Framework string  `json:"framework"`
	Suites    []Suite `json:"suites"`
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
	ShardIndex  *int              `json:"shardIndex,omitempty"`
	Error       string            `json:"error,omitempty"`
	Priority    string            `json:"priority,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Steps       []Step            `json:"steps,omitempty"`
}

type Step struct {
	Name     string `json:"name"`
	Keyword  string `json:"keyword,omitempty"`
	Status   string `json:"status"`
	Duration int64  `json:"duration"` // nanoseconds
	Error    string `json:"error,omitempty"`
	Location string `json:"location,omitempty"`
}

type Attachment struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Content  string `json:"content,omitempty"` // Base64 encoded
	// LocalVideoPath, relative to the report file this Attachment came from —
	// resolved to an absolute path in ParsePath before it ever reaches
	// convertCase. See domain.Attachment.LocalVideoPath's doc comment.
	LocalVideoPath string `json:"localVideoPath,omitempty"`
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
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var collect Collect
	if err := json.NewDecoder(f).Decode(&collect); err != nil {
		return nil, err
	}
	return buildSuite(collect, filepath.Dir(path))
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

func convertCase(c Case, suiteName string, sourceDir string) domain.Case {
	testCase := domain.Case{
		ID:         c.ID,
		Name:       c.Name,
		ClassName:  base.CoalesceString(c.ClassName, suiteName),
		Status:     mapStatus(c.Status),
		Duration:   base.ParseDurationNs(c.Duration),
		RetryCount: c.RetryCount,
		IsFlaky:    c.IsFlaky,
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
		if a.LocalVideoPath != "" && sourceDir != "" {
			attachment.LocalVideoPath = filepath.Join(sourceDir, a.LocalVideoPath)
		} else if a.LocalVideoPath != "" {
			attachment.LocalVideoPath = a.LocalVideoPath
		}
		testCase.Attachments = append(testCase.Attachments, attachment)
	}

	for _, s := range c.Steps {
		testCase.Steps = append(testCase.Steps, domain.Step{
			Name:     s.Name,
			Keyword:  s.Keyword,
			Status:   mapStatus(s.Status),
			Duration: base.ParseDurationNs(s.Duration),
			Error:    s.Error,
			Location: s.Location,
		})
	}

	return testCase
}

// mapStatus maps the reporters' 7-value CaseStatus (shared/types.ts) onto
// domain.Status's 5 values. `timeout`/`aborted` have no direct equivalent
// and fold into the closest real outcome (a timeout is a failure; an
// aborted run is closer to an error than a clean pass/skip). `pending`
// deliberately maps to StatusSkipped, not domain.StatusPending — mirroring
// cypress.go's BUG-08 fix: StatusPending out-ranks passed at the
// suite/launch level server-side, so one pending case would flip an
// otherwise-green merged launch pending.
func mapStatus(status string) domain.Status {
	switch status {
	case "passed":
		return domain.StatusPassed
	case "failed", "timeout":
		return domain.StatusFailed
	case "error", "aborted":
		return domain.StatusError
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
