# Qualflare CLI

A command-line tool for [Qualflare](https://qualflare.com) — parse test results from 19 testing frameworks, manage test data, and interact with your Qualflare projects from the terminal or CI/CD pipelines. Designed for both humans and AI agents.

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
brew install Qualflare/tap/qf
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

The CLI manages credentials per-project through local **identifiers** (aliases). Log in once with `qf login`, then run every command under that identifier.

```bash
# Save credentials under a local alias
qf login myapp YOUR_API_KEY

# Collect test results (format auto-detected)
qf myapp collect results.xml

# Specify framework explicitly
qf myapp collect results.json --format playwright

# Collect multiple files
qf myapp collect *.xml --format junit

# Dry run — parse and preview without sending
qf myapp collect results.xml --dry-run

# Output parsed results as JSON
qf myapp collect results.xml --dry-run --output json

# Validate files without sending (no auth required)
qf myapp validate results.xml

# Browse your test data
qf myapp suites list
qf myapp launches list --milestone 3
qf myapp defects list --severity critical,high
qf myapp case get 42

# Manage saved identifiers
qf projects             # list saved identifiers
qf logout myapp         # remove credentials
```

## CI/CD Integration

In CI, run `qf login` first (with `--force` to skip the overwrite prompt) and then use that identifier for every subsequent command. Pick a stable identifier like `ci` or your project slug.

### GitHub Actions

```yaml
- name: Collect test results
  run: |
    qf login ci "$QF_TOKEN" --force
    qf ci collect test-results/*.xml
  env:
    QF_TOKEN: ${{ secrets.QF_TOKEN }}
    QF_ENVIRONMENT: ci
    QF_BRANCH: ${{ github.ref_name }}
    QF_COMMIT: ${{ github.sha }}
```

### GitLab CI

```yaml
collect_results:
  stage: report
  script:
    - qf login ci "$QF_TOKEN" --force
    - qf ci collect test-results/*.xml
  variables:
    QF_TOKEN: $QF_TOKEN
    QF_ENVIRONMENT: ci
    QF_BRANCH: $CI_COMMIT_REF_NAME
    QF_COMMIT: $CI_COMMIT_SHA
```

### Jenkins

```groovy
post {
    always {
        sh '''
            export QF_BRANCH=${GIT_BRANCH}
            export QF_COMMIT=${GIT_COMMIT}
            qf login ci "${QF_TOKEN}" --force
            qf ci collect test-results/*.xml
        '''
    }
}
```

### Docker

```bash
docker run --rm \
  -v $(pwd)/test-results:/results \
  -e QF_TOKEN="$QF_TOKEN" \
  ghcr.io/Qualflare/qf:latest \
  /bin/sh -c 'qf login ci "$QF_TOKEN" --force && qf ci collect /results/*.xml'
```

## Configuration

### Authentication

Tokens are stored locally per identifier in:

| Platform | Path |
|----------|------|
| Linux | `$XDG_CONFIG_HOME/qualflare/config.toml` (defaults to `~/.config/qualflare/config.toml`) |
| macOS | `~/Library/Application Support/qualflare/config.toml` |
| Windows | `%AppData%\qualflare\config.toml` |

The file is created with `0600` permissions and the directory with `0700`. Tokens are stored in plain text — protect the file as you would an SSH key.

```bash
qf login myapp qf_xxx           # save credentials
qf login myapp qf_yyy --force   # overwrite without prompting
qf logout myapp                 # remove
qf projects                     # list saved identifiers
```

Identifiers must match `^[a-z0-9][a-z0-9_-]{0,62}$` and cannot collide with reserved command names (`login`, `logout`, `projects`, `version`, `list-formats`, `help`, `completion`).

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
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

> **Migrating from earlier versions:** `QF_API_KEY` and `--api-key` are no longer read. Run `qf login <identifier> $QF_API_KEY` once, then prefix every command with `<identifier>`.

## Commands

### Auth & meta (no identifier)

```
qf login <id> <token>      Save credentials (use --force to overwrite)
qf logout <id>             Remove saved credentials
qf projects                List saved identifiers
qf list-formats            List all supported test frameworks
qf version                 Print version information
```

### Project-scoped (run as `qf <identifier> <command>`)

All read commands output JSON to stdout, making them pipeable to `jq` and usable by AI agents.

```
qf <id> collect [files...]         Collect test results and send to Qualflare
qf <id> validate [files...]        Validate test result files without sending

qf <id> suites list                List test suites
qf <id> suite get <seq>            Get suite details

qf <id> cases list --suite <n>     List cases in a suite
qf <id> case get <seq>             Get case details
qf <id> case steps <seq>           Get steps for a case

qf <id> plans list                 List test plans
qf <id> plan get <seq>             Get plan details
qf <id> plan cases <seq>           Get cases in a plan

qf <id> launches list              List test launches
qf <id> launch get <seq>           Get launch details

qf <id> defects list               List defects
qf <id> defect get <seq>           Get defect details

qf <id> clusters list              List failure clusters
qf <id> cluster get <id>           Get cluster details

qf <id> milestones list            List milestones
qf <id> milestone get <seq>        Get milestone details
```

### Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--api-endpoint` | | API endpoint URL (or set `QF_API_ENDPOINT`) |
| `--verbose` | `-v` | Enable verbose output |
| `--quiet` | `-q` | Suppress non-error output |

### Collect Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--format` | `-f` | Test framework format (auto-detected if omitted) |
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
