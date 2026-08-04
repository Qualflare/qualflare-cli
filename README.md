# Qualflare CLI

[![CI](https://github.com/qualflare/qualflare-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/qualflare/qualflare-cli/actions/workflows/ci.yml)
[![Quality Gate](https://sonarcloud.io/api/project_badges/measure?project=Qualflare_qualflare-cli&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Qualflare_qualflare-cli)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=Qualflare_qualflare-cli&metric=coverage)](https://sonarcloud.io/component_measures?id=Qualflare_qualflare-cli&metric=coverage)
[![Release](https://img.shields.io/github/v/release/qualflare/qualflare-cli)](https://github.com/qualflare/qualflare-cli/releases/latest)
[![npm](https://img.shields.io/npm/v/@qualflare/cli?logo=npm)](https://www.npmjs.com/package/@qualflare/cli)
[![Docker](https://img.shields.io/docker/v/qualflare/qf?logo=docker&label=docker&sort=semver)](https://hub.docker.com/r/qualflare/qf)
[![Go Report Card](https://goreportcard.com/badge/github.com/qualflare/qualflare-cli)](https://goreportcard.com/report/github.com/qualflare/qualflare-cli)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

Upload test results to [Qualflare](https://qualflare.com) from any CI pipeline or local machine. Supports 23 testing frameworks with automatic format detection.

## Installation

**Homebrew** (macOS / Linux — recommended)

```bash
brew install qualflare/tap/qf

# Upgrade to the latest version
brew upgrade qualflare/tap/qf
```

**npm** (any OS, Node ≥ 16) — downloads the platform-native binary and puts `qf` on your `PATH`:

```bash
npm install -g @qualflare/cli
```

**Docker** — run without installing anything, ideal for CI. Multi-arch (amd64 + arm64), published to both GitHub Container Registry and Docker Hub:

```bash
# GitHub Container Registry (no login needed to pull)
docker run --rm ghcr.io/qualflare/qf version

# Also on Docker Hub — pin a version for reproducible CI
docker run --rm docker.io/qualflare/qf:0.1.10 version
```

To collect results from a pipeline, mount your workspace and pass your token — see [CI/CD](#cicd) for the full flow.

**Binary download** — grab the latest release for your platform from the [Releases](https://github.com/qualflare/qualflare-cli/releases/latest) page and place `qf` on your `PATH`.

**Build from source** — requires Go 1.25+: `git clone` then `make build`. The binary lands in `build/qf`.

## Getting Started

**1. Get your API token**

Open **Project → Settings → Access Tokens** in the [Qualflare dashboard](https://app.qualflare.com) (`/project/<slug>/settings/access-tokens/`) and create a new token.

**2. Log in**

Save your token under a short local identifier (the project slug shown in the dashboard works well):

```bash
qf login myapp qf_your_token_here
```

This writes credentials to `~/.config/qualflare/config.toml` (`0600`). Run `qf login --help` for options.

**3. Upload your first report**

```bash
qf myapp collect path/to/results.xml
```

The framework is detected automatically from the filename and file content. You're done.

## Usage

### Collect test results

```bash
# Auto-detect framework from filename / content
qf myapp collect results.xml

# Multiple files (glob or space-separated)
qf myapp collect test-results/*.xml

# Specify the framework explicitly
qf myapp collect report.json --format playwright

# Parse and preview locally without sending (no auth required)
qf myapp collect results.xml --dry-run --output json
```

Run `qf myapp collect --help` for the full flag list (`--branch`, `--commit`, `--environment`, `--timeout`, etc.).

### Validate before uploading

Validate that `qf` can parse your files — useful as a pre-upload gate:

```bash
qf myapp validate results.xml
```

### Browse your test data

All read commands print JSON, so they pipe naturally to `jq` and work well with AI agents:

```bash
qf myapp suites list
qf myapp cases list --suite 5
qf myapp plans list
qf myapp launches list --milestone 3
qf myapp defects list --severity critical,high
```

Run `qf myapp <command> --help` for pagination, filtering, and sorting options.

## CI/CD

Add a single step after your test run. Store your token in a repository secret named `QF_TOKEN`:

```yaml
- name: Upload test results
  run: |
    qf login ci "$QF_TOKEN" --force
    qf ci collect test-results/*.xml
  env:
    QF_TOKEN: ${{ secrets.QF_TOKEN }}
    QF_BRANCH: ${{ github.ref_name }}
    QF_COMMIT: ${{ github.sha }}
```

`qf` is a single static binary with no runtime dependencies. If you're using GitLab CI, CircleCI, Jenkins, or another platform, download the binary from [Releases](https://github.com/qualflare/qualflare-cli/releases/latest) and run the same two commands. See [the documentation](https://docs.qualflare.com) for platform-specific snippets.

## Commands

| Command | Description |
|---------|-------------|
| `qf login <id> <token>` | Save credentials for a project identifier |
| `qf logout <id>` | Remove saved credentials |
| `qf projects` | List all saved identifiers |
| `qf list-formats` | List all supported test frameworks |
| `qf version` | Print version information |
| `qf <id> collect [files...]` | Parse and upload test results |
| `qf <id> validate [files...]` | Parse and validate without uploading |
| `qf <id> suites\|cases\|plans\|launches\|defects\|clusters\|milestones ...` | Read project data |

Run `qf <command> --help` for flags and examples.

## Supported Frameworks

| Category | Frameworks |
|----------|-----------|
| **Generic (JUnit-compatible)** | JUnit |
| **Unit Testing** | pytest, Go testing, Jest/Vitest, Mocha, RSpec, PHPUnit, TestNG |
| **BDD** | Cucumber, Karate |
| **UI / E2E / Mobile** | Playwright, Cypress, Selenium, TestCafe, Maestro, XCTest, Espresso |
| **API Testing** | Newman (Postman), k6 |
| **Security** | OWASP ZAP, Trivy, Snyk, SonarQube |

Format is auto-detected from the filename and file content. Pass `--format <name>` to override. Run `qf list-formats` for all valid format names.

Any framework that emits standard JUnit XML — NUnit, MSTest, xUnit.net, Robot Framework, and others — can be uploaded using `--format junit`. Because all JUnit emitters share the same XML schema, use `--format <name>` (or a recognizable filename like `maestro-results.xml`) to disambiguate Maestro, XCTest, Espresso, and TestNG outputs from generic JUnit.

## Configuration

Credentials are stored per identifier in `$XDG_CONFIG_HOME/qualflare/config.toml` (permissions `0600`). Treat this file like an SSH key.

Key environment variables:

| Variable | Description |
|----------|-------------|
| `QF_VERBOSE` | Set to `true` for debug request/response logging |
| `QF_BRANCH` / `QF_COMMIT` | Git context (auto-detected from common CI vars if unset) |

Run `qf --help` for the full list of flags and environment variables.

## Contributing & License

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for dev setup, commit conventions, and the PR checklist.

Licensed under the [Apache License 2.0](LICENSE).
