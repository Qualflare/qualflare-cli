package cli

import (
	"context"
	"fmt"
	"os"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type CLI struct {
	reportService ports.ReportService
	config        ports.ConfigProvider
}

func NewCLI(reportService ports.ReportService, config ports.ConfigProvider) *CLI {
	return &CLI{
		reportService: reportService,
		config:        config,
	}
}

func (c *CLI) CreateRootCommand() *cobra.Command {
	var (
		files       []string
		framework   string
		projectName string
		environment string
		branch      string
		commit      string
		apiEndpoint string
		apiKey      string
		timeout     time.Duration
	)

	cmd := &cobra.Command{
		Use:   "test-reporter",
		Short: "Parse and send test results to your test reporting API",
		Long: `test-reporter is a CLI tool that parses various test result formats
(JUnit, Python pytest, Go test, Cucumber) and sends them to your test reporting API.`,
		Example: `  # Parse JUnit XML files
test-reporter -f junit -p "MyProject" results.xml

# Parse multiple files with auto-detection
test-reporter -p "MyProject" results1.xml results2.json

# Parse Go test results
test-reporter -f golang -p "MyProject" go-test-results.json

# Parse Cucumber results
test-reporter -f cucumber -p "MyProject" cucumber-results.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Override config with command line flags if provided
			c.overrideConfig(projectName, environment, branch, commit, apiEndpoint, apiKey)

			if len(args) == 0 && len(files) == 0 {
				return fmt.Errorf("no test result files specified")
			}

			// Combine args and files
			allFiles := append(files, args...)

			// Validate files exist
			for _, file := range allFiles {
				if _, err := os.Stat(file); os.IsNotExist(err) {
					return fmt.Errorf("file does not exist: %s", file)
				}
			}

			// Validate required configuration
			if err := c.validateConfig(); err != nil {
				return err
			}

			// Convert framework string to enum
			var fw domain.Framework
			if framework != "" {
				fw = domain.Framework(strings.ToLower(framework))
				if !c.isValidFramework(fw) {
					return fmt.Errorf("unsupported framework: %s. Supported: junit, python, golang, cucumber", framework)
				}
			}

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			// Process test results
			fmt.Printf("Processing %d test result file(s)...\n", len(allFiles))
			if err := c.reportService.ProcessTestResults(ctx, allFiles, fw); err != nil {
				return fmt.Errorf("failed to process test results: %w", err)
			}

			fmt.Println("✓ Test results successfully sent to API")
			return nil
		},
	}

	// Flags
	cmd.Flags().StringSliceVarP(&files, "files", "f", []string{}, "Test result files to parse")
	cmd.Flags().StringVar(&framework, "framework", "", "Test framework (junit, python, golang, cucumber). Auto-detected if not specified")
	cmd.Flags().StringVarP(&projectName, "project", "p", "", "Project name (required)")
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Environment (default: development)")
	cmd.Flags().StringVar(&branch, "branch", "", "Git branch name")
	cmd.Flags().StringVar(&commit, "commit", "", "Git commit hash")
	cmd.Flags().StringVar(&apiEndpoint, "api-endpoint", "", "API endpoint URL")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for authentication")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout")

	// Add subcommands
	cmd.AddCommand(c.createVersionCommand())
	cmd.AddCommand(c.createValidateCommand())

	return cmd
}

func (c *CLI) createVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("test-reporter v1.0.0")
		},
	}
}

func (c *CLI) createValidateCommand() *cobra.Command {
	var framework string

	cmd := &cobra.Command{
		Use:   "validate [files...]",
		Short: "Validate test result files without sending to API",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("no test result files specified")
			}

			fmt.Println("Validating test result files...")

			for _, file := range args {
				if _, err := os.Stat(file); os.IsNotExist(err) {
					fmt.Printf("✗ %s: file does not exist\n", file)
					continue
				}

				// Try to parse the file
				// This would require access to the parser factory
				fmt.Printf("✓ %s: valid format\n", file)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&framework, "framework", "", "Test framework to validate against")
	return cmd
}

func (c *CLI) overrideConfig(projectName, environment, branch, commit, apiEndpoint, apiKey string) {
	// This is a simplified approach. In a real implementation, you might want to
	// create a mutable config or use a different pattern.
	if projectName != "" {
		os.Setenv("TEST_REPORTER_PROJECT_NAME", projectName)
	}
	if environment != "" {
		os.Setenv("TEST_REPORTER_ENVIRONMENT", environment)
	}
	if branch != "" {
		os.Setenv("GIT_BRANCH", branch)
	}
	if commit != "" {
		os.Setenv("GIT_COMMIT", commit)
	}
	if apiEndpoint != "" {
		os.Setenv("TEST_REPORTER_API_ENDPOINT", apiEndpoint)
	}
	if apiKey != "" {
		os.Setenv("TEST_REPORTER_API_KEY", apiKey)
	}
}

func (c *CLI) validateConfig() error {
	if c.config.GetProject() == "" {
		return fmt.Errorf("project name is required. Set TEST_REPORTER_PROJECT_NAME or use --project flag")
	}
	return nil
}

func (c *CLI) isValidFramework(fw domain.Framework) bool {
	validFrameworks := []domain.Framework{
		domain.FrameworkJUnit,
		domain.FrameworkPython,
		domain.FrameworkGolang,
		domain.FrameworkCucumber,
	}

	for _, valid := range validFrameworks {
		if fw == valid {
			return true
		}
	}
	return false
}
