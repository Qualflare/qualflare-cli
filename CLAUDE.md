# Qualflare CLI

This file provides guidance to Claude Code when working with code in this repository.

## Overview

Command-line tool for parsing and collecting test results from various testing frameworks to the Qualflare test management platform. Supports 20+ test frameworks across unit, integration, E2E, API, and security testing.

## Tech Stack

- **Language**: Go 1.23
- **CLI Framework**: Cobra (spf13/cobra)
- **HTTP Client**: Custom HTTP client with retry logic

## Commands

```bash
# Build
make build              # Build binary for current platform
make build-all          # Build for Linux, macOS, Windows

# Testing
make test               # Run tests with coverage
make test-short         # Run short tests

# Code Quality
make lint               # Run golangci-lint

# Installation
make install            # Install to GOPATH/bin

# Release
make release            # Build with goreleaser
```

## Architecture

### Directory Structure

```
qualflare-cli/
├── cmd/
│   └── main.go                 # CLI entry point
├── internal/
│   ├── adapters/
│   │   ├── cli/                # CLI command definitions
│   │   ├── http/               # HTTP client for API
│   │   └── parsers/            # Test framework parsers
│   │       ├── unit/           # JUnit, pytest, golang, jest, mocha, rspec, phpunit
│   │       ├── bdd/            # Cucumber, Karate
│   │       ├── e2e/            # Playwright, Cypress, Selenium, TestCafe
│   │       ├── api/            # Newman, k6
│   │       ├── security/       # ZAP, Trivy, Snyk, SonarQube
│   │       └── factory/        # Parser factory
│   ├── config/                 # Configuration management
│   ├── core/
│   │   ├── domain/             # Domain models (Launch, Suite, Case, Framework)
│   │   ├── ports/              # Interface definitions
│   │   └── services/           # Report processing service
│   └── version/                # Version info
├── examples/                   # Example test result files
├── docs/                       # Documentation
├── Makefile
├── go.mod
├── .goreleaser.yml             # Release configuration
└── README.md
```

## Supported Test Frameworks

### Unit Testing
- **JUnit** (Java/Kotlin)
- **pytest** (Python)
- **Go testing** (Go)
- **Jest** (JavaScript/TypeScript)
- **Mocha** (JavaScript)
- **RSpec** (Ruby)
- **PHPUnit** (PHP)

### BDD Testing
- **Cucumber** (Multiple languages)
- **Karate** (Java-based DSL)

### E2E/UI Testing
- **Playwright**
- **Cypress**
- **Selenium**
- **TestCafe**

### API Testing
- **Newman** (Postman)
- **k6**

### Security Scanning
- **OWASP ZAP**
- **Trivy**
- **Snyk**
- **SonarQube**

## Usage

### Collect Test Results
```bash
qf collect [files...]
```

### Validate Without Sending
```bash
qf validate [files...]
```

### Show Version
```bash
qf version
```

### List Supported Frameworks
```bash
qf list-formats
```

## Configuration

### Environment Variables

| Variable | Description |
|----------|-------------|
| `QF_API_KEY` | API authentication key |
| `QF_API_ENDPOINT` | API endpoint URL (default: `https://api.qualflare.com`) |
| `QF_ENVIRONMENT` | Environment name |
| `QF_LANGUAGE` | Language/culture (BCP 47) |
| `QF_BRANCH` | Git branch name |
| `QF_COMMIT` | Git commit hash |
| `QF_RETRY_MAX` | Retry attempts (default: 3) |
| `QF_TIMEOUT` | Request timeout in seconds |
| `QF_VERBOSE` | Enable verbose output |
| `QF_QUIET` | Suppress non-error output |

### Configuration File

Create a `qualflare.yaml` or `.qualflarerc` in your project root:

```yaml
api_key: your-api-key
api_endpoint: https://api.qualflare.com
environment: production
language: en-US
branch: main
commit: abc123
retry_max: 3
timeout: 30
```

## Parser Architecture

### Parser Interface

```go
type Parser interface {
    Name() string
    SupportedFormats() []string
    Parse(path string) (*domain.Launch, error)
}
```

### Factory Pattern

The parser factory (`internal/adapters/parsers/factory/`) automatically selects the correct parser based on file extension or format detection.

### Adding a New Parser

1. Create a new file in `internal/adapters/parsers/{category}/`
2. Implement the `Parser` interface
3. Register the parser in the factory

Example:
```go
type myFrameworkParser struct{}

func (p *myFrameworkParser) Name() string {
    return "myframework"
}

func (p *myFrameworkParser) SupportedFormats() []string {
    return []string{".xml", ".json"}
}

func (p *myFrameworkParser) Parse(path string) (*domain.Launch, error) {
    // Parse implementation
}
```

## Domain Models

### Launch
Represents a test execution run with metadata (name, environment, branch, commit).

### Suite
A collection of test cases within a launch.

### Case
Individual test case with status (passed, failed, skipped, broken).

### Framework
Metadata about the test framework that produced the results.

## HTTP Client

Custom HTTP client with:
- Automatic retry with exponential backoff
- JWT authentication
- Progress tracking for large submissions
- Timeout handling

## Related Documentation

- [docs/](./docs/) - Additional documentation
