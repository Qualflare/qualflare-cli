# Qualflare CLI

A command-line tool for [Qualflare](https://qualflare.com) — parse test results from 20+ testing frameworks, manage test data, and interact with your Qualflare projects from the terminal or CI/CD pipelines. Designed for both humans and AI agents.

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
# Collect test results (format auto-detected)
qf collect results.xml --api-key YOUR_API_KEY

# Specify framework explicitly
qf collect results.json --format playwright --api-key YOUR_API_KEY

# Collect multiple files
qf collect *.xml --format junit --api-key YOUR_API_KEY

# Dry run — parse and preview without sending
qf collect results.xml --dry-run

# Output parsed results as JSON
qf collect results.xml --dry-run --output json

# Validate files without sending
qf validate results.xml

# Browse your test data
qf suites list --api-key YOUR_API_KEY
qf launches list --milestone 3
qf defects list --severity critical,high
qf case get 42
```

## CI/CD Integration

Set your API key as an environment variable and add a step after your test runner:

### GitHub Actions

```yaml
- name: Collect test results
  run: qf collect test-results/*.xml
  env:
    QF_API_KEY: ${{ secrets.QF_API_KEY }}
    QF_ENVIRONMENT: ci
    QF_BRANCH: ${{ github.ref_name }}
    QF_COMMIT: ${{ github.sha }}
```

### GitLab CI

```yaml
collect_results:
  stage: report
  script:
    - qf collect test-results/*.xml
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
            qf collect test-results/*.xml
        '''
    }
}
```

### Docker

```bash
docker run --rm \
  -e QF_API_KEY=your-api-key \
  -v $(pwd)/test-results:/results \
  ghcr.io/qualflare/qf:latest collect /results/*.xml
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

### Collect & Parse

```
qf collect [files...]      Collect test results and send to Qualflare
qf validate [files...]     Validate test result files without sending
qf list-formats            List all supported test frameworks
qf version                 Print version information
```

### Test Management

All read commands output JSON to stdout, making them pipeable to `jq` and usable by AI agents.

```
qf suites list             List test suites
qf suite get <seq>         Get suite details

qf cases list --suite <n>  List cases in a suite
qf case get <seq>          Get case details
qf case steps <seq>        Get steps for a case

qf plans list              List test plans
qf plan get <seq>          Get plan details
qf plan cases <seq>        Get cases in a plan

qf launches list           List test launches
qf launch get <seq>        Get launch details

qf defects list            List defects
qf defect get <seq>        Get defect details

qf clusters list           List failure clusters
qf cluster get <id>        Get cluster details

qf milestones list         List milestones
qf milestone get <seq>     Get milestone details
```

### Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--api-key` | | API key for authentication (or set `QF_API_KEY`) |
| `--api-endpoint` | | API endpoint URL (or set `QF_API_ENDPOINT`) |
| `--verbose` | `-v` | Enable verbose output |
| `--quiet` | `-q` | Suppress non-error output |

### Collect Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--format` | `-f` | Test framework format (auto-detected if omitted) |
| `--project` | `-p` | Project name |
| `--environment` | `-e` | Environment name |
| `--lang` | | Language/culture (BCP 47) |
| `--branch` | | Git branch name |
| `--commit` | | Git commit hash |
| `--timeout` | | Request timeout |
| `--dry-run` | | Parse without sending |
| `--output` | `-o` | Output format for dry-run (`json`) |

### Common List Flags

| Flag | Description |
|------|-------------|
| `--page` | Page number |
| `--sort-by` | Sort by field |
| `--sort-desc` | Sort in descending order |
| `--query` | Search query (suites, plans, milestones) |
| `--severity` | Filter by severity (defects, clusters) |
| `--status` | Filter by status (defects) |
| `--suite` | Suite sequence number (cases list, required) |
| `--milestone` | Filter by milestone (launches) |
| `--environment` | Filter by environment (launches) |

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
