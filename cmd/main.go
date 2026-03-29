package main

import (
	"fmt"
	"os"

	"qualflare-cli/internal/adapters/cli"
	"qualflare-cli/internal/adapters/http"
	"qualflare-cli/internal/adapters/parsers/factory"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/services"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Initialize configuration
	cfg := config.NewConfig()

	// Initialize parser factory
	parserFactory := factory.NewParserFactory()

	// Initialize HTTP client
	httpClient := http.NewHTTPClient(cfg)
	defer httpClient.Close()

	// Initialize report service
	reportService := services.NewReportService(parserFactory, httpClient, cfg)

	// Initialize CLI
	cliApp := cli.NewCLI(reportService, cfg, parserFactory, httpClient)

	// Create and execute root command
	cmd := cliApp.CreateRootCommand()
	return cmd.Execute()
}
