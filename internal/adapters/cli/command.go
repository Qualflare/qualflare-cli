package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"qualflare-cli/internal/version"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// CLI handles command-line interface operations
type CLI struct {
	reportService ports.ReportService
	config        *config.Config
	parserFactory ports.ParserFactory
}

// NewCLI creates a new CLI instance
func NewCLI(reportService ports.ReportService, cfg *config.Config, parserFactory ports.ParserFactory) *CLI {
	return &CLI{
		reportService: reportService,
		config:        cfg,
		parserFactory: parserFactory,
	}
}

// CreateRootCommand creates the root command
func (c *CLI) CreateRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qf",
		Short: "Qualflare CLI - Upload test results to Qualflare",
		Long: `qf is a CLI tool that parses various test result formats and uploads them to Qualflare.

Supported frameworks:
  Unit Testing:    junit, python, golang, jest, mocha, rspec, phpunit
  BDD:             cucumber, karate
  UI/E2E Testing:  playwright, cypress, selenium, testcafe
  API Testing:     newman, k6
  Security:        zap, trivy, snyk, sonarqube`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global flags
	cmd.PersistentFlags().BoolVarP(&c.config.Verbose, "verbose", "v", false, "Enable verbose output")
	cmd.PersistentFlags().BoolVarP(&c.config.Quiet, "quiet", "q", false, "Suppress non-error output")

	// Add subcommands
	cmd.AddCommand(c.createUploadCommand())
	cmd.AddCommand(c.createValidateCommand())
	cmd.AddCommand(c.createVersionCommand())
	cmd.AddCommand(c.createListFormatsCommand())

	return cmd
}

// createUploadCommand creates the upload subcommand
func (c *CLI) createUploadCommand() *cobra.Command {
	var (
		format      string
		project     string
		environment string
		language    string
		branch      string
		commit      string
		apiEndpoint string
		apiKey      string
		timeout     time.Duration
		dryRun      bool
		output      string
	)

	cmd := &cobra.Command{
		Use:   "upload [files...]",
		Short: "Upload test results to Qualflare",
		Long: `Parse test result files and upload them to the Qualflare API.

Files can be specified as arguments or using glob patterns.
The format is auto-detected if not specified.`,
		Example: `  # Upload JUnit XML files
  qf upload results.xml --project my-app --format junit

  # Auto-detect format
  qf upload playwright-results.json --project my-app

  # Upload multiple files
  qf upload *.xml --project my-app --format junit

  # Dry run (parse and show what would be sent)
  qf upload results.xml --project my-app --dry-run

  # Output parsed results as JSON
  qf upload results.xml --project my-app --dry-run --output json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runUpload(cmd.Context(), args, uploadOptions{
				format:      format,
				environment: environment,
				language:    language,
				branch:      branch,
				commit:      commit,
				apiEndpoint: apiEndpoint,
				apiKey:      apiKey,
				timeout:     timeout,
				dryRun:      dryRun,
				output:      output,
			})
		},
	}

	// Flags
	cmd.Flags().StringVarP(&format, "format", "f", "", "Test framework format (auto-detected if not specified)")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name (optional, defaults to API key project)")
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Environment name")
	cmd.Flags().StringVar(&language, "lang", "en-US", "Language/culture (BCP 47 format, e.g., en-US, de-DE)")
	cmd.Flags().StringVar(&branch, "branch", "", "Git branch name")
	cmd.Flags().StringVar(&commit, "commit", "", "Git commit hash")
	cmd.Flags().StringVar(&apiEndpoint, "api-endpoint", "", "API endpoint URL")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for authentication")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse files without uploading")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format for dry-run (json)")

	return cmd
}

type uploadOptions struct {
	format      string
	environment string
	language    string
	branch      string
	commit      string
	apiEndpoint string
	apiKey      string
	timeout     time.Duration
	dryRun      bool
	output      string
}

func (c *CLI) runUpload(ctx context.Context, files []string, opts uploadOptions) error {
	// Apply command line overrides
	c.config.SetEnvironment(opts.environment)
	c.config.SetLanguage(opts.language)
	c.config.SetBranch(opts.branch)
	c.config.SetCommit(opts.commit)
	c.config.SetAPIEndpoint(opts.apiEndpoint)
	c.config.SetAPIKey(opts.apiKey)
	c.config.SetTimeout(opts.timeout)
	c.config.SetDryRun(opts.dryRun)

	// Validate configuration
	if err := c.config.Validate(); err != nil {
		return err
	}

	// Validate files exist
	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", file)
		}
	}

	// Convert format string to framework
	var framework domain.Framework
	if opts.format != "" {
		framework = domain.Framework(strings.ToLower(opts.format))
		if !framework.IsValid() {
			return fmt.Errorf("unsupported format: %s. Use 'qf list-formats' to see supported formats", opts.format)
		}
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	if !c.config.IsQuiet() {
		c.printInfo("Processing %d test result file(s)...", len(files))
	}

	// For dry run with output, parse and display
	if opts.dryRun && opts.output == "json" {
		report, err := c.reportService.ParseTestResults(ctx, files, framework)
		if err != nil {
			return fmt.Errorf("failed to parse test results: %w", err)
		}

		jsonData, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal report: %w", err)
		}

		fmt.Println(string(jsonData))
		return nil
	}

	// Process test results
	if err := c.reportService.ProcessTestResults(ctx, files, framework); err != nil {
		return fmt.Errorf("failed to process test results: %w", err)
	}

	if !c.config.IsQuiet() {
		if opts.dryRun {
			c.printSuccess("Test results parsed successfully (dry run)")
		} else {
			c.printSuccess("Test results uploaded successfully")
		}
	}

	return nil
}

// createValidateCommand creates the validate subcommand
func (c *CLI) createValidateCommand() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "validate [files...]",
		Short: "Validate test result files without uploading",
		Long:  `Validate that test result files can be parsed correctly without uploading them.`,
		Example: `  # Validate a single file
  qf validate results.xml

  # Validate with specific format
  qf validate results.json --format playwright

  # Validate multiple files
  qf validate *.xml`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runValidate(cmd.Context(), args, format)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "", "Test framework format to validate against")

	return cmd
}

func (c *CLI) runValidate(ctx context.Context, files []string, formatStr string) error {
	var framework domain.Framework
	if formatStr != "" {
		framework = domain.Framework(strings.ToLower(formatStr))
		if !framework.IsValid() {
			return fmt.Errorf("unsupported format: %s", formatStr)
		}
	}

	c.printInfo("Validating %d test result file(s)...", len(files))

	results, err := c.reportService.ValidateFiles(ctx, files, framework)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	hasErrors := false
	for _, result := range results {
		if result.Valid {
			c.printSuccess("%s: valid (%s, %d tests)", result.FilePath, result.Framework, result.TestCount)
		} else {
			c.printError("%s: invalid - %s", result.FilePath, result.Error)
			hasErrors = true
		}
	}

	if hasErrors {
		return fmt.Errorf("one or more files failed validation")
	}

	return nil
}

// createVersionCommand creates the version subcommand
func (c *CLI) createVersionCommand() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			info := version.Get()
			if short {
				fmt.Println(info.Short())
			} else {
				fmt.Println(info.String())
			}
		},
	}

	cmd.Flags().BoolVarP(&short, "short", "s", false, "Print short version")

	return cmd
}

// createListFormatsCommand creates the list-formats subcommand
func (c *CLI) createListFormatsCommand() *cobra.Command {
	var category string

	cmd := &cobra.Command{
		Use:     "list-formats",
		Aliases: []string{"formats", "lf"},
		Short:   "List supported test result formats",
		Run: func(cmd *cobra.Command, args []string) {
			c.printFormats(category)
		},
	}

	cmd.Flags().StringVarP(&category, "category", "c", "", "Filter by category (unit, bdd, e2e, api, security)")

	return cmd
}

func (c *CLI) printFormats(categoryFilter string) {
	categories := map[domain.FrameworkCategory][]domain.Framework{
		domain.CategoryUnitTest: {},
		domain.CategoryBDD:      {},
		domain.CategoryE2E:      {},
		domain.CategoryAPI:      {},
		domain.CategorySecurity: {},
	}

	for _, fw := range domain.AllFrameworks() {
		cat := fw.GetCategory()
		categories[cat] = append(categories[cat], fw)
	}

	categoryNames := map[domain.FrameworkCategory]string{
		domain.CategoryUnitTest: "Unit Testing",
		domain.CategoryBDD:      "BDD / Behavior-Driven",
		domain.CategoryE2E:      "UI / E2E Testing",
		domain.CategoryAPI:      "API Testing",
		domain.CategorySecurity: "Security Testing",
	}

	order := []domain.FrameworkCategory{
		domain.CategoryUnitTest,
		domain.CategoryBDD,
		domain.CategoryE2E,
		domain.CategoryAPI,
		domain.CategorySecurity,
	}

	for _, cat := range order {
		if categoryFilter != "" && string(cat) != categoryFilter {
			continue
		}

		frameworks := categories[cat]
		if len(frameworks) == 0 {
			continue
		}

		fmt.Printf("\n%s:\n", categoryNames[cat])
		for _, fw := range frameworks {
			fmt.Printf("  - %s\n", fw)
		}
	}
	fmt.Println()
}

// Output helpers
func (c *CLI) printInfo(format string, args ...interface{}) {
	if !c.config.IsQuiet() {
		fmt.Printf(format+"\n", args...)
	}
}

func (c *CLI) printSuccess(format string, args ...interface{}) {
	if !c.config.IsQuiet() {
		fmt.Printf("OK "+format+"\n", args...)
	}
}

func (c *CLI) printError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERR "+format+"\n", args...)
}

func (c *CLI) printVerbose(format string, args ...interface{}) {
	if c.config.IsVerbose() {
		fmt.Printf(format+"\n", args...)
	}
}
