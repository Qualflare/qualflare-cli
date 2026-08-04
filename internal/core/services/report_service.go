// Package services implements the core business logic for parsing and sending test reports.
package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"runtime"
	"time"
)

// ReportService handles test report processing
type ReportService struct {
	parserFactory ports.ParserFactory
	sender        ports.ReportSender
	config        ports.ConfigProvider
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
	}
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
		stripCapturedOutput(testSuites)
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

// stripCapturedOutput removes captured stdout/stderr (JUnit system-out/system-err)
// from every case in place. Those streams routinely echo whatever an environment
// printed during a run — including secrets — and --no-capture-output opts out of
// uploading them. Test status, timing, and failure messages are left intact; only
// the bulk captured output is dropped (SEC-04). delete on a nil map is a no-op.
func stripCapturedOutput(suites []domain.Suite) {
	for i := range suites {
		for j := range suites[i].Cases {
			delete(suites[i].Cases[j].Properties, "system-out")
			delete(suites[i].Cases[j].Properties, "system-err")
		}
	}
}
