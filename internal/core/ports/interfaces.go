package ports

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"qualflare-cli/internal/core/domain"
	"time"
)

// Parser defines the interface for parsing test result files
type Parser interface {
	// Parse reads test results from the reader and returns a Suite
	Parse(reader io.Reader) (*domain.Suite, error)
	// GetFramework returns the framework this parser handles
	GetFramework() domain.Framework
	// SupportedFileExtensions returns file extensions this parser supports
	SupportedFileExtensions() []string
}

// ParserFactory defines the interface for creating parsers
type ParserFactory interface {
	// GetParser returns a parser for the specified framework
	GetParser(framework domain.Framework) (Parser, error)
	// DetectFramework attempts to detect the framework from a file
	DetectFramework(filename string) (domain.Framework, error)
	// DetectFrameworkFromContent attempts to detect the framework from file content
	DetectFrameworkFromContent(filename string, content []byte) (domain.Framework, error)
	// GetSupportedFrameworks returns all supported frameworks
	GetSupportedFrameworks() []domain.Framework
	// RegisterParser registers a new parser
	RegisterParser(parser Parser)
}

// ReportSender defines the interface for sending reports to the API
type ReportSender interface {
	// SendReport sends a report to the API
	SendReport(ctx context.Context, report *domain.Launch) error
}

// APIClient defines the interface for API communication (read + write)
type APIClient interface {
	ReportSender
	Get(ctx context.Context, path string, params url.Values) (json.RawMessage, error)
}

// ConfigProvider defines the interface for configuration
type ConfigProvider interface {
	// API settings
	GetAPIKey() string
	GetAPIEndpoint() string

	// Project settings
	GetProject() string
	GetEnvironment() string
	GetLanguage() string

	// Git information
	GetBranch() string
	GetCommit() string

	// Retry settings
	GetRetryConfig() (max int, baseDelay, maxDelay time.Duration)

	// Request settings
	GetTimeout() time.Duration

	// Output settings
	IsVerbose() bool
	IsQuiet() bool
	IsDryRun() bool

	// Validation
	Validate() error
}

// ConfigMutator defines the interface for mutating configuration
type ConfigMutator interface {
	SetAPIKey(key string)
	SetAPIEndpoint(endpoint string)
	SetProject(project string)
	SetEnvironment(env string)
	SetLanguage(language string)
	SetBranch(branch string)
	SetCommit(commit string)
	SetTimeout(timeout time.Duration)
	SetVerbose(verbose bool)
	SetQuiet(quiet bool)
	SetDryRun(dryRun bool)
}

// Config combines ConfigProvider and ConfigMutator
type Config interface {
	ConfigProvider
	ConfigMutator
}

// ReportService defines the core business logic interface
type ReportService interface {
	// ProcessTestResults parses files and sends results to the API
	ProcessTestResults(ctx context.Context, files []string, framework domain.Framework) error
	// ParseTestResults parses files and returns the parsed report without sending
	ParseTestResults(ctx context.Context, files []string, framework domain.Framework) (*domain.Launch, error)
	// ValidateFiles validates that files can be parsed
	ValidateFiles(ctx context.Context, files []string, framework domain.Framework) ([]ValidationResult, error)
}

// ValidationResult represents the result of validating a file
type ValidationResult struct {
	FilePath  string
	Valid     bool
	Framework domain.Framework
	Error     string
	TestCount int
}
