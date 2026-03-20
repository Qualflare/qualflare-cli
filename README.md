# Qualflare CLI

A command-line tool for parsing test results from 20+ testing frameworks and uploading them to [Qualflare](https://qualflare.com) for centralized test management and analytics.

## Supported Frameworks

| Category | Frameworks |
|----------|-----------|
| **Unit Testing** | JUnit, pytest, Go testing, Jest/Vitest, Mocha, RSpec, PHPUnit |
| **BDD** | Cucumber, Karate |
| **E2E / UI** | Playwright, Cypress, Selenium, TestCafe |
| **API Testing** | Newman (Postman), k6 |
| **Security** | OWASP ZAP, Trivy, Snyk, SonarQube |

## Installation

### Homebrew (macOS / Linux)

```bash
brew install qualflare/tap/qf
```

### Docker

```bash
docker pull ghcr.io/qualflare/qf:latest
```

### Binary Download

Download the latest release for your platform from the [Releases](https://github.com/Qualflare/qualflare-cli/releases) page.

### Build from Source

Requires Go 1.23+.

```bash
git clone https://github.com/Qualflare/qualflare-cli.git
cd qualflare-cli
make build
```

The binary is output to `build/qf`.

## Quick Start

```bash
# Upload test results (format auto-detected)
qf upload results.xml --api-key YOUR_API_KEY

# Specify framework explicitly
qf upload results.json --format playwright --api-key YOUR_API_KEY

# Upload multiple files
qf upload *.xml --format junit --api-key YOUR_API_KEY

# Dry run — parse and preview without uploading
qf upload results.xml --dry-run

# Output parsed results as JSON
qf upload results.xml --dry-run --output json

# Validate files without uploading
qf validate results.xml
```

## CI/CD Integration

Set your API key as an environment variable and add a step after your test runner:

### GitHub Actions

```yaml
- name: Upload test results
  run: qf upload test-results/*.xml
  env:
    QF_API_KEY: ${{ secrets.QF_API_KEY }}
    QF_ENVIRONMENT: ci
    QF_BRANCH: ${{ github.ref_name }}
    QF_COMMIT: ${{ github.sha }}
```

### GitLab CI

```yaml
upload_results:
  stage: report
  script:
    - qf upload test-results/*.xml
  variables:
    QF_API_KEY: $QF_API_KEY
    QF_ENVIRONMENT: ci
    QF_BRANCH: $CI_COMMIT_REF_NAME
    QF_COMMIT: $CI_COMMIT_SHA
```

### Jenkins

```groovy
post {
    always {
        sh '''
            export QF_API_KEY=${QF_API_KEY}
            export QF_BRANCH=${GIT_BRANCH}
            export QF_COMMIT=${GIT_COMMIT}
            qf upload test-results/*.xml
        '''
    }
}
```

### Docker

```bash
docker run --rm \
  -e QF_API_KEY=your-api-key \
  -v $(pwd)/test-results:/results \
  ghcr.io/qualflare/qf:latest upload /results/*.xml
```

## Configuration

Configuration is resolved in order: CLI flags > environment variables > config file > defaults.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QF_API_KEY` | API authentication key | — |
| `QF_API_ENDPOINT` | API endpoint URL | `https://api.qualflare.com` |
| `QF_ENVIRONMENT` | Environment name | `development` |
| `QF_LANGUAGE` | Language/culture (BCP 47) | `en-US` |
| `QF_BRANCH` | Git branch name | auto-detected from CI |
| `QF_COMMIT` | Git commit hash | auto-detected from CI |
| `QF_RETRY_MAX` | Max retry attempts | `3` |
| `QF_TIMEOUT` | Request timeout | `30s` |
| `QF_VERBOSE` | Enable verbose output | `false` |
| `QF_QUIET` | Suppress non-error output | `false` |

Git branch and commit are auto-detected from common CI environment variables (`GITHUB_REF_NAME`, `CI_COMMIT_REF_NAME`, `BITBUCKET_BRANCH`, etc.).

### Config File

Create a `qualflare.yaml` or `.qualflarerc` in your project root:

```yaml
api_key: your-api-key
api_endpoint: https://api.qualflare.com
environment: production
language: en-US
branch: main
retry_max: 3
timeout: 30s
```

## Commands

```
qf upload [files...]       Upload test results to Qualflare
qf validate [files...]     Validate test result files without uploading
qf list-formats            List all supported test frameworks
qf version                 Print version information
```

### Upload Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--format` | `-f` | Test framework format (auto-detected if omitted) |
| `--project` | `-p` | Project name |
| `--environment` | `-e` | Environment name |
| `--lang` | | Language/culture (BCP 47) |
| `--branch` | | Git branch name |
| `--commit` | | Git commit hash |
| `--api-endpoint` | | API endpoint URL |
| `--api-key` | | API key |
| `--timeout` | | Request timeout |
| `--dry-run` | | Parse without uploading |
| `--output` | `-o` | Output format for dry-run (`json`) |

## Format Detection

When `--format` is not specified, the CLI auto-detects the framework using:

1. **Filename patterns** — e.g., `playwright-report.json` resolves to Playwright
2. **File content analysis** — characteristic JSON keys or XML root elements
3. **File extension fallback** — `.xml` defaults to JUnit, `.json` uses content detection

For best results, use descriptive filenames or specify `--format` explicitly.

## Development

### Prerequisites

- Go 1.23+
- Make

### Build & Test

```bash
make build          # Build for current platform
make build-all      # Build for Linux, macOS, Windows
make test           # Run tests with coverage
make test-short     # Run short tests only
make lint           # Run golangci-lint
make install        # Install to $GOPATH/bin
```

### Adding a New Parser

1. Create a package under `internal/adapters/parsers/<category>/`
2. Implement the `Parser` interface:

```go
type Parser interface {
    Parse(reader io.Reader) (*domain.Suite, error)
    GetFramework() domain.Framework
    SupportedFileExtensions() []string
}
```

3. Register the framework constant in `internal/core/domain/models.go`
4. Register the parser in `internal/adapters/parsers/factory/factory.go`
5. Add detection rules in the factory's `DetectFramework` and `detectJSONFramework` methods

### Project Structure

```
qualflare-cli/
├── cmd/                        # CLI entry point
├── internal/
│   ├── adapters/
│   │   ├── cli/                # Cobra command definitions
│   │   ├── http/               # HTTP client with retry logic
│   │   └── parsers/            # Test framework parsers
│   │       ├── unit/           # JUnit, pytest, Go, Jest, Mocha, RSpec, PHPUnit
│   │       ├── bdd/            # Cucumber, Karate
│   │       ├── e2e/            # Playwright, Cypress, Selenium, TestCafe
│   │       ├── api/            # Newman, k6
│   │       ├── security/       # ZAP, Trivy, Snyk, SonarQube
│   │       └── factory/        # Parser selection and framework detection
│   ├── config/                 # Configuration management
│   ├── core/
│   │   ├── domain/             # Domain models (Launch, Suite, Case)
│   │   ├── ports/              # Interface definitions
│   │   └── services/           # Report processing service
│   └── version/                # Version info
├── .goreleaser.yml             # Release automation
├── Makefile
└── go.mod
```

## Documentation

- [Qualflare Documentation](https://docs.qualflare.com)
- [Qualflare Website](https://qualflare.com)

## Contributing

Contributions are welcome. Please open an issue to discuss your idea before submitting a pull request.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Make your changes and add tests
4. Run `make test && make lint` to verify
5. Commit and open a pull request

## License

Licensed under the [Apache License 2.0](LICENSE).
