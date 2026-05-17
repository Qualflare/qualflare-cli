package main

import (
	"fmt"
	"os"
	"regexp"

	"qualflare-cli/internal/adapters/cli"
	"qualflare-cli/internal/adapters/http"
	"qualflare-cli/internal/adapters/parsers/factory"
	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/services"
)

func main() {
	os.Exit(run())
}

func run() int {
	// Initialize configuration
	cfg := config.NewConfig()

	// Initialize parser factory
	parserFactory := factory.NewParserFactory()

	// Initialize HTTP client
	httpClient := http.NewHTTPClient(cfg)
	defer httpClient.Close()

	// Initialize report service
	reportService := services.NewReportService(parserFactory, httpClient, cfg)

	// Load auth store (returns an empty in-memory store if the file is missing)
	storePath, err := auth.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: locate config path: %v\n", err)
		return 1
	}
	store, err := auth.Load(storePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Initialize CLI
	cliApp := cli.NewCLI(reportService, cfg, parserFactory, httpClient, store)

	// Create and execute root command
	cmd := cliApp.CreateRootCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", rewriteUnknownCommandError(err))
		return 1
	}
	return 0
}

var unknownCommandPattern = regexp.MustCompile(`^unknown command "([^"]+)" for "qf"`)

// rewriteUnknownCommandError turns cobra's `unknown command "X" for "qf"` into
// a friendly hint when X looks like an identifier the user probably meant to
// log in with.
func rewriteUnknownCommandError(err error) error {
	if err == nil {
		return nil
	}
	m := unknownCommandPattern.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	candidate := m[1]
	if auth.Validate(candidate) != nil {
		return err
	}
	return fmt.Errorf("no identifier %q configured. Run 'qf login %s <token>' to add it. (See 'qf projects' for the list.)", candidate, candidate)
}
