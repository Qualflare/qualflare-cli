// Package cli wires together the Cobra command tree and delegates to core services.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"qualflare-cli/internal/version"
	"sort"
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

Project-scoped commands (run as ` + "`qf <identifier> <command>`" + `):
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
  Generic (JUnit): junit, ctrf, qualflare-json
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
	cmd.PersistentFlags().BoolVar(&c.config.Debug, "debug", c.config.Debug, "Log full HTTP request/response to stderr (token redacted)")
	cmd.PersistentFlags().BoolVar(&c.config.NoCaptureOutput, "no-capture-output", c.config.NoCaptureOutput, "Do not upload captured stdout/stderr (system-out/system-err), nor any custom <property> value a test declared at case level (record_property) or, for pytest, suite level (record_testsuite_property) — keeps secrets printed or recorded during tests off the server. Structural properties a parser generates itself (file, line, shard, severity, ...) are still uploaded.")

	// Flat (auth-less) subcommands
	cmd.AddCommand(c.createLoginCommand())
	cmd.AddCommand(c.createLogoutCommand())
	cmd.AddCommand(c.createProjectsCommand())
	cmd.AddCommand(c.createVersionCommand())
	cmd.AddCommand(c.createListFormatsCommand())
	// validate parses files locally and never calls the API, so requiring a saved
	// identifier for it was wrong: `qf validate results.xml` used to fail with
	// `no identifier "validate" configured`, because cobra read the subcommand
	// name as the project. It stays in buildAuthedSubtree too, so the documented
	// `qf <id> validate` form keeps working -- createValidateCommand returns a
	// fresh *cobra.Command per call, which is what makes registering it in both
	// places safe (see buildAuthedSubtree's note on not sharing pointers).
	cmd.AddCommand(c.createValidateCommand())

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
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
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
		format          string
		environment     string
		language        string
		platform        string
		milestone       int64
		branch          string
		commit          string
		timeout         time.Duration
		dryRun          bool
		allowMixed      bool
		uploadArtifacts string
		shard           bool
		output          string
	)

	cmd := &cobra.Command{
		Use:   "collect [files...]",
		Short: "Collect test results for Qualflare",
		Long: `Parse test result files and send them to the Qualflare API.

Files can be specified as arguments, using glob patterns, or as a directory.
The format is auto-detected if not specified.`,
		Example: `  # Collect JUnit XML files for project 'my-app'
  qf my-app collect results.xml --format junit

  # Auto-detect format
  qf my-app collect playwright-results.json

  # Collect multiple files
  qf my-app collect *.xml --format junit

  # Collect (and auto-merge, if the files carry their own shardIndex) every
  # report in a directory — this is what @qualflare/cypress and
  # @qualflare/cucumberjs's outputDir produces
  qf my-app collect ./qualflare-results

  # Dry run (parse and show what would be sent)
  qf my-app collect results.xml --dry-run

  # Output parsed results as JSON
  qf my-app collect results.xml --dry-run --output json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runCollect(cmd.Context(), args, collectOptions{
				format:          format,
				environment:     environment,
				language:        language,
				platform:        platform,
				milestone:       milestone,
				branch:          branch,
				commit:          commit,
				timeout:         timeout,
				dryRun:          dryRun,
				shard:           shard,
				output:          output,
				allowMixed:      allowMixed,
				uploadArtifacts: uploadArtifacts,
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
	cmd.Flags().BoolVar(&shard, "shard", false,
		"Treat each input file as one parallel shard of the same run, numbered by argument position starting at 0 (requires 2+ files). Does not apply to a single file. WARNING: with a glob, files are numbered in lexical filename order (t1, t10, t100, t2, ...), which is NOT a stable per-shard identity — adding, renaming or losing a file shifts every later index. List the files explicitly, in shard order, when the index has to be stable across runs.")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format for dry-run (json)")
	cmd.Flags().StringVar(&uploadArtifacts, "upload-artifacts", "",
		"Comma-separated heavy artifact kinds to upload: "+strings.Join(domain.AllArtifactKinds(), ", ")+
			". Default uploads NONE — a video or Playwright trace is the largest thing in a report, so "+
			"uploading one is opt-in. Also settable via QF_UPLOAD_ARTIFACTS.")
	cmd.Flags().BoolVar(&allowMixed, "allow-mixed-runs", false,
		"Upload even when the report files come from different runs (by default this is refused, "+
			"because a stale file from an earlier run would be merged into this launch)")

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
	shard       bool
	output      string
	allowMixed  bool
	// uploadArtifacts is the raw --upload-artifacts value, validated into a
	// set by config.ParseArtifactKinds in applyCollectOptions.
	uploadArtifacts string
}

// validPlatforms mirrors the server's launch platform enum
// (oneof=android ios desktop web api).
var validPlatforms = map[string]struct{}{
	"android": {}, "ios": {}, "desktop": {}, "web": {}, "api": {},
}

// expandGlobs expands any argument containing a glob metacharacter via
// filepath.Glob, preserving order and passing literal (non-glob) args through. A
// pattern that matches nothing is an error so a mistyped glob fails loudly rather
// than silently uploading nothing (BUG-28).
func expandGlobs(patterns []string) ([]string, error) {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if !strings.ContainsAny(p, "*?[") {
			out = append(out, p)
			continue
		}
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", p, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no files match pattern %q", p)
		}
		out = append(out, matches...)
	}
	return out, nil
}

// expandDirectories expands any argument that is a directory into the *.json
// files directly inside it (non-recursive — matches the reporters' flat
// outputDir layout), preserving order and passing a non-directory argument
// through literally. A directory with no *.json files inside is an error,
// matching expandGlobs's "no matches = loud error, not a silent empty
// upload" convention (BUG-28).
func expandDirectories(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			// Let the existing verifyFilesExist give the real "does not
			// exist" error later — this function only expands directories
			// it can actually see.
			out = append(out, p)
			continue
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(p, "*.json"))
		if err != nil {
			return nil, fmt.Errorf("invalid directory %q: %w", p, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("directory %q contains no .json report files", p)
		}
		sort.Strings(matches)
		out = append(out, matches...)
	}
	return out, nil
}

// validatePlatform checks an explicit --platform before it reaches the server, so a
// typo fails fast with a clear message instead of a 400 on the whole upload. An empty
// value means "not set" and is left to the server's default.
func validatePlatform(platform string) error {
	if platform == "" {
		return nil
	}
	if _, ok := validPlatforms[platform]; !ok {
		return fmt.Errorf("invalid --platform %q: must be one of android, ios, desktop, web, api", platform)
	}
	return nil
}

// applyCollectOptions folds the command-line overrides into the config. The setters
// ignore empty/zero values, so an unset flag leaves whatever env detection supplied.
func applyCollectOptions(cfg *config.Config, opts collectOptions) error {
	// Validated before anything else in the collect path runs: a typo'd
	// --upload-artifacts=vidoe must fail with the valid list, not silently
	// upload nothing and look like the gate working as intended.
	kinds, err := config.ParseArtifactKinds(opts.uploadArtifacts, domain.AllArtifactKinds())
	if err != nil {
		return err
	}
	cfg.SetUploadArtifacts(kinds)

	cfg.SetEnvironment(opts.environment)
	cfg.SetLanguage(opts.language)
	cfg.SetPlatform(opts.platform)
	cfg.SetMilestone(opts.milestone)
	cfg.SetBranch(opts.branch)
	cfg.SetCommit(opts.commit)
	cfg.SetTimeout(opts.timeout)
	cfg.SetDryRun(opts.dryRun)
	cfg.SetShard(opts.shard)
	return nil
}

// runMeta is what one report file says about the run it belongs to.
type runMeta struct {
	runID     string
	timestamp time.Time
	modTime   time.Time
	path      string
}

// runGroups buckets report files by the `metadata.runId` their reporter wrote.
// Files without one — every report produced before reporters started stamping
// it — land under "" and are deliberately NOT treated as a distinct run, so an
// older reporter keeps working exactly as before.
func runGroups(files []string) map[string][]runMeta {
	groups := make(map[string][]runMeta)
	for _, f := range files {
		m := readRunMeta(f)
		groups[m.runID] = append(groups[m.runID], m)
	}
	return groups
}

// readRunMeta pulls metadata.runId and metadata.timestamp out of a report,
// returning zero values for anything it cannot read or parse. A malformed file
// is not this function's problem: the parser reports it properly a moment
// later, and failing here would turn a clear parse error into a confusing
// run-identity one.
//
// modTime is carried as the ordering fallback for a report whose timestamp is
// missing or unparseable, so selection stays total — see selectCurrentRun.
func readRunMeta(path string) runMeta {
	m := runMeta{path: path}
	if info, err := os.Stat(path); err == nil {
		m.modTime = info.ModTime()
	}
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the user's own argument list
	if err != nil {
		return m
	}
	var probe struct {
		Metadata struct {
			RunID     string `json:"runId"`
			Timestamp string `json:"timestamp"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return m
	}
	m.runID = probe.Metadata.RunID
	if ts, err := time.Parse(time.RFC3339, probe.Metadata.Timestamp); err == nil {
		m.timestamp = ts
	}
	return m
}

// selectCurrentRun narrows a directory's report files to the run that just
// finished, and returns the files to upload.
//
// WHY THIS EXISTS
// `collect <dir>` uploads every *.json it finds, which is what lets N sharded
// jobs write into one directory and merge into a single Launch. The same
// behaviour once merged a file left over from a PREVIOUS run — the launch
// looked entirely plausible and contained results nobody ran, which corrupts
// the history flaky-detection is built on.
//
// Refusing to upload was the first fix for that, and it traded a silent-wrong
// launch for a broken one: the user had to go and clear the directory, on every
// project, in CI and locally, forever. Now that reports carry a runId there is
// no need to ask. The newest run is the one that just produced these files;
// anything older is by definition stale, and dropping it needs no confirmation
// because nothing is deleted — the files stay on disk, they are simply not
// uploaded.
//
// Files with NO runId are always included. They come from reporters released
// before the field existed and, the case that actually matters, from a
// mixed-version directory where an updated reporter and an older one both
// describe one real run. Excluding them to guard against stale data would drop
// live results.
func selectCurrentRun(files []string, allowMixed bool, warn io.Writer) []string {
	groups := runGroups(files)

	known := make([]string, 0, len(groups))
	for id := range groups {
		if id != "" {
			known = append(known, id)
		}
	}
	// One known run, or none at all (older reporters), is the normal case and
	// needs no narrowing.
	if len(known) <= 1 || allowMixed {
		if len(known) > 1 && allowMixed {
			fmt.Fprintf(warn, "merging %d runs into one launch (--allow-mixed-runs)\n", len(known))
		}
		return files
	}

	current := newestRun(groups, known)

	// Filter IN THE CALLER'S ORDER rather than rebuilding from the groups.
	// --shard numbers files by their position in this slice (tagShardsByFile),
	// and its own help tells users to list them explicitly in shard order when
	// the index has to be stable — so reordering here would silently renumber
	// every shard. Input order is already deterministic, so this also keeps the
	// selection reproducible without sorting.
	selected := make(map[string]bool, len(groups[current])+len(groups[""]))
	for _, m := range groups[current] {
		selected[m.path] = true
	}
	// Unattributable files ride along, as documented above.
	for _, m := range groups[""] {
		selected[m.path] = true
	}
	kept := make([]string, 0, len(selected))
	for _, f := range files {
		if selected[f] {
			kept = append(kept, f)
		}
	}

	fmt.Fprintf(warn, "ignored %d file(s) from %d earlier run(s) (--allow-mixed-runs to include them)\n",
		len(files)-len(kept), len(known)-1)
	return kept
}

// newestRun picks the run whose most recent report is newest. Ordering must be
// TOTAL: two shards can write in the same millisecond, and a report may carry no
// parseable timestamp at all, so ties fall through to file mtime and finally to
// the run id itself — never to map iteration order, which would make the choice
// differ between runs on identical input.
func newestRun(groups map[string][]runMeta, known []string) string {
	sort.Strings(known)
	best := known[0]
	bestTS, bestMod := groupHigh(groups[best])
	for _, id := range known[1:] {
		ts, mod := groupHigh(groups[id])
		if ts.After(bestTS) || (ts.Equal(bestTS) && mod.After(bestMod)) {
			best, bestTS, bestMod = id, ts, mod
		}
	}
	return best
}

// groupHigh is a run's high-water mark: the newest timestamp and mtime any of
// its files carries.
func groupHigh(ms []runMeta) (time.Time, time.Time) {
	var ts, mod time.Time
	for _, m := range ms {
		if m.timestamp.After(ts) {
			ts = m.timestamp
		}
		if m.modTime.After(mod) {
			mod = m.modTime
		}
	}
	return ts, mod
}

// verifyFilesExist fails on the first missing path rather than uploading a partial set.
func verifyFilesExist(files []string) error {
	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", file)
		}
	}
	return nil
}

// resolveFramework converts an explicit --format into a domain.Framework. An empty
// format returns the zero value, which tells the service to auto-detect per file.
func resolveFramework(format string) (domain.Framework, error) {
	if format == "" {
		return "", nil
	}
	framework := domain.Framework(strings.ToLower(format))
	if !framework.IsValid() {
		return "", fmt.Errorf("unsupported format: %s. Use 'qf list-formats' to see supported formats", format)
	}
	return framework, nil
}

// validateOutputOption rejects --output rather than silently ignoring it (API-01). It
// only affects dry runs and only supports "json"; previously "--output yaml", or
// "--output json" without --dry-run, just uploaded as normal with no hint that the flag
// did nothing.
func validateOutputOption(opts collectOptions) error {
	if opts.output == "" {
		return nil
	}
	if opts.output != "json" {
		return fmt.Errorf("unsupported output format: %q (only 'json' is supported)", opts.output)
	}
	if !opts.dryRun {
		return errors.New("--output only applies to --dry-run; add --dry-run to print the parsed report")
	}
	return nil
}

func (c *CLI) runCollect(ctx context.Context, files []string, opts collectOptions) error {
	warnLegacyAPIKey()

	// Order matters: --platform is checked before the config is mutated, and the file
	// checks come before --format/--output so a bad path still reports as a bad path.
	if err := validatePlatform(opts.platform); err != nil {
		return err
	}

	if err := applyCollectOptions(c.config, opts); err != nil {
		return err
	}
	// Fill branch/commit from local git only now (collect is the sole consumer),
	// after explicit flags/CI env vars have had their say (BUG-39).
	c.config.DetectGit()

	if err := c.config.Validate(); err != nil {
		return err
	}

	// Expand glob patterns (the help text and examples advertise them, but the
	// CLI never expanded them — BUG-28). A pattern that matches nothing is an
	// error, so a typo'd glob fails loudly instead of uploading nothing.
	files, err := expandGlobs(files)
	if err != nil {
		return err
	}

	files, err = expandDirectories(files)
	if err != nil {
		return err
	}

	if err := verifyFilesExist(files); err != nil {
		return err
	}

	// Guards the one hazard of collecting a whole directory: a report left over
	// from a previous run merging silently into this launch. Narrowed rather
	// than refused — see selectCurrentRun.
	// os.Stderr, not printInfo: --quiet must not hide the fact that files
	// were left out of the upload.
	files = selectCurrentRun(files, opts.allowMixed, os.Stderr)

	framework, err := resolveFramework(opts.format)
	if err != nil {
		return err
	}

	if err := validateOutputOption(opts); err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	if !c.config.IsQuiet() {
		c.printInfo("Processing %d test result file(s)...", len(files))
	}

	// --dry-run --output json prints the parsed report instead of uploading it.
	if opts.dryRun && opts.output == "json" {
		return c.printReportJSON(ctx, files, framework)
	}

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

// printReportJSON parses the files and writes the report to stdout rather than uploading
// it. Output goes to stdout specifically so it can be piped, while the diagnostics
// elsewhere in collect go to stderr (BUG-04).
func (c *CLI) printReportJSON(ctx context.Context, files []string, framework domain.Framework) error {
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
		return errors.New("one or more files failed validation")
	}

	return nil
}

// createVersionCommand creates the version subcommand
func (c *CLI) createVersionCommand() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
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
		Run: func(_ *cobra.Command, _ []string) {
			c.printFormats(category)
		},
	}

	cmd.Flags().StringVarP(&category, "category", "c", "", "Filter by category (generic, unit, bdd, e2e, api, security)")

	return cmd
}

// frameworkDisplayGroups maps every framework onto the six coarse display
// buckets list-formats groups its output under (unit/bdd/e2e/api/security/
// generic). This is DELIBERATELY independent of Framework.GetCategory():
// since the per-framework category redesign (see GetCategory's doc comment),
// GetCategory() returns a category named after the framework itself for
// every specifically-identified framework (e.g. cypress -> "cypress"), which
// is the right value to send the server but the wrong thing to group a
// human-facing listing by — grouping on GetCategory() directly silently
// dropped every framework whose category no longer matched one of the six
// bucket keys (list-formats showed only "qualflare-json" instead of all 24
// frameworks). Keep this map in sync with domain.AllFrameworks(): a
// framework missing from here falls back to CategoryGeneric in
// buildFrameworkDisplayGroups rather than vanishing from the output, and the
// TestBuildFrameworkDisplayGroups_IncludesEveryFramework test in
// list_formats_test.go asserts every framework is actually reachable so a
// future addition can't silently repeat this bug.
var frameworkDisplayGroups = map[domain.Framework]domain.FrameworkCategory{
	domain.FrameworkJUnit:         domain.CategoryGeneric,
	domain.FrameworkCTRF:          domain.CategoryGeneric,
	domain.FrameworkQualflareJSON: domain.CategoryGeneric,

	domain.FrameworkPython:  domain.CategoryUnitTest,
	domain.FrameworkGolang:  domain.CategoryUnitTest,
	domain.FrameworkJest:    domain.CategoryUnitTest,
	domain.FrameworkVitest:  domain.CategoryUnitTest,
	domain.FrameworkMocha:   domain.CategoryUnitTest,
	domain.FrameworkRSpec:   domain.CategoryUnitTest,
	domain.FrameworkPHPUnit: domain.CategoryUnitTest,
	domain.FrameworkTestNG:  domain.CategoryUnitTest,

	domain.FrameworkCucumber: domain.CategoryBDD,
	domain.FrameworkKarate:   domain.CategoryBDD,

	domain.FrameworkPlaywright: domain.CategoryE2E,
	domain.FrameworkCypress:    domain.CategoryE2E,
	domain.FrameworkSelenium:   domain.CategoryE2E,
	domain.FrameworkTestCafe:   domain.CategoryE2E,
	domain.FrameworkMaestro:    domain.CategoryE2E,
	domain.FrameworkXCTest:     domain.CategoryE2E,
	domain.FrameworkEspresso:   domain.CategoryE2E,

	domain.FrameworkNewman: domain.CategoryAPI,
	domain.FrameworkK6:     domain.CategoryAPI,

	domain.FrameworkZAP:       domain.CategorySecurity,
	domain.FrameworkTrivy:     domain.CategorySecurity,
	domain.FrameworkSnyk:      domain.CategorySecurity,
	domain.FrameworkSonarQube: domain.CategorySecurity,
}

// buildFrameworkDisplayGroups groups every framework in domain.AllFrameworks()
// into the six display buckets, via frameworkDisplayGroups rather than
// Framework.GetCategory() — see that map's doc comment for why. Split out
// from printFormats so the grouping itself is directly unit-testable without
// capturing stdout.
func buildFrameworkDisplayGroups() map[domain.FrameworkCategory][]domain.Framework {
	categories := map[domain.FrameworkCategory][]domain.Framework{
		domain.CategoryGeneric:  {},
		domain.CategoryUnitTest: {},
		domain.CategoryBDD:      {},
		domain.CategoryE2E:      {},
		domain.CategoryAPI:      {},
		domain.CategorySecurity: {},
	}

	for _, fw := range domain.AllFrameworks() {
		cat, ok := frameworkDisplayGroups[fw]
		if !ok {
			// Should never happen once frameworkDisplayGroups is kept in sync
			// with domain.AllFrameworks() — fall back to generic rather than
			// silently dropping the framework from the listing.
			cat = domain.CategoryGeneric
		}
		categories[cat] = append(categories[cat], fw)
	}
	return categories
}

func (c *CLI) printFormats(categoryFilter string) {
	categories := buildFrameworkDisplayGroups()

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
