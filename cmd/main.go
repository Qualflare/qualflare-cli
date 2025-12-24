package main

import (
	"log"
	"qualflare-cli/internal/adapters/cli"
	"qualflare-cli/internal/adapters/http"
	"qualflare-cli/internal/adapters/parsers"
	"qualflare-cli/internal/config"
	"qualflare-cli/internal/core/services"
)

func main() {
	// Initialize configuration
	cfg := config.NewConfig()

	// Initialize parsers
	parserFactory := parsers.NewParserFactory()

	// Initialize HTTP client
	httpClient := http.NewHTTPClient(cfg)

	// Initialize services
	reportService := services.NewReportService(parserFactory, httpClient, cfg)

	// Initialize CLI
	cliApp := cli.NewCLI(reportService, cfg)

	// Execute CLI
	cmd := cliApp.CreateRootCommand()
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
