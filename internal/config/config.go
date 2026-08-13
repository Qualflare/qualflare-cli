// Package config holds runtime configuration and loads it from env vars.
package config

import (
	"context"
	"os"
	"strconv"
	"time"

	"qualflare-cli/internal/git"
	"qualflare-cli/internal/version"
)

const (
	maxRetryCount = 10
	MaxFileSize   = 100 * 1024 * 1024 // 100 MB
)

var apiEndpoint = "https://api.qualflare.com"

// Config holds the application configuration
type Config struct {
	// API settings
	APIKey string

	// Project settings
	Environment string
	Language    string
	Platform    string
	Milestone   int64

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
	Debug   bool
	// Shard is mechanism B (opt-in, explicit) for populating Case.ShardIndex:
	// when set with 2+ input files, every case from file i is tagged
	// shard_index = i (0-based), overwriting any value another mechanism set.
	Shard bool
	// NoCaptureOutput suppresses uploading captured stdout/stderr (JUnit
	// system-out/system-err) — the streams most likely to contain secrets an
	// environment printed during a test run. Test status and failure messages are
	// still uploaded; only the bulk captured output is dropped (SEC-04).
	NoCaptureOutput bool
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		APIKey:         "",
		Environment:    "development",
		Language:       "en-US",
		Platform:       "api", // backward-compatible default; override with --platform / QF_PLATFORM
		Branch:         "",
		Commit:         "",
		RetryMax:       3,
		RetryBaseDelay: 1 * time.Second,
		RetryMaxDelay:  30 * time.Second,
		// 120s (not 30s): the server's own /collect DB budget is ~30s, so a 30s client
		// deadline had zero headroom — a large upload that legitimately took ~30s
		// server-side would trip the client timeout at the same moment, failing an
		// upload the server was about to accept (BUG-27). 120s leaves room for retries.
		Timeout: 120 * time.Second,
		Verbose: false,
		Quiet:   false,
		DryRun:  false,
	}
}

// NewConfig creates a new configuration instance with environment variable overrides
func NewConfig() *Config {
	cfg := DefaultConfig()
	cfg.LoadFromEnv()
	return cfg
}

// LoadFromEnv loads configuration from environment variables.
// Note: QF_API_KEY is intentionally NOT read here — tokens come from the
// auth store via `qf login <identifier> <token>`.
func (c *Config) LoadFromEnv() {
	envString(&c.Environment, "QF_ENVIRONMENT")
	envString(&c.Language, "QF_LANGUAGE")
	envString(&c.Platform, "QF_PLATFORM")
	// A milestone sequence number is 1-based, so 0 and negatives are rejected.
	envInt64(&c.Milestone, "QF_MILESTONE", 1)

	// Git information from CI env vars only. Local git-subprocess detection is
	// deferred to DetectGit() so it runs once, for `collect` — not on every
	// invocation (version/help/login/logout all forked two `git` processes) (BUG-39).
	c.Branch = getFirstEnv("QF_BRANCH", "GIT_BRANCH", "GITHUB_REF_NAME", "CI_COMMIT_REF_NAME", "BITBUCKET_BRANCH")
	c.Commit = getFirstEnv("QF_COMMIT", "GIT_COMMIT", "GITHUB_SHA", "CI_COMMIT_SHA", "BITBUCKET_COMMIT")

	// Retry settings. 0 retries is a legitimate choice, unlike a 0 milestone.
	envInt(&c.RetryMax, "QF_RETRY_MAX", 0)
	envDuration(&c.RetryBaseDelay, "QF_RETRY_DELAY")
	envDuration(&c.RetryMaxDelay, "QF_RETRY_MAX_DELAY")

	// Request settings
	envDuration(&c.Timeout, "QF_TIMEOUT")

	// Output settings
	envBool(&c.Verbose, "QF_VERBOSE")
	envBool(&c.Quiet, "QF_QUIET")
	envBool(&c.Debug, "QF_DEBUG")
	envBool(&c.NoCaptureOutput, "QF_NO_CAPTURE_OUTPUT")
}

// The env* helpers share one rule: an unset, empty, or unparseable variable leaves the
// destination alone, so a bad value degrades to the existing default rather than
// zeroing a setting or failing the whole invocation.

func envString(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

// envInt64 assigns only when the value parses and is at least minValue.
func envInt64(dst *int64, key string, minValue int64) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= minValue {
			*dst = n
		}
	}
}

// envInt assigns only when the value parses and is at least minValue.
func envInt(dst *int, key string, minValue int) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= minValue {
			*dst = n
		}
	}
}

func envDuration(dst *time.Duration, key string) {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*dst = d
		}
	}
}

// envBool only ever turns a flag on: these mirror boolean CLI flags that default to
// false, so an absent or falsey variable must not clear a value set elsewhere.
func envBool(dst *bool, key string) {
	if v := os.Getenv(key); v == "true" || v == "1" {
		*dst = true
	}
}

// DetectGit fills Branch/Commit from the local git repo when CI env vars did not
// already supply them. It shells out to `git`, so it is invoked explicitly by the
// `collect` path rather than on every CLI invocation (BUG-39).
func (c *Config) DetectGit() {
	if c.Branch != "" && c.Commit != "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if c.Branch == "" {
		c.Branch = git.DetectBranch(ctx)
	}
	if c.Commit == "" {
		c.Commit = git.DetectCommit(ctx)
	}
}

// SetAPIKey sets the API key
func (c *Config) SetAPIKey(key string) {
	if key != "" {
		c.APIKey = key
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

// SetPlatform sets the platform (skips empty so an unset flag doesn't clobber
// the env var / default, matching SetEnvironment).
func (c *Config) SetPlatform(platform string) {
	if platform != "" {
		c.Platform = platform
	}
}

// SetMilestone sets the milestone sequence number (skips 0 = unset).
func (c *Config) SetMilestone(milestone int64) {
	if milestone > 0 {
		c.Milestone = milestone
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

// SetShard sets shard mode
func (c *Config) SetShard(shard bool) {
	c.Shard = shard
}

// GetAPIKey returns the API key
func (c *Config) GetAPIKey() string {
	return c.APIKey
}

// GetAPIEndpoint returns the API endpoint
func (c *Config) GetAPIEndpoint() string {
	return apiEndpoint
}

// GetEnvironment returns the environment
func (c *Config) GetEnvironment() string {
	return c.Environment
}

// GetLanguage returns the language
func (c *Config) GetLanguage() string {
	return c.Language
}

// GetPlatform returns the platform (android/ios/desktop/web/api).
func (c *Config) GetPlatform() string {
	return c.Platform
}

// GetMilestone returns the milestone sequence number (0 = none).
func (c *Config) GetMilestone() int64 {
	return c.Milestone
}

// GetMaxFileSize returns the per-file upload size cap. Exposed via the provider
// so the core service does not import this concrete config package (ARCH-02).
func (c *Config) GetMaxFileSize() int64 {
	return MaxFileSize
}

// GetCLIVersion returns the build version string.
func (c *Config) GetCLIVersion() string {
	return version.Version
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
func (c *Config) GetRetryConfig() (retryMax int, baseDelay, maxDelay time.Duration) {
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

// IsDebug returns whether debug (full request/response logging) is enabled.
func (c *Config) IsDebug() bool {
	return c.Debug
}

// IsNoCaptureOutput returns whether captured stdout/stderr (system-out/system-err)
// must be dropped before the report is sent (SEC-04).
func (c *Config) IsNoCaptureOutput() bool {
	return c.NoCaptureOutput
}

// IsDryRun returns whether dry run mode is enabled
func (c *Config) IsDryRun() bool {
	return c.DryRun
}

// IsShard returns whether shard mode is enabled
func (c *Config) IsShard() bool {
	return c.Shard
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if !c.DryRun && c.APIKey == "" {
		return &ValidationError{
			Field:   "api_key",
			Message: "no token loaded; run 'qf login <identifier> <token>' first",
		}
	}
	if c.RetryMax > maxRetryCount {
		c.RetryMax = maxRetryCount
	}
	if c.Timeout <= 0 {
		c.Timeout = 120 * time.Second
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
