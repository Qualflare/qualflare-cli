// Package services implements the core business logic for parsing and sending test reports.
package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
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

	// context.Background(), not ctx: ctx here is runCollect's
	// context.WithTimeout(ctx, opts.timeout) (default 30s, the --timeout flag
	// meant for lightweight metadata requests), and context.WithTimeout always
	// resolves to the EARLIER of a parent and child deadline. Passing that ctx
	// through would silently cap UploadVideo's own, deliberately independent
	// 5-minute upload budget (see client.go's UploadVideo) at whatever was left
	// of the 30s after parsing and any earlier upload — for any real-sized
	// video, that composed deadline is routinely already exhausted, and since
	// video upload is fail-open, the failure is silent (a warning, but collect
	// still exits 0). Using context.Background() here lets UploadVideo's own
	// internal timeout be the only deadline governing upload duration.
	s.resolveArtifactAttachments(context.Background(), report)
	s.offloadInlineAttachments(context.Background(), report)
	s.warnIfBodyLooksTooLarge(report)

	return s.sender.SendReport(ctx, report)
}

// resolveArtifactAttachments walks every attachment in the merged launch and
// resolves any LocalPath (set by the qualflare-native parser's ParsePath — see
// internal/adapters/parsers/native/qualflare) into a real StorageKey/FileSize
// via the presigned-URL flow.
//
// GATING. An artifact kind is only uploaded when --upload-artifacts asked for
// it; the default uploads nothing. A video or a Playwright trace is the largest
// thing in a report by an order of magnitude, and the previous behaviour
// uploaded every one automatically with no way to decline.
//
// A gated-out artifact is REMOVED from the payload, not merely left unresolved.
// That distinction is load-bearing: the server persists an attachment row from
// its Name alone — it does not filter out one with both Content and StorageKey
// empty — so leaving it would put an undownloadable placeholder in the UI for
// every skipped artifact. Dropping it means a gated run simply has no video
// rows, which is what the user asked for.
//
// A FAILED upload is deliberately not dropped, and keeps the old behaviour of
// leaving the placeholder: a fault is worth seeing in the UI, a deliberate skip
// is not.
//
// Fail-open per attachment otherwise, matching the policy the reporters
// themselves used before this responsibility moved here.
//
// The ctx passed in by ProcessTestResults is deliberately NOT the
// deadline-bearing --timeout ctx — see the call site's comment — so a caller
// whose ctx is already expired or near-expired does not, by itself, prevent an
// upload from being attempted here.
func (s *ReportService) resolveArtifactAttachments(ctx context.Context, launch *domain.Launch) {
	// Memoizes one upload's result per distinct absolute LocalPath, so an
	// artifact referenced by more than one attachment (e.g. a spec-level
	// recording attached to several test cases) is uploaded once, not once per
	// attachment. Scoped to a single call — ReportService instances are reused
	// across unrelated ProcessTestResults calls (in tests, at least) where a
	// stale entry would be wrong.
	type uploadResult struct {
		storageKey string
		fileSize   int64
		err        error
	}
	uploaded := make(map[string]uploadResult)
	skipped := make(map[string]int)

	for i := range launch.Suites {
		for j := range launch.Suites[i].Cases {
			attachments := launch.Suites[i].Cases[j].Attachments
			kept := attachments[:0]

			for k := range attachments {
				if attachments[k].LocalPath == "" {
					kept = append(kept, attachments[k])
					continue
				}

				kind := attachments[k].ArtifactKind
				if !s.config.IsArtifactUploadEnabled(kind) {
					// Dropped, not kept — see the placeholder note above.
					skipped[kind]++
					continue
				}

				localPath := attachments[k].LocalPath
				result, ok := uploaded[localPath]
				if !ok {
					storageKey, fileSize, err := s.sender.UploadVideo(ctx, localPath, attachments[k].MimeType)
					result = uploadResult{storageKey: storageKey, fileSize: fileSize, err: err}
					uploaded[localPath] = result
				}

				if result.err != nil {
					fmt.Fprintf(s.warnWriter(), "skipping %s attachment %q (%s): %v\n",
						kind, attachments[k].Name, localPath, result.err)
					attachments[k].LocalPath = ""
					attachments[k].ArtifactKind = ""
					kept = append(kept, attachments[k])
					continue
				}
				attachments[k].StorageKey = result.storageKey
				attachments[k].FileSize = result.fileSize
				attachments[k].LocalPath = ""
				attachments[k].ArtifactKind = ""
				kept = append(kept, attachments[k])
			}
			launch.Suites[i].Cases[j].Attachments = kept
		}
	}

	s.warnSkippedArtifacts(skipped)
}

// maxInlineBodyWarnBytes is the point past which the assembled request is close
// enough to /collect's 10MB server-side limit to be worth saying so. The server
// answers an oversized body with a bare 413 before parsing anything, which
// reaches the user as an unexplained failure with the whole launch lost.
const maxInlineBodyWarnBytes = 8 << 20

// attachmentUploadConcurrency bounds how many attachment PUTs are in flight.
// Serial uploads would make a suite with hundreds of screenshots unusable at
// collect time; unbounded would open a socket per attachment.
const attachmentUploadConcurrency = 8

// offloadInlineAttachments moves every base64 `content` attachment out of the
// request body and into blob storage, replacing it with a storageKey.
//
// WHY THIS EXISTS
// An inlined attachment competes with the test results for /collect's single
// 10MB body budget. Reporters guard that with a per-run inline cap, but the cap
// is PER PROCESS and collect merges every shard into ONE request, so it does not
// compose: eleven shards each honouring a 750KB budget still assemble a body
// over the limit, and the server answers with a 413 that loses the entire
// launch. Nobody has to raise a cap for this to happen.
//
// Videos and traces already avoid the body this way. This puts screenshots on
// the same path.
//
// Fail-open per attachment, matching resolveArtifactAttachments: an upload that
// fails leaves the attachment inline and warns. Dropping a user's screenshot to
// save a request would be the wrong trade — worst case we are back to today's
// behaviour for that one file.
func (s *ReportService) offloadInlineAttachments(ctx context.Context, launch *domain.Launch) {
	type target struct {
		suite, kase, att int
		data             []byte
	}

	var targets []target
	for i := range launch.Suites {
		for j := range launch.Suites[i].Cases {
			atts := launch.Suites[i].Cases[j].Attachments
			for k := range atts {
				if atts[k].Content == "" || atts[k].StorageKey != "" {
					continue
				}
				data, err := base64.StdEncoding.DecodeString(atts[k].Content)
				if err != nil {
					// Not ours to report: the parser accepted this content, and a
					// decode failure here would be a confusing place to surface it.
					continue
				}
				targets = append(targets, target{i, j, k, data})
			}
		}
	}
	if len(targets) == 0 {
		return
	}

	// One upload per distinct payload. The same screenshot attached to several
	// cases is one object in storage, mirroring how resolveArtifactAttachments
	// memoises by path.
	type uploaded struct {
		storageKey string
		size       int64
		err        error
	}
	var mu sync.Mutex
	byHash := make(map[string]*uploaded)
	order := make([]string, 0, len(targets))
	hashes := make([]string, len(targets))
	for i, t := range targets {
		h := fmt.Sprintf("%x", sha256.Sum256(t.data))
		hashes[i] = h
		if _, seen := byHash[h]; !seen {
			byHash[h] = nil
			order = append(order, h)
		}
	}
	firstFor := make(map[string]target, len(order))
	for i, t := range targets {
		if _, ok := firstFor[hashes[i]]; !ok {
			firstFor[hashes[i]] = t
		}
	}

	sem := make(chan struct{}, attachmentUploadConcurrency)
	var wg sync.WaitGroup
	for _, h := range order {
		h, t := h, firstFor[h]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			att := launch.Suites[t.suite].Cases[t.kase].Attachments[t.att]
			key, size, err := s.sender.UploadAttachment(ctx, t.data, att.MimeType, att.Name)
			mu.Lock()
			byHash[h] = &uploaded{storageKey: key, size: size, err: err}
			mu.Unlock()
		}()
	}
	wg.Wait()

	failed := 0
	for i, t := range targets {
		res := byHash[hashes[i]]
		if res == nil || res.err != nil {
			failed++
			continue
		}
		att := &launch.Suites[t.suite].Cases[t.kase].Attachments[t.att]
		att.StorageKey = res.storageKey
		att.FileSize = res.size
		att.Content = ""
	}
	if failed > 0 {
		fmt.Fprintf(s.warnWriter(),
			"%d attachment(s) could not be uploaded and were left inline; they still count against the request size\n",
			failed)
	}
}

// warnIfBodyLooksTooLarge says plainly what the server would otherwise answer
// with a bare 413. Nothing here prevents the failure — it only stops it being
// mysterious when an attachment could not be offloaded.
func (s *ReportService) warnIfBodyLooksTooLarge(launch *domain.Launch) {
	inline := 0
	for i := range launch.Suites {
		for j := range launch.Suites[i].Cases {
			for _, a := range launch.Suites[i].Cases[j].Attachments {
				inline += len(a.Content)
			}
		}
	}
	if inline > maxInlineBodyWarnBytes {
		fmt.Fprintf(s.warnWriter(),
			"warning: %d MB of attachments are still inline; /collect rejects a body over 10MB outright, "+
				"which fails the whole launch\n", inline>>20)
	}
}

// warnSkippedArtifacts tells the user exactly what was left out and how to
// include it. Silence here would be the worst outcome of the gate: videos used
// to upload automatically, so someone upgrading loses them, and must be told
// why rather than discovering it in the UI.
func (s *ReportService) warnSkippedArtifacts(skipped map[string]int) {
	if len(skipped) == 0 {
		return
	}
	kinds := make([]string, 0, len(skipped))
	for _, kind := range domain.AllArtifactKinds() {
		if skipped[kind] > 0 {
			kinds = append(kinds, fmt.Sprintf("%d %s", skipped[kind], kind))
		}
	}
	fmt.Fprintf(s.warnWriter(),
		"skipped %s attachment(s): upload them with --upload-artifacts=%s\n",
		strings.Join(kinds, ", "), strings.Join(sortedKeys(skipped), ","))
}

// sortedKeys keeps the suggested flag value deterministic, so the message is
// the same across runs and copy-pasteable from any of them.
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
	launchProps := promoteConsistentSuiteProperties(testSuites, "browser", "platform", "environment")

	// The qualflare-json reporters write the environment they were configured
	// with into their report. Until this was read, it went nowhere and the
	// launch landed in the CLI's default environment instead — silently, and
	// wherever "development" existed, in the wrong place rather than nowhere.
	// A --environment flag or QF_ENVIRONMENT still wins: the person running
	// the upload outranks the file being uploaded.
	//
	// It is deleted from launchProps afterwards because Environment is a
	// first-class Launch field the server validates and displays; leaving a
	// duplicate "environment" property would just be noise in the UI.
	s.config.SetEnvironmentFallback(launchProps["environment"])
	delete(launchProps, "environment")

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
