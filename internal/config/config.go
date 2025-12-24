package config

import (
	"os"
)

// Config holds the application configuration
type Config struct {
	APIKey      string
	ProjectName string
	Environment string
	Branch      string
	Commit      string
}

// NewConfig creates a new configuration instance
func NewConfig() *Config {
	return &Config{
		APIKey:      getEnvOrDefault("QF_API_KEY", ""),
		ProjectName: getEnvOrDefault("QF_PROJECT", "project"),
		Environment: getEnvOrDefault("QF_ENVIRONMENT", "development"),
		Branch:      getEnvOrDefault("GIT_BRANCH", ""),
		Commit:      getEnvOrDefault("GIT_COMMIT", ""),
	}
}

func (c *Config) GetAPIKey() string      { return c.APIKey }
func (c *Config) GetProject() string     { return c.ProjectName }
func (c *Config) GetEnvironment() string { return c.Environment }
func (c *Config) GetBranch() string      { return c.Branch }
func (c *Config) GetCommit() string      { return c.Commit }

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
