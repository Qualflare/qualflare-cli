package ports

import (
	"context"
	"io"
	"qualflare-cli/internal/core/domain"
)

type Parser interface {
	Parse(reader io.Reader) (*domain.Suite, error)
	GetFramework() domain.Framework
	SupportedFileExtensions() []string
}

// ParserFactory defines the interface for creating parsers
type ParserFactory interface {
	GetParser(framework domain.Framework) (Parser, error)
	DetectFramework(filename string) (domain.Framework, error)
	GetSupportedFrameworks() []domain.Framework
}

// ReportSender defines the interface for sending reports to the API
type ReportSender interface {
	SendReport(ctx context.Context, report *domain.Launch) error
}

// ConfigProvider defines the interface for configuration
type ConfigProvider interface {
	GetAPIKey() string
	GetProject() string
	GetEnvironment() string
	GetBranch() string
	GetCommit() string
}

// ReportService defines the core business logic interface
type ReportService interface {
	ProcessTestResults(ctx context.Context, files []string, framework domain.Framework) error
}
