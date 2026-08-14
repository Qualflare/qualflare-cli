// Package services implements the core business logic for parsing and sending test reports.
package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"runtime"
	"strings"
	"time"
)

// ReportService handles test report processing
type ReportService struct {
	parserFactory ports.ParserFactory
	sender        ports.ReportSender
	config        ports.ConfigProvider
	// warn receives operator-facing diagnostics only — never report data — so it is
	// stderr in production (stdout stays machine-parseable, BUG-03/04) and a buffer
	// under test.
	warn io.Writer
}

// NewReportService creates a new report service
func NewReportService(
	parserFactory ports.ParserFactory,
	sender ports.ReportSender,
	config ports.ConfigProvider,
) *ReportService {
	return &ReportService{
		parserFactory: parserFactory,
		sender:        sender,
		config:        config,
		warn:          os.Stderr,
	}
}

// warnWriter never returns nil, so a zero-value ReportService cannot panic on a warning.
func (s *ReportService) warnWriter() io.Writer {
	if s.warn == nil {
		return io.Discard
	}
	return s.warn
}

// ProcessTestResults parses files and sends results to the API
func (s *ReportService) ProcessTestResults(ctx context.Context, files []string, framework domain.Framework) error {
	report, err := s.ParseTestResults(ctx, files, framework)
	if err != nil {
		return err
	}

	// Check for dry run mode
	if s.config.IsDryRun() {
		return nil
	}

	return s.sender.SendReport(ctx, report)
}

// ParseTestResults parses files and returns the parsed report without sending
func (s *ReportService) ParseTestResults(ctx context.Context, files []string, framework domain.Framework) (*domain.Launch, error) {
	if len(files) == 0 {
		return nil, errors.New("no files provided")
	}

	// Dedupe by resolved absolute path: the same file passed twice (directly or
	// via overlapping globs) was parsed twice and silently double-counted every
	// result in it (BUG-40).
	files = dedupeFiles(files)

	var parser ports.Parser
	var err error

	// Get parser for specified framework
	if framework != "" {
		parser, err = s.parserFactory.GetParser(framework)
		if err != nil {
			return nil, fmt.Errorf("failed to get parser for framework %s: %w", framework, err)
		}
	}

	testSuites := make([]domain.Suite, 0, len(files))
	detected := make(map[domain.Framework]struct{})

	for _, filePath := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		currentParser := parser

		// Auto-detect the framework when --format was not given.
		if currentParser == nil {
			p, detectedFramework, perr := s.parserForFile(filePath)
			if perr != nil {
				return nil, perr
			}
			currentParser = p
			detected[detectedFramework] = struct{}{}
		}

		suite, parseErr := s.parseFile(filePath, currentParser)
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", filePath, parseErr)
		}

		testSuites = append(testSuites, *suite)
	}

	framework = resolveLaunchFramework(parser, detected, framework)

	return s.createReport(testSuites, framework), nil
}

// parserForFile detects a file's framework and returns the parser for it, reporting
// which framework was detected so the caller can track agreement across files.
func (s *ReportService) parserForFile(filePath string) (ports.Parser, domain.Framework, error) {
	detectedFramework, err := s.detectFramework(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to detect framework for file %s: %w", filePath, err)
	}

	parser, err := s.parserFactory.GetParser(detectedFramework)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get parser for detected framework %s: %w", detectedFramework, err)
	}
	return parser, detectedFramework, nil
}

// resolveLaunchFramework picks the label for the launch as a whole. An explicit --format
// wins. Otherwise, when every auto-detected file agreed, that framework is used; when
// they disagree the launch is labelled "mixed" rather than tagged with whichever file
// happened to be parsed first (BUG-41). The server stores framework as a free string
// (required,max=100), so "mixed" is a valid, honest label.
func resolveLaunchFramework(parser ports.Parser, detected map[domain.Framework]struct{}, current domain.Framework) domain.Framework {
	switch {
	case parser != nil:
		return parser.GetFramework()
	case len(detected) > 1:
		return "mixed"
	case len(detected) == 1:
		for f := range detected {
			return f
		}
	}
	return current
}

// ValidateFiles validates that files can be parsed
func (s *ReportService) ValidateFiles(ctx context.Context, files []string, framework domain.Framework) ([]ports.ValidationResult, error) {
	results := make([]ports.ValidationResult, 0, len(files))

	for _, filePath := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result := ports.ValidationResult{
			FilePath: filePath,
		}

		// Check if file exists
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			result.Valid = false
			result.Error = "file does not exist"
			results = append(results, result)
			continue
		}

		// Detect or use specified framework
		var detectedFramework domain.Framework
		var err error

		if framework != "" {
			detectedFramework = framework
		} else {
			detectedFramework, err = s.detectFramework(filePath)
			if err != nil {
				result.Valid = false
				result.Error = fmt.Sprintf("failed to detect framework: %v", err)
				results = append(results, result)
				continue
			}
		}

		result.Framework = detectedFramework

		// Get parser
		parser, err := s.parserFactory.GetParser(detectedFramework)
		if err != nil {
			result.Valid = false
			result.Error = fmt.Sprintf("unsupported framework: %s", detectedFramework)
			results = append(results, result)
			continue
		}

		// Try to parse the file
		suite, err := s.parseFile(filePath, parser)
		if err != nil {
			result.Valid = false
			result.Error = fmt.Sprintf("parse error: %v", err)
			results = append(results, result)
			continue
		}

		result.Valid = true
		result.TestCount = suite.TotalTests
		results = append(results, result)
	}

	return results, nil
}

// detectFramework detects the framework for a file
func (s *ReportService) detectFramework(filePath string) (domain.Framework, error) {
	// Check file size before reading
	info, err := os.Stat(filePath)
	if err != nil {
		return s.parserFactory.DetectFramework(filePath)
	}
	if info.Size() > s.config.GetMaxFileSize() {
		return "", fmt.Errorf("file %s is too large (%d bytes, max %d bytes)", filePath, info.Size(), s.config.GetMaxFileSize())
	}

	// First try content-based detection
	content, err := os.ReadFile(filePath)
	if err != nil {
		// Fall back to filename-based detection
		return s.parserFactory.DetectFramework(filePath)
	}

	return s.parserFactory.DetectFrameworkFromContent(filePath, content)
}

// parseFile parses a single file using the specified parser
func (s *ReportService) parseFile(filePath string, parser ports.Parser) (*domain.Suite, error) {
	// Enforce the size cap here (not only in detectFramework): passing --format
	// skips detection entirely, so without this an unbounded file is read
	// straight into memory (BUG-18).
	if info, statErr := os.Stat(filePath); statErr == nil && info.Size() > s.config.GetMaxFileSize() {
		return nil, fmt.Errorf("file %s is too large (%d bytes, max %d bytes)", filePath, info.Size(), s.config.GetMaxFileSize())
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	suite, err := parser.Parse(file)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file content: %w", err)
	}

	return suite, nil
}

// dedupeFiles removes duplicate file arguments, keying on the resolved absolute
// path (so ./r.xml and r.xml collapse) while preserving order and the original
// path strings. Paths that fail to resolve fall back to their literal value.
func dedupeFiles(files []string) []string {
	seen := make(map[string]struct{}, len(files))
	out := make([]string, 0, len(files))
	for _, f := range files {
		key := f
		if abs, err := filepath.Abs(f); err == nil {
			key = abs
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	return out
}

// createReport creates a Launch report from test suites
func (s *ReportService) createReport(testSuites []domain.Suite, framework domain.Framework) *domain.Launch {
	normalizeCasePriorities(testSuites)
	if s.config.IsNoCaptureOutput() {
		stripSensitiveCaseProperties(testSuites)
	}
	if s.config.IsShard() {
		tagShardsByFile(testSuites, s.warnWriter())
	}
	return &domain.Launch{
		Framework:   string(framework),
		Platform:    s.config.GetPlatform(),
		OS:          fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		Environment: s.config.GetEnvironment(),
		Language:    s.config.GetLanguage(),
		Milestone:   s.config.GetMilestone(),
		Branch:      s.config.GetBranch(),
		Commit:      s.config.GetCommit(),
		Metadata: domain.Metadata{
			Version:   s.config.GetCLIVersion(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			CLIName:   "qf",
		},
		Suites: testSuites,
	}
}

// normalizeCasePriorities coerces every case's Priority into a value the API's
// `priority` enum accepts (critical/high/medium/low). Security parsers emit
// "info"/"unknown" severities, which an older server rejects with a 500 on the
// whole launch; this guarantees a valid wire payload regardless of API version.
// The server normalizes authoritatively too — this is defense in depth.
func normalizeCasePriorities(suites []domain.Suite) {
	for i := range suites {
		for j := range suites[i].Cases {
			suites[i].Cases[j].Priority = suites[i].Cases[j].Priority.ToCasePriority()
		}
	}
}

// structuralCaseProperties is the allowlist --no-capture-output keeps: every case
// property key this repo's own parsers synthesize from a report's structure. Their
// names AND values come from the tool's schema, never from free-form user text, so
// they cannot carry a secret the way captured output or a custom property can.
//
// Anything NOT listed here is treated as user-authored and dropped by
// stripSensitiveCaseProperties — see the comment there for why. Adding a parser that
// emits a new structural case property means adding its key here, or
// --no-capture-output users will silently lose it.
var structuralCaseProperties = map[string]struct{}{
	// Shared JUnit/pytest signals the parsers themselves interpret. The parsed values
	// already live in typed fields (ShardIndex, RetryCount) by the time this runs, so
	// keeping the raw properties is about not mangling a report the user can read back
	// — and each is a bounded integer written by tooling, not free-form text.
	"shard": {}, "retries": {}, "retryCount": {},

	// Source location (junit, pytest, jest, mocha, phpunit, rspec, cypress, playwright,
	// sonarqube, k6, testcafe).
	"file": {}, "line": {}, "line_number": {}, "path": {},

	// Test identity, grouping and environment (playwright, cypress, selenium, testcafe,
	// cucumber, karate, k6).
	"project": {}, "fullTitle": {}, "methodName": {}, "fixture": {}, "feature": {},
	"uri": {}, "group": {}, "speed": {}, "browser": {}, "userAgent": {},

	// newman request/response metadata.
	"method": {}, "responseCode": {}, "responseTime": {},

	// k6 check results.
	"passes": {}, "fails": {}, "passRate": {},

	// trivy / snyk package and vulnerability metadata (trivy's per-source cvss_<source>
	// keys are open-ended and handled by prefix in isStructuralCaseProperty).
	"package": {}, "installedVersion": {}, "fixedVersion": {}, "version": {},
	"severity": {}, "url": {}, "resolution": {}, "cvssScore": {}, "isPatchable": {},
	"isUpgradable": {}, "language": {}, "packageManager": {}, "fixedIn": {},
	"dependencyPath": {},

	// zap alert metadata.
	"host": {}, "port": {}, "riskCode": {}, "riskDesc": {}, "confidence": {},
	"cweId": {}, "wascId": {}, "solution": {}, "reference": {}, "instanceCount": {},
	"affectedURL": {},

	// sonarqube issue metadata.
	"rule": {}, "ruleName": {}, "type": {}, "status": {}, "effort": {},
	"component": {}, "assignee": {},
}

// isStructuralCaseProperty reports whether a case property key is one this repo's
// parsers generate from a report's structure, rather than something a test author
// wrote.
func isStructuralCaseProperty(key string) bool {
	if _, ok := structuralCaseProperties[key]; ok {
		return true
	}
	// trivy emits one CVSS score per scoring source (cvss_nvd, cvss_redhat, ...): an
	// open-ended key set, but every member is generated by the scanner.
	return strings.HasPrefix(key, "cvss_")
}

// stripSensitiveCaseProperties drops every case property that a parser did not
// generate itself, in place, for --no-capture-output (SEC-04).
//
// It started life deleting only system-out/system-err, which was equivalent to
// emptying the map back when captured output was the only thing JUnit-family parsers
// put in it. Once generic <property> values began flowing through (they have to — the
// "shard" fallback reads one), that equivalence broke: a
// <property name="AWS_SECRET_ACCESS_KEY" value="..."/> written by a test survived the
// very flag that exists to keep such values off the server. So the rule is now an
// allowlist: captured output, custom <property> values, TestCafe fixture/test meta and
// any other user-authored key are dropped, and only structuralCaseProperties (plus
// trivy's cvss_* family) survive.
//
// Status, timing, error messages and attachments are untouched — only Properties is
// filtered. Ranging over a map while deleting from it is defined behaviour in Go, and
// ranging a nil map is a no-op.
func stripSensitiveCaseProperties(suites []domain.Suite) {
	for i := range suites {
		for j := range suites[i].Cases {
			for key := range suites[i].Cases[j].Properties {
				if !isStructuralCaseProperty(key) {
					delete(suites[i].Cases[j].Properties, key)
				}
			}
		}
	}
}

// tagShardsByFile is mechanism B. testSuites[i] is exactly the i-th input
// file's parsed Suite, in the same order ParseTestResults processed the
// (globbed, deduped) files — i.e. post-glob-expansion (matches expand in
// filepath.Glob's lexical order via expandGlobs, non-glob args pass through
// literally, both in original argument order) and post-dedupeFiles (the
// same path given twice, directly or via overlapping globs, collapses to
// ONE file and gets exactly one shard slot, not two).
//
// Every case in file i gets shard_index = i (0-based), UNCONDITIONALLY
// overwriting whatever mechanism A or C already set: --shard is an
// explicit, per-invocation user statement that these files are separate
// shards/machines, and it must win even over a file's own native worker
// index. This is exactly the scenario --shard exists for — merging
// multiple already-parallel shards' own report files — where a native
// per-file WorkerIndex is only locally unique within that one shard/machine
// and would otherwise collide once merged with another file's WorkerIndex
// values (e.g. two Playwright shard files each have their own worker 0).
//
// With fewer than 2 suites (i.e. a single input file, post-dedupe), --shard
// is a no-op: there is nothing to number relative to another shard, and
// blindly stamping shard_index = 0 would silently clobber a real per-worker
// index a file's own parser already set (native WorkerIndex, or the
// shard-property fallback). This matches the flag's documented behavior
// ("requires 2+ files. Does not apply to a single file."). That no-op is
// announced on warn rather than taken silently: the user explicitly asked for
// --shard semantics, and a glob that collapsed to one file (or two spellings
// of the same path) otherwise looks like it worked.
//
// i is a slice index over the input files, so shard_index is inherently within
// the server's 32-bit range here — unlike the property-driven fallbacks, which
// bound-check their parsed value (base.ParseShardIndex).
func tagShardsByFile(suites []domain.Suite, warn io.Writer) {
	if len(suites) < 2 {
		// Counted after glob expansion and de-duplication, which is where the count can
		// differ from the number of arguments the user typed.
		fmt.Fprintf(warn,
			"warning: --shard requires 2+ input files to have any effect; %d file(s) left after glob expansion and de-duplication, shard tagging skipped\n",
			len(suites))
		return
	}
	for i := range suites {
		for j := range suites[i].Cases {
			suites[i].Cases[j].ShardIndex = domain.IntPtr(i)
		}
	}
}
