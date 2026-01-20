package services

import (
	"context"
	"fmt"
	"os"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"qualflare-cli/internal/version"
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
		return nil, fmt.Errorf("no files provided")
	}

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
	var detectedFramework domain.Framework

	for _, filePath := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		currentParser := parser

		// Auto-detect framework if not specified
		if currentParser == nil {
			detectedFramework, err = s.detectFramework(filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to detect framework for file %s: %w", filePath, err)
			}

			currentParser, err = s.parserFactory.GetParser(detectedFramework)
			if err != nil {
				return nil, fmt.Errorf("failed to get parser for detected framework %s: %w", detectedFramework, err)
			}
		}

		suite, err := s.parseFile(filePath, currentParser)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", filePath, err)
		}

		testSuites = append(testSuites, *suite)

		// Use the detected framework if no explicit framework was provided
		if framework == "" && detectedFramework != "" {
			framework = detectedFramework
		}
	}

	// Use the parser's framework if we had an explicit parser
	if parser != nil {
		framework = parser.GetFramework()
	}

	return s.createReport(testSuites, framework), nil
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
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	suite, err := parser.Parse(file)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file content: %w", err)
	}

	return suite, nil
}

// createReport creates a Launch report from test suites
func (s *ReportService) createReport(testSuites []domain.Suite, framework domain.Framework) *domain.Launch {
	return &domain.Launch{
		Framework:   string(framework),
		Platform:    fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		OS:          runtime.GOOS,
		Environment: s.config.GetEnvironment(),
		Language:    s.config.GetLanguage(),
		Branch:      s.config.GetBranch(),
		Commit:      s.config.GetCommit(),
		Metadata: domain.Metadata{
			Version:   version.Version,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			CLIName:   "qf",
		},
		Suites: testSuites,
	}
}
