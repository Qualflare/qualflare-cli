package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	maxRetryCount  = 10
	maxFileSize    = 100 * 1024 * 1024 // 100MB
	MaxFileSize    = maxFileSize
)

// Config holds the application configuration
type Config struct {
	// API settings
	APIKey      string
	APIEndpoint string

	// Project settings
	Project     string
	Environment string
	Language    string

	// Git information
	Branch string
	Commit string

	// Retry settings
	RetryMax       int
	RetryBaseDelay time.Duration
	RetryMaxDelay  time.Duration

	// Request settings
	Timeout time.Duration

	// Output settings
	Verbose bool
	Quiet   bool
	DryRun  bool
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		APIKey:         "",
		APIEndpoint:    "https://api.qualflare.com/api/v1/collect",
		Environment:    "development",
		Language:       "en-US",
		Branch:         "",
		Commit:         "",
		RetryMax:       3,
		RetryBaseDelay: 1 * time.Second,
		RetryMaxDelay:  30 * time.Second,
		Timeout:        30 * time.Second,
		Verbose:        false,
		Quiet:          false,
		DryRun:         false,
	}
}

// NewConfig creates a new configuration instance with environment variable overrides
func NewConfig() *Config {
	cfg := DefaultConfig()
	cfg.LoadFromEnv()
	return cfg
}

// LoadFromEnv loads configuration from environment variables
func (c *Config) LoadFromEnv() {
	if v := os.Getenv("QF_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("QF_API_ENDPOINT"); v != "" {
		c.APIEndpoint = v
	}
	if v := os.Getenv("QF_ENVIRONMENT"); v != "" {
		c.Environment = v
	}
	if v := os.Getenv("QF_LANGUAGE"); v != "" {
		c.Language = v
	}

	// Git information (common CI environment variables)
	c.Branch = getFirstEnv("QF_BRANCH", "GIT_BRANCH", "GITHUB_REF_NAME", "CI_COMMIT_REF_NAME", "BITBUCKET_BRANCH")
	c.Commit = getFirstEnv("QF_COMMIT", "GIT_COMMIT", "GITHUB_SHA", "CI_COMMIT_SHA", "BITBUCKET_COMMIT")

	// Retry settings
	if v := os.Getenv("QF_RETRY_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.RetryMax = n
		}
	}
	if v := os.Getenv("QF_RETRY_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.RetryBaseDelay = d
		}
	}
	if v := os.Getenv("QF_RETRY_MAX_DELAY"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.RetryMaxDelay = d
		}
	}

	// Request settings
	if v := os.Getenv("QF_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.Timeout = d
		}
	}

	// Output settings
	if v := os.Getenv("QF_VERBOSE"); v == "true" || v == "1" {
		c.Verbose = true
	}
	if v := os.Getenv("QF_QUIET"); v == "true" || v == "1" {
		c.Quiet = true
	}
}

// SetAPIKey sets the API key
func (c *Config) SetAPIKey(key string) {
	if key != "" {
		c.APIKey = key
	}
}

// SetAPIEndpoint sets the API endpoint
func (c *Config) SetAPIEndpoint(endpoint string) {
	if endpoint != "" {
		c.APIEndpoint = endpoint
	}
}

// SetProject sets the project name
func (c *Config) SetProject(project string) {
	if project != "" {
		c.Project = project
	}
}

// SetEnvironment sets the environment
func (c *Config) SetEnvironment(env string) {
	if env != "" {
		c.Environment = env
	}
}

// SetLanguage sets the language
func (c *Config) SetLanguage(language string) {
	if language != "" {
		c.Language = language
	}
}

// SetBranch sets the git branch
func (c *Config) SetBranch(branch string) {
	if branch != "" {
		c.Branch = branch
	}
}

// SetCommit sets the git commit
func (c *Config) SetCommit(commit string) {
	if commit != "" {
		c.Commit = commit
	}
}

// SetTimeout sets the request timeout
func (c *Config) SetTimeout(timeout time.Duration) {
	if timeout > 0 {
		c.Timeout = timeout
	}
}

// SetVerbose sets verbose mode
func (c *Config) SetVerbose(verbose bool) {
	c.Verbose = verbose
}

// SetQuiet sets quiet mode
func (c *Config) SetQuiet(quiet bool) {
	c.Quiet = quiet
}

// SetDryRun sets dry run mode
func (c *Config) SetDryRun(dryRun bool) {
	c.DryRun = dryRun
}

// GetAPIKey returns the API key
func (c *Config) GetAPIKey() string {
	return c.APIKey
}

// GetAPIEndpoint returns the API endpoint
func (c *Config) GetAPIEndpoint() string {
	return c.APIEndpoint
}

// GetProject returns the project name
func (c *Config) GetProject() string {
	return c.Project
}

// GetEnvironment returns the environment
func (c *Config) GetEnvironment() string {
	return c.Environment
}

// GetLanguage returns the language
func (c *Config) GetLanguage() string {
	return c.Language
}

// GetBranch returns the git branch
func (c *Config) GetBranch() string {
	return c.Branch
}

// GetCommit returns the git commit
func (c *Config) GetCommit() string {
	return c.Commit
}

// GetRetryConfig returns retry configuration
func (c *Config) GetRetryConfig() (max int, baseDelay, maxDelay time.Duration) {
	return c.RetryMax, c.RetryBaseDelay, c.RetryMaxDelay
}

// GetTimeout returns the request timeout
func (c *Config) GetTimeout() time.Duration {
	return c.Timeout
}

// IsVerbose returns whether verbose mode is enabled
func (c *Config) IsVerbose() bool {
	return c.Verbose
}

// IsQuiet returns whether quiet mode is enabled
func (c *Config) IsQuiet() bool {
	return c.Quiet
}

// IsDryRun returns whether dry run mode is enabled
func (c *Config) IsDryRun() bool {
	return c.DryRun
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if !c.DryRun && c.APIKey == "" {
		return &ValidationError{
			Field:   "api_key",
			Message: "API key is required. Set it via --api-key flag or QF_API_KEY environment variable.",
		}
	}
	if c.APIEndpoint == "" {
		return &ValidationError{
			Field:   "api_endpoint",
			Message: "API endpoint is required.",
		}
	}
	if _, err := url.ParseRequestURI(c.APIEndpoint); err != nil {
		return &ValidationError{
			Field:   "api_endpoint",
			Message: fmt.Sprintf("API endpoint is not a valid URL: %s", c.APIEndpoint),
		}
	}
	if c.RetryMax > maxRetryCount {
		c.RetryMax = maxRetryCount
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.RetryBaseDelay <= 0 {
		c.RetryBaseDelay = 1 * time.Second
	}
	if c.RetryMaxDelay <= 0 {
		c.RetryMaxDelay = 30 * time.Second
	}
	return nil
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// getFirstEnv returns the first non-empty environment variable value
func getFirstEnv(keys ...string) string {
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}
