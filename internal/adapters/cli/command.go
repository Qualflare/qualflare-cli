// Package cli wires together the Cobra command tree and delegates to core services.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"qualflare-cli/internal/version"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const apiV1 = "/api/v1"

func warnLegacyAPIKey() {
	if os.Getenv("QF_API_KEY") != "" {
		fmt.Fprintln(os.Stderr, "Note: QF_API_KEY is no longer read. Run 'qf login <identifier> $QF_API_KEY' to migrate.")
	}
}

// CLI handles command-line interface operations
type CLI struct {
	reportService ports.ReportService
	config        *config.Config
	parserFactory ports.ParserFactory
	apiClient     ports.APIClient
	store         *auth.Store
}

// NewCLI creates a new CLI instance
func NewCLI(reportService ports.ReportService, cfg *config.Config, parserFactory ports.ParserFactory, apiClient ports.APIClient, store *auth.Store) *CLI {
	return &CLI{
		reportService: reportService,
		config:        cfg,
		parserFactory: parserFactory,
		apiClient:     apiClient,
		store:         store,
	}
}

// CreateRootCommand creates the root command
func (c *CLI) CreateRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qf",
		Short: "Qualflare CLI - Collect test results for Qualflare",
		Long: `qf is a CLI tool for Qualflare — parse test results and manage test data.

Authentication:
  login            Save credentials for a project (qf login <id> <token>)
  logout           Remove saved credentials
  projects         List locally saved project identifiers

Project-scoped commands (run as `+"`qf <identifier> <command>`"+`):
  collect          Collect test results and send to Qualflare
  validate         Validate test result files
  suites / suite         List and view test suites
  cases / case           List and view test cases and steps
  plans / plan           List and view test plans
  launches / launch      List and view test launches
  defects / defect       List and view defects
  clusters / cluster     List and view failure clusters
  milestones / milestone List and view milestones

Other:
  list-formats     List supported test frameworks
  version          Print version information

Supported frameworks:
  Generic (JUnit): junit
  Unit Testing:    python, golang, jest, mocha, rspec, phpunit, testng
  BDD:             cucumber, karate
  UI/E2E/Mobile:   playwright, cypress, selenium, testcafe, maestro, xctest, espresso
  API Testing:     newman, k6
  Security:        zap, trivy, snyk, sonarqube`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && len(c.store.List()) == 0 {
				fmt.Println("No projects configured. Run 'qf login <identifier> <token>' to get started.")
				return nil
			}
			return cmd.Help()
		},
	}

	// Global flags. Seed the defaults from the already-loaded config (which read
	// QF_VERBOSE/QF_QUIET) so pflag's registration does not zero the env-derived
	// value before any command runs (BUG-02).
	cmd.PersistentFlags().BoolVarP(&c.config.Verbose, "verbose", "v", c.config.Verbose, "Enable verbose output")
	cmd.PersistentFlags().BoolVarP(&c.config.Quiet, "quiet", "q", c.config.Quiet, "Suppress non-error output")

	// Flat (auth-less) subcommands
	cmd.AddCommand(c.createLoginCommand())
	cmd.AddCommand(c.createLogoutCommand())
	cmd.AddCommand(c.createProjectsCommand())
	cmd.AddCommand(c.createVersionCommand())
	cmd.AddCommand(c.createListFormatsCommand())

	// One identifier-scoped subtree per saved project
	for _, id := range c.store.List() {
		token, _ := c.store.Get(id)
		cmd.AddCommand(c.createIdentifierCommand(id, token))
	}

	return cmd
}

// createIdentifierCommand builds a parent subcommand for one saved identifier
// and attaches a fresh authed subtree under it. PersistentPreRunE injects the
// token into config so the http client middleware picks it up at request time.
func (c *CLI) createIdentifierCommand(identifier, token string) *cobra.Command {
	parent := &cobra.Command{
		Use:           identifier,
		Short:         "Commands scoped to project " + identifier,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			c.config.SetAPIKey(token)
			return nil
		},
	}
	for _, sub := range c.buildAuthedSubtree() {
		parent.AddCommand(sub)
	}
	return parent
}

// buildAuthedSubtree returns a fresh slice of authed subcommands. Each call
// produces new *cobra.Command instances; do NOT share these pointers across
// parents because cobra mutates the parent reference on AddCommand.
func (c *CLI) buildAuthedSubtree() []*cobra.Command {
	return []*cobra.Command{
		c.createCollectCommand(),
		c.createValidateCommand(),
		c.createSuitesCommand(),
		c.createSuiteCommand(),
		c.createCasesCommand(),
		c.createCaseCommand(),
		c.createPlansCommand(),
		c.createPlanCommand(),
		c.createLaunchesCommand(),
		c.createLaunchCommand(),
		c.createDefectsCommand(),
		c.createDefectCommand(),
		c.createClustersCommand(),
		c.createClusterCommand(),
		c.createMilestonesCommand(),
		c.createMilestoneCommand(),
	}
}

// createCollectCommand creates the collect subcommand
func (c *CLI) createCollectCommand() *cobra.Command {
	var (
		format      string
		environment string
		language    string
		platform    string
		milestone   int64
		branch      string
		commit      string
		timeout     time.Duration
		dryRun      bool
		output      string
	)

	cmd := &cobra.Command{
		Use:   "collect [files...]",
		Short: "Collect test results for Qualflare",
		Long: `Parse test result files and send them to the Qualflare API.

Files can be specified as arguments or using glob patterns.
The format is auto-detected if not specified.`,
		Example: `  # Collect JUnit XML files for project 'my-app'
  qf my-app collect results.xml --format junit

  # Auto-detect format
  qf my-app collect playwright-results.json

  # Collect multiple files
  qf my-app collect *.xml --format junit

  # Dry run (parse and show what would be sent)
  qf my-app collect results.xml --dry-run

  # Output parsed results as JSON
  qf my-app collect results.xml --dry-run --output json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runCollect(cmd.Context(), args, collectOptions{
				format:      format,
				environment: environment,
				language:    language,
				platform:    platform,
				milestone:   milestone,
				branch:      branch,
				commit:      commit,
				timeout:     timeout,
				dryRun:      dryRun,
				output:      output,
			})
		},
	}

	// Flags
	// environment/language default to "" so an explicit flag overrides, but an
	// unset flag falls through to QF_ENVIRONMENT/QF_LANGUAGE (or the built-in
	// DefaultConfig default) instead of a non-empty flag default always winning
	// over the env var (CLI-H1). SetEnvironment/SetLanguage skip on "".
	cmd.Flags().StringVarP(&format, "format", "f", "", "Test framework format (auto-detected if not specified)")
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Environment name (default: $QF_ENVIRONMENT or 'development')")
	cmd.Flags().StringVar(&language, "lang", "", "Language/culture, BCP 47 (default: $QF_LANGUAGE or 'en-US')")
	cmd.Flags().StringVar(&platform, "platform", "", "Platform: android|ios|desktop|web|api (default: $QF_PLATFORM or 'api')")
	cmd.Flags().Int64Var(&milestone, "milestone", 0, "Milestone sequence number to link this launch to (or $QF_MILESTONE)")
	cmd.Flags().StringVar(&branch, "branch", "", "Git branch name")
	cmd.Flags().StringVar(&commit, "commit", "", "Git commit hash")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Parse files without sending")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format for dry-run (json)")

	return cmd
}

type collectOptions struct {
	format      string
	environment string
	language    string
	platform    string
	milestone   int64
	branch      string
	commit      string
	timeout     time.Duration
	dryRun      bool
	output      string
}

// validPlatforms mirrors the server's launch platform enum
// (oneof=android ios desktop web api).
var validPlatforms = map[string]struct{}{
	"android": {}, "ios": {}, "desktop": {}, "web": {}, "api": {},
}

func (c *CLI) runCollect(ctx context.Context, files []string, opts collectOptions) error {
	warnLegacyAPIKey()
	// Validate an explicit --platform before it reaches the server (fail fast with
	// a clear message instead of a 400 on the whole upload).
	if opts.platform != "" {
		if _, ok := validPlatforms[opts.platform]; !ok {
			return fmt.Errorf("invalid --platform %q: must be one of android, ios, desktop, web, api", opts.platform)
		}
	}
	// Apply command line overrides
	c.config.SetEnvironment(opts.environment)
	c.config.SetLanguage(opts.language)
	c.config.SetPlatform(opts.platform)
	c.config.SetMilestone(opts.milestone)
	c.config.SetBranch(opts.branch)
	c.config.SetCommit(opts.commit)
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
			c.printSuccess("Test results collected successfully")
		}
	}

	return nil
}

// createValidateCommand creates the validate subcommand
func (c *CLI) createValidateCommand() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "validate [files...]",
		Short: "Validate test result files without sending",
		Long:  `Validate that test result files can be parsed correctly without sending them.`,
		Example: `  # Validate a single file
  qf <id> validate results.xml

  # Validate with specific format
  qf <id> validate results.json --format playwright

  # Validate multiple files
  qf <id> validate *.xml`,
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

	cmd.Flags().StringVarP(&category, "category", "c", "", "Filter by category (generic, unit, bdd, e2e, api, security)")

	return cmd
}

func (c *CLI) printFormats(categoryFilter string) {
	categories := map[domain.FrameworkCategory][]domain.Framework{
		domain.CategoryGeneric:  {},
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
		domain.CategoryGeneric:  "Generic (JUnit-compatible)",
		domain.CategoryUnitTest: "Unit Testing",
		domain.CategoryBDD:      "BDD / Behavior-Driven",
		domain.CategoryE2E:      "UI / E2E / Mobile Testing",
		domain.CategoryAPI:      "API Testing",
		domain.CategorySecurity: "Security Testing",
	}

	order := []domain.FrameworkCategory{
		domain.CategoryGeneric,
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
// printInfo/printSuccess write DIAGNOSTICS, so they go to stderr — stdout is
// reserved for command data (the --output json payload, read-command JSON), which
// must stay machine-parseable when piped (BUG-03/04).
func (c *CLI) printInfo(format string, args ...interface{}) {
	if !c.config.IsQuiet() {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

func (c *CLI) printSuccess(format string, args ...interface{}) {
	if !c.config.IsQuiet() {
		fmt.Fprintf(os.Stderr, "OK "+format+"\n", args...)
	}
}

func (c *CLI) printError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERR "+format+"\n", args...)
}

