package services

import (
	"context"
	"fmt"
	"os"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"runtime"
	"time"
)

type ReportService struct {
	parserFactory ports.ParserFactory
	sender        ports.ReportSender
	config        ports.ConfigProvider
}

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

func (s *ReportService) ProcessTestResults(ctx context.Context, files []string, framework domain.Framework) error {
	if len(files) == 0 {
		return fmt.Errorf("no files provided")
	}

	var parser ports.Parser
	var err error

	if framework != "" {
		parser, err = s.parserFactory.GetParser(framework)
		if err != nil {
			return fmt.Errorf("failed to get parser for framework %s: %w", framework, err)
		}
	}

	testSuites := make([]domain.Suite, 0, len(files))

	for _, filePath := range files {
		// Auto-detect framework if not specified
		if parser == nil {
			detectedFramework, err := s.parserFactory.DetectFramework(filePath)
			if err != nil {
				return fmt.Errorf("failed to detect framework for file %s: %w", filePath, err)
			}

			parser, err = s.parserFactory.GetParser(detectedFramework)
			if err != nil {
				return fmt.Errorf("failed to get parser for detected framework %s: %w", detectedFramework, err)
			}
		}

		suite, err := s.parseFile(filePath, parser)
		if err != nil {
			return fmt.Errorf("failed to parse file %s: %w", filePath, err)
		}
		testSuites = append(testSuites, *suite)
	}

	report := s.createReport(testSuites, parser.GetFramework())

	return s.sender.SendReport(ctx, report)
}

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

func (s *ReportService) createReport(testSuites []domain.Suite, framework domain.Framework) *domain.Launch {
	return &domain.Launch{
		Project:     s.config.GetProject(),
		Framework:   string(framework),
		Platform:    fmt.Sprintf("%s / %s", runtime.GOOS, runtime.GOARCH),
		Environment: s.config.GetEnvironment(),
		Branch:      s.config.GetBranch(),
		Commit:      s.config.GetCommit(),
		Metadata: domain.Metadata{
			Version:   "1.0.0",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Suites: testSuites,
	}
}
