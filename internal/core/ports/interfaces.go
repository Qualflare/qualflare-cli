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

// LogStepExtractor derives domain.Case entries (each with its own nested
// Steps) from a wrapped test command's captured console output (interleaved
// stdout+stderr, in the order the process wrote them). Unlike Parser, there
// is no structured file to decode — everything comes from a framework's own
// human-readable reporter text.
type LogStepExtractor interface {
	// ExtractCases parses captured output and returns one domain.Case per
	// detected test boundary, each Case's Steps populated from that
	// framework's own step-narration convention.
	ExtractCases(output []byte) ([]domain.Case, error)
	// GetFramework returns the framework this extractor targets.
	GetFramework() domain.Framework
	// Detect reports whether this extractor's console convention appears
	// present in output, used for auto-selection when --format is not given.
	Detect(output []byte) bool
}

// LogStepExtractorRegistry mirrors ParserFactory for the log-capture path.
type LogStepExtractorRegistry interface {
	GetExtractor(framework domain.Framework) (LogStepExtractor, error)
	DetectExtractor(output []byte) (LogStepExtractor, error)
	RegisterExtractor(extractor LogStepExtractor)
	GetSupportedFrameworks() []domain.Framework
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

	// BuildCapturedLaunch wraps an already-built Suite (from `qf run`'s
	// log-capture mode) into a Launch, applying the same priority
	// normalization / --no-capture-output / --shard rules ParseTestResults
	// applies, without sending it.
	BuildCapturedLaunch(suite *domain.Suite, framework domain.Framework) *domain.Launch
	// ProcessCapturedSuite builds and sends a Launch from an already-built
	// Suite, honoring --dry-run exactly like ProcessTestResults.
	ProcessCapturedSuite(ctx context.Context, suite *domain.Suite, framework domain.Framework) error
}

// ValidationResult represents the result of validating a file
type ValidationResult struct {
	FilePath  string
	Valid     bool
	Framework domain.Framework
	Error     string
	TestCount int
}
