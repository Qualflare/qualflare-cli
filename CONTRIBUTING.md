# Contributing to Qualflare CLI

Thank you for your interest in contributing! This document covers how to set up your environment, submit changes, and get them reviewed.

## Prerequisites

- Go 1.25+
- Make
- [golangci-lint](https://golangci-lint.run/usage/install/) (for linting)
- [GoReleaser](https://goreleaser.com/install/) (for release testing only)

## Development Setup

```bash
git clone https://github.com/qualflare/qualflare-cli.git
cd qualflare-cli

# Build the binary
make build                 # output: build/qf

# Run tests (includes -race detector)
make test

# Run linter
make lint

# Run vulnerability check
make security

# Cross-platform builds
make build-all
```

## Making Changes

1. **Fork** the repository and create a feature branch:

   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Write your change.** Keep commits focused and atomic.

3. **Add or update tests** for any changed behaviour. Parser changes should include a fixture file under `examples/`.

4. **Run the full check suite** before pushing:

   ```bash
   make test && make lint && make security
   ```

5. **Open a pull request** against `main`. Fill in the PR template.

## Commit Style

This project uses [Conventional Commits](https://www.conventionalcommits.org/). The goreleaser changelog is generated from commit messages, so please follow the format:

```
feat: add support for JUnit 5 nested suites
fix: handle missing timestamp in Playwright output
docs: update Homebrew install instructions
refactor: simplify framework detection logic
test: add edge-case fixtures for Mocha reporter
ci: pin govulncheck version
```

Breaking changes should include `!` after the type, e.g. `feat!: rename --api-key flag`.

## Adding a New Parser

1. Create a new package under `internal/adapters/parsers/<category>/<framework>/`.
   - Valid categories: `generic`, `unit`, `bdd`, `e2e`, `api`, `security`.
   - If your framework emits standard JUnit XML, use the shared helper: `internal/adapters/parsers/shared/junitxml`. Import it and call `junitxml.Parse(reader, domain.FrameworkYours)` — no XML decoding needed in your package.
2. Implement the `ports.Parser` interface (`Parse(io.Reader) (*domain.Suite, error)`, `GetFramework() domain.Framework`, `SupportedFileExtensions() []string`).
3. Register the parser in `internal/adapters/parsers/factory/factory.go`.
4. Add the framework constant to `internal/core/domain/models.go` (`AllFrameworks`, `GetCategory`).
5. Add the name to `internal/auth/identifier.go:reservedNames` only if it matches a top-level CLI subcommand name.
6. Add a fixture file under `examples/<category>/<framework>-example.<ext>` and wire up `make validate-<framework>` and `make collect-<framework>` targets in the `Makefile`.
7. Write a `_test.go` file that covers at minimum: a happy-path parse, an empty file, and a malformed file.

