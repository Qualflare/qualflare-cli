package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/runner"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"

	"github.com/spf13/cobra"
)

// WrappedCommandError signals that qf run's wrapped test command itself
// exited non-zero. main.go propagates ExitCode verbatim, without printing an
// "Error: ..." line — the wrapped command already printed its own failure
// output, and repeating it would be noise.
type WrappedCommandError struct {
	ExitCode int
}

func (e *WrappedCommandError) Error() string {
	return fmt.Sprintf("wrapped command exited with status %d", e.ExitCode)
}

type runOptions struct {
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
	suiteName   string
}

// createRunCommand creates the `run` subcommand: wraps execution of a test
// command, captures its console output, extracts step-narrated test cases
// from it via a framework-specific log-step extractor, and uploads them
// exactly like `collect` does.
func (c *CLI) createRunCommand() *cobra.Command {
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
		suiteName   string
	)

	cmd := &cobra.Command{
		Use:   "run [flags] -- <command> [args...]",
		Short: "Run a test command, capture its output, and upload extracted results",
		Long: `Wraps execution of a test command: streams its stdout/stderr live to your
terminal exactly as it would run normally, while also capturing a copy. After the
command exits, the captured output is parsed by a per-framework "log step extractor"
(RSpec --format documentation, pytest-bdd --gherkin-terminal-reporter, Mocha's
default spec reporter) to build test cases — name, status, and Given/When/Then-style
steps — from the console narration your framework already prints, then uploads them
exactly like 'collect' does.

'qf run' exits with the SAME exit code as the wrapped command, so it can safely
replace the bare test command in an existing CI step.

REQUIRES SERIAL TEST EXECUTION. A parallel test runner interleaves multiple tests'
output lines, making it impossible to attribute a line to the right test — qf run
warns on a best-effort basis when it recognizes a parallel-execution flag, but cannot
catch every case.`,
		Example: `  # RSpec, using nested Given/When/Then-style contexts
  qf my-app run --format rspec -- bundle exec rspec --format documentation

  # pytest-bdd
  qf my-app run --format python -- pytest --gherkin-terminal-reporter -v

  # Mocha
  qf my-app run --format mocha -- npx mocha --reporter spec

  # Auto-detect the extractor from the captured output
  qf my-app run -- bundle exec rspec --format documentation`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dashAt := cmd.ArgsLenAtDash()
			if dashAt < 0 {
				return errors.New("qf run requires '--' before the wrapped command, e.g. `qf <id> run -- pytest -v`")
			}
			wrapped := args[dashAt:]
			if len(wrapped) == 0 {
				return errors.New("no command given after '--'")
			}
			return c.runRun(cmd.Context(), wrapped, runOptions{
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
				suiteName:   suiteName,
			})
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "", "Test framework for step extraction (auto-detected from captured output if not specified)")
	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Environment name (default: $QF_ENVIRONMENT or 'development')")
	cmd.Flags().StringVar(&language, "lang", "", "Language/culture, BCP 47 (default: $QF_LANGUAGE or 'en-US')")
	cmd.Flags().StringVar(&platform, "platform", "", "Platform: android|ios|desktop|web|api (default: $QF_PLATFORM or 'api')")
	cmd.Flags().Int64Var(&milestone, "milestone", 0, "Milestone sequence number to link this launch to (or $QF_MILESTONE)")
	cmd.Flags().StringVar(&branch, "branch", "", "Git branch name")
	cmd.Flags().StringVar(&commit, "commit", "", "Git commit hash")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Request timeout")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Run and capture without uploading")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format for dry-run (json)")
	cmd.Flags().StringVar(&suiteName, "name", "", `Suite name for the captured run (default: "<framework> (captured run)")`)

	return cmd
}

// knownParallelFlags is a best-effort, non-exhaustive allowlist of flags that
// enable parallel test execution in frameworks this package targets.
// Detecting every case is not possible — a wrapper script that internally
// parallelizes with no recognizable flag on qf run's own argv slips through —
// this exists to catch the common, explicit cases and warn early.
var knownParallelFlags = []string{"-n", "--numprocesses", "--parallel"}

func warnIfLooksParallel(warn io.Writer, wrapped []string) {
	for _, arg := range wrapped {
		for _, flag := range knownParallelFlags {
			if arg == flag || strings.HasPrefix(arg, flag+"=") {
				fmt.Fprintf(warn, "warning: qf run: the wrapped command appears to enable parallel test execution (detected %q); step extraction requires serial execution and results may be incomplete or misattributed.\n", flag)
				return
			}
		}
	}
}

func (c *CLI) runRun(ctx context.Context, wrapped []string, opts runOptions) error {
	warnLegacyAPIKey()

	if err := validatePlatform(opts.platform); err != nil {
		return err
	}

	applyCollectOptions(c.config, collectOptions{
		environment: opts.environment,
		language:    opts.language,
		platform:    opts.platform,
		milestone:   opts.milestone,
		branch:      opts.branch,
		commit:      opts.commit,
		timeout:     opts.timeout,
		dryRun:      opts.dryRun,
	})
	c.config.DetectGit()

	if err := c.config.Validate(); err != nil {
		return err
	}
	if err := validateOutputOption(collectOptions{output: opts.output, dryRun: opts.dryRun}); err != nil {
		return err
	}

	var explicitFramework domain.Framework
	if opts.format != "" {
		fw, err := resolveFramework(opts.format)
		if err != nil {
			return err
		}
		explicitFramework = fw
	}

	warnIfLooksParallel(os.Stderr, wrapped)

	if !c.config.IsQuiet() {
		c.printInfo("Running: %s", strings.Join(wrapped, " "))
	}

	runCmd := c.runCmd
	if runCmd == nil {
		runCmd = runner.Run
	}
	result, runErr := runCmd(ctx, os.Stdout, wrapped[0], wrapped[1:]...)
	if runErr != nil {
		return fmt.Errorf("failed to run wrapped command: %w", runErr)
	}

	extractor, framework, err := c.resolveExtractor(explicitFramework, result.Output)
	if err != nil {
		return err
	}

	cases, err := extractor.ExtractCases(result.Output)
	if err != nil {
		return fmt.Errorf("failed to extract test steps from captured output: %w", err)
	}
	if len(cases) == 0 {
		fmt.Fprintf(os.Stderr, "warning: no test cases recognized for --format %s; nothing to upload\n", framework)
	}

	suiteName := opts.suiteName
	if suiteName == "" {
		suiteName = fmt.Sprintf("%s (captured run)", framework)
	}
	suite := &domain.Suite{
		Name:      suiteName,
		Category:  framework.GetCategory(),
		Duration:  result.Duration,
		Timestamp: time.Now().UTC(),
		Cases:     cases,
	}
	suite.RecomputeCounts()

	var uploadErr error
	if opts.dryRun && opts.output == "json" {
		launch := c.reportService.BuildCapturedLaunch(suite, framework)
		jsonData, marshalErr := json.MarshalIndent(launch, "", "  ")
		if marshalErr != nil {
			uploadErr = fmt.Errorf("failed to marshal report: %w", marshalErr)
		} else {
			fmt.Println(string(jsonData))
		}
	} else {
		// --timeout only bounds the upload, not the wrapped command above — a
		// test suite's own runtime is the CI system's concern, not qf run's.
		uploadCtx, cancel := context.WithTimeout(ctx, opts.timeout)
		defer cancel()
		uploadErr = c.reportService.ProcessCapturedSuite(uploadCtx, suite, framework)
	}

	// Exit-code rule: the wrapped command's own exit code is the primary
	// signal (so `qf run` is a safe drop-in CI replacement); an upload
	// failure only overrides it when the tests themselves passed — a failing
	// upload must never be silently swallowed, but a real test failure must
	// never be masked by a flaky upload.
	switch {
	case result.ExitCode != 0 && uploadErr != nil:
		fmt.Fprintf(os.Stderr, "ERR failed to upload captured results: %v\n", uploadErr)
		return &WrappedCommandError{ExitCode: result.ExitCode}
	case result.ExitCode != 0:
		return &WrappedCommandError{ExitCode: result.ExitCode}
	case uploadErr != nil:
		return fmt.Errorf("failed to upload captured results: %w", uploadErr)
	default:
		if !c.config.IsQuiet() {
			if opts.dryRun {
				c.printSuccess("Test results parsed successfully (dry run)")
			} else {
				c.printSuccess("Test results collected successfully")
			}
		}
		return nil
	}
}

// resolveExtractor uses explicit when set, otherwise auto-detects from the
// captured output.
func (c *CLI) resolveExtractor(explicit domain.Framework, output []byte) (ports.LogStepExtractor, domain.Framework, error) {
	if explicit != "" {
		e, err := c.logExtractors.GetExtractor(explicit)
		if err != nil {
			return nil, "", err
		}
		return e, explicit, nil
	}
	e, err := c.logExtractors.DetectExtractor(output)
	if err != nil {
		return nil, "", err
	}
	return e, e.GetFramework(), nil
}
