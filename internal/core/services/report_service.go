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
	"strconv"
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

	s.resolveVideoAttachments(ctx, report)

	return s.sender.SendReport(ctx, report)
}

// resolveVideoAttachments walks every attachment in the merged launch and
// resolves any LocalVideoPath (set by the qualflare-native parser's
// ParsePath — see internal/adapters/parsers/native/qualflare) into a real
// StorageKey/FileSize via the presigned-URL flow. Fail-open per attachment,
// matching the policy the reporters themselves used before this
// responsibility moved here: a failed upload is logged and the attachment
// is left with neither StorageKey nor LocalVideoPath resolved (effectively
// dropped server-side, since neither Content nor StorageKey ends up set) —
// it never fails the whole collect.
func (s *ReportService) resolveVideoAttachments(ctx context.Context, launch *domain.Launch) {
	for i := range launch.Suites {
		for j := range launch.Suites[i].Cases {
			attachments := launch.Suites[i].Cases[j].Attachments
			for k := range attachments {
				if attachments[k].LocalVideoPath == "" {
					continue
				}
				localPath := attachments[k].LocalVideoPath
				storageKey, fileSize, err := s.sender.UploadVideo(ctx, localPath, attachments[k].MimeType)
				if err != nil {
					fmt.Fprintf(s.warnWriter(), "skipping video attachment %q (%s): %v\n", attachments[k].Name, localPath, err)
					attachments[k].LocalVideoPath = ""
					continue
				}
				attachments[k].StorageKey = storageKey
				attachments[k].FileSize = fileSize
				attachments[k].LocalVideoPath = ""
			}
		}
	}
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

		// The rest of --no-capture-output is applied in createReport, which sees only
		// []domain.Suite. This half has to happen here because it is the one filter
		// that depends on WHICH parser produced the suite, and that is known only
		// inside this loop — a launch can mix frameworks, so the launch-level label
		// resolved below cannot answer it.
		if s.config.IsNoCaptureOutput() {
			stripUserAuthoredSuiteProperties(suite, currentParser.GetFramework())
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
	// straight into memory (BUG-18). Skipped for a directory (e.g. an XCTest
	// .xcresult bundle) — a directory-entry's Stat size isn't what this cap
	// protects against, and a PathAwareParser reading one owns its own I/O
	// bounds.
	if info, statErr := os.Stat(filePath); statErr == nil && !info.IsDir() && info.Size() > s.config.GetMaxFileSize() {
		return nil, fmt.Errorf("file %s is too large (%d bytes, max %d bytes)", filePath, info.Size(), s.config.GetMaxFileSize())
	}

	// Opt-in extension point: a parser needing directory-bundle access
	// takes over all I/O for this input.
	if pathParser, ok := parser.(ports.PathAwareParser); ok {
		suite, err := pathParser.ParsePath(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file content: %w", err)
		}
		return suite, nil
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
		// Suite-level properties are filtered in ParseTestResults, where the producing
		// parser is still known — see stripUserAuthoredSuiteProperties.
		stripSensitiveCaseProperties(testSuites)
	}
	if s.config.IsShard() {
		tagShardsByFile(testSuites, s.warnWriter())
	}

	// browser/platform are already structural, parser-generated Suite.Properties
	// (Selenium, Playwright, TestCafe) — promoting them here is not putting new
	// user-authored data through an unvetted path, so no --no-capture-output
	// allowlist consideration is needed for THIS producer specifically. A future
	// producer of user-authored Launch.Properties (e.g. a --property flag) would
	// need its own allowlist, same as stripUserAuthoredSuiteProperties's pytest case.
	launchProps := promoteConsistentSuiteProperties(testSuites, "browser", "platform")

	return &domain.Launch{
		Framework:   string(framework),
		Platform:    s.config.GetPlatform(),
		OS:          fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		Browser:     launchProps["browser"],
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
		Properties: launchProps,
		Suites:     testSuites,
	}
}

// promoteConsistentSuiteProperties collects, for each key, the value every
// suite that sets it agrees on. A key is omitted from the result — never
// guessed — when suites disagree (a genuine multi-browser/multi-platform
// run) or when no suite sets it at all. Suite-level values are left
// untouched; this only reads them.
func promoteConsistentSuiteProperties(suites []domain.Suite, keys ...string) map[string]string {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		value, consistent := "", true
		seen := false
		for _, suite := range suites {
			v, ok := suite.Properties[key]
			if !ok || v == "" {
				continue
			}
			if !seen {
				value, seen = v, true
				continue
			}
			if v != value {
				consistent = false
				break
			}
		}
		if seen && consistent {
			result[key] = value
		}
	}
	return result
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
	// keys are open-ended and handled by prefix in isStructuralProperty).
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

// k6MetricAggregations are the aggregation keys k6 puts in a metric's `values` object,
// which the k6 parser copies verbatim onto each threshold case. They are tool-generated
// measurements, structural in exactly the way trivy's cvss_* scores are — but unlike
// cvss_*, several are bare English words that a test author could plausibly also use as
// a custom property name, so membership here is not sufficient on its own: see
// isStructuralProperty.
//
// Trend metrics also emit percentiles (p(90), p(95), and any p(N) a threshold defines),
// handled by prefix.
var k6MetricAggregations = map[string]struct{}{
	"avg": {}, "min": {}, "med": {}, "max": {}, "count": {}, "rate": {}, "value": {},
}

// isStructuralProperty reports whether a property is one this repo's parsers generate
// from a report's structure, rather than something a test author wrote.
//
// It takes the value as well as the key because one family of keys (k6's metric
// aggregations) cannot be decided by name alone. Callers with no value to offer — or
// that want the stricter key-only answer — pass "".
func isStructuralProperty(key, value string) bool {
	if _, ok := structuralCaseProperties[key]; ok {
		return true
	}
	// trivy emits one CVSS score per scoring source (cvss_nvd, cvss_redhat, ...): an
	// open-ended key set, but every member is generated by the scanner.
	if strings.HasPrefix(key, "cvss_") {
		return true
	}
	// k6 threshold metrics. The key alone is too weak a signal — "max" or "rate" is a
	// name a test author might also pick — so the value must also be what the k6 parser
	// writes there: a formatted float and nothing else. That keeps k6's threshold
	// numbers, which the flag was gutting for no security benefit, without handing every
	// passthrough parser a way to smuggle a secret out under a six-word vocabulary.
	if _, ok := k6MetricAggregations[key]; ok {
		return isFormattedNumber(value)
	}
	return strings.HasPrefix(key, "p(") && isFormattedNumber(value)
}

// isFormattedNumber reports whether s is entirely a decimal number, i.e. carries no
// information beyond a measurement. Empty is not a number, so a caller passing "" gets
// false for the value-dependent families.
func isFormattedNumber(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
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
// any other user-authored key are dropped, and only what isStructuralProperty accepts
// survives.
//
// Status, timing, error messages and attachments are untouched — only Properties is
// filtered. Ranging over a map while deleting from it is defined behaviour in Go, and
// ranging a nil map is a no-op.
func stripSensitiveCaseProperties(suites []domain.Suite) {
	for i := range suites {
		for j := range suites[i].Cases {
			for key, value := range suites[i].Cases[j].Properties {
				if !isStructuralProperty(key, value) {
					delete(suites[i].Cases[j].Properties, key)
				}
			}
		}
	}
}

// stripUserAuthoredSuiteProperties applies the same allowlist to a suite's own
// Properties, for --no-capture-output (SEC-04), when the producing parser is one that
// puts user-authored text there.
//
// Only pytest does. record_testsuite_property() is the sibling of the record_property()
// the case-level filter already covers, and pytest.go copies every <testsuite>-level
// <property> through verbatim, so a secret recorded that way survived the flag entirely
// — the case-level fix alone left the flag's promise half true. Every other parser fills
// suite Properties from fixed structural keys of its own (zapVersion, artifactName,
// browser, k6's http_req_* summary, ...) with no user-authored text among them, and
// junitxml does not decode <testsuite>-level <properties> at all, so they are left
// alone: filtering them would need a second, suite-specific allowlist whose only effect
// would be to silently gut scanner and load-test summaries the day a key is missed.
//
// Skipping non-pytest frameworks is a deliberate scope limit, not an oversight. Should
// another parser ever pass user-authored suite properties through, add it here.
func stripUserAuthoredSuiteProperties(suite *domain.Suite, framework domain.Framework) {
	if framework != domain.FrameworkPython {
		return
	}
	// The key-only form of the allowlist: pytest is a passthrough parser, so nothing it
	// puts here is a k6-style measurement, and the value-dependent families would only
	// widen what a record_testsuite_property() call can smuggle past the flag.
	for key := range suite.Properties {
		if !isStructuralProperty(key, "") {
			delete(suite.Properties, key)
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
