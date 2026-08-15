package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"qualflare-cli/internal/adapters/cli"
	qfhttp "qualflare-cli/internal/adapters/http"
	"qualflare-cli/internal/adapters/parsers/factory"
	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/services"
)

// Exit codes let CI distinguish failure classes (API-04): a transient network
// blip is retryable, a bad token or quota wall is not.
const (
	exitOK        = 0
	exitGeneric   = 1
	exitAuth      = 3 // 401 — token invalid/expired
	exitForbidden = 4 // 402/403 — quota/payment or access denied
	exitNotFound  = 5 // 404
	exitTransient = 7 // 429 / 5xx — safe to retry
)

// exitCodeForError maps an error (unwrapping to *qfhttp.APIError) to an exit code.
func exitCodeForError(err error) int {
	var apiErr *qfhttp.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized:
			return exitAuth
		case apiErr.StatusCode == http.StatusPaymentRequired || apiErr.StatusCode == http.StatusForbidden:
			return exitForbidden
		case apiErr.StatusCode == http.StatusNotFound:
			return exitNotFound
		case apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500:
			return exitTransient
		}
	}
	return exitGeneric
}

func main() {
	os.Exit(run())
}

func run() int {
	// Initialize configuration
	cfg := config.NewConfig()

	// Initialize parser factory
	parserFactory := factory.NewParserFactory()

	// Initialize HTTP client
	httpClient := qfhttp.NewHTTPClient(cfg)
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
		return exitCodeForError(err)
	}
	return exitOK
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
