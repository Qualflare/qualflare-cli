// Package ports declares the interfaces that connect the domain to its adapters.
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

// PathAwareParser is an optional extension for parsers that need
// filesystem-path access beyond a single io.Reader: a companion file next
// to the primary input (Maestro's commands.json beside its JUnit XML), or a
// directory bundle a plain io.Reader can't represent (XCTest's .xcresult).
//
// Opt-in: report_service.go type-asserts a Parser for this and calls
// ParsePath instead of Parse when present. Implementations own ALL their
// own I/O and must degrade gracefully — a missing companion file, a missing
// external tool, or unexpected output shape are reasons to fall back to
// case-level-only results, never reasons to fail the whole collect.
type PathAwareParser interface {
	Parser
	// ParsePath parses the test results rooted at path. path may name a
	// regular file or a directory; the implementation owns all of its own
	// I/O rather than being handed a pre-opened reader.
	ParsePath(path string) (*domain.Suite, error)
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
	GetEnvironment() string
	GetLanguage() string
	GetPlatform() string
	GetMilestone() int64
	GetMaxFileSize() int64
	GetCLIVersion() string

	// Git information
	GetBranch() string
	GetCommit() string

	// Retry settings
	GetRetryConfig() (retryMax int, baseDelay, maxDelay time.Duration)

	// Request settings
	GetTimeout() time.Duration

	// Output settings
	IsVerbose() bool
	IsQuiet() bool
	IsDryRun() bool
	IsDebug() bool
	// IsNoCaptureOutput reports whether captured stdout/stderr (system-out/
	// system-err) must be stripped from the report before sending (SEC-04).
	IsNoCaptureOutput() bool
	// IsShard reports whether --shard mode is enabled: every case from input
	// file i is tagged shard_index = i, overwriting any other mechanism's value.
	IsShard() bool

	// Validation
	Validate() error
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
