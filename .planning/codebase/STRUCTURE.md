# Codebase Structure

**Analysis Date:** 2026-01-13

## Directory Layout

```
qualflare-cli/
├── cmd/                  # Application entry points
├── internal/             # Internal application code
│   ├── adapters/        # External interface implementations
│   │   ├── cli/         # CLI command handlers
│   │   ├── http/        # HTTP client for API communication
│   │   └── parsers/     # Test result framework parsers
│   │       ├── factory/ # Parser factory and detection
│   │       ├── unit/    # Unit test framework parsers
│   │       ├── bdd/     # BDD framework parsers
│   │       ├── e2e/     # E2E framework parsers
│   │       ├── api/     # API testing framework parsers
│   │       └── security/# Security tool parsers
│   ├── config/          # Configuration management
│   ├── core/            # Core business logic
│   │   ├── domain/      # Domain models
│   │   ├── ports/       # Interface definitions
│   │   └── services/    # Business logic services
│   └── version/         # Version information
├── examples/            # Example test result files
├── docs/                # Documentation
├── build/               # Compiled binaries
├── .github/             # GitHub Actions workflows
├── go.mod               # Go module definition
├── go.sum               # Go module checksums
├── Makefile             # Build automation
└── .goreleaser.yml      # Release configuration
```

## Directory Purposes

**cmd/**
- Purpose: Application entry point
- Contains: main.go
- Key files: main.go - dependency injection setup, Cobra initialization
- Subdirectories: None

**internal/adapters/**
- Purpose: Implement external interfaces
- Contains: CLI, HTTP, and parser implementations
- Key files: cli/command.go, http/client.go, parsers/factory/factory.go
- Subdirectories: cli/, http/, parsers/

**internal/config/**
- Purpose: Configuration management
- Contains: Config struct and environment variable handling
- Key files: config.go - defines Config struct with environment loading
- Subdirectories: None

**internal/core/**
- Purpose: Core business logic
- Contains: Domain models, interfaces, and services
- Key files: domain/models.go, ports/interfaces.go, services/report_service.go
- Subdirectories: domain/, ports/, services/

**internal/core/domain/**
- Purpose: Domain models and types
- Contains: Framework definitions, test result structures
- Key files: models.go - all domain types
- Subdirectories: None

**internal/core/ports/**
- Purpose: Interface definitions
- Contains: Parser, ReportSender, ConfigProvider, Logger interfaces
- Key files: interfaces.go - all port interfaces
- Subdirectories: None

**internal/core/services/**
- Purpose: Business logic orchestration
- Contains: ReportService implementation
- Key files: report_service.go
- Subdirectories: None

**internal/adapters/parsers/**
- Purpose: Framework-specific test result parsers
- Contains: Parser implementations organized by category
- Key files: factory/factory.go - parser registration and detection
- Subdirectories: factory/, unit/, bdd/, e2e/, api/, security/

**examples/**
- Purpose: Example test result files for each supported framework
- Contains: JSON and XML files representing various test outputs
- Key files: junit-example.xml, playwright-example.json, etc.
- Subdirectories: unit/, bdd/, e2e/, api/, security/

**docs/**
- Purpose: Project documentation
- Contains: Framework schema documentation
- Key files: framework-output-schema.md
- Subdirectories: None

**build/**
- Purpose: Compiled binary output
- Contains: Built qf executable
- Key files: qf
- Subdirectories: None
- Note: Generated directory, not in source control

**.github/workflows/**
- Purpose: CI/CD automation
- Contains: GitHub Actions workflows
- Key files: ci.yml, release.yml
- Subdirectories: None

## Key File Locations

**Entry Points:**
- cmd/main.go - CLI entry point and dependency injection

**Configuration:**
- go.mod - Go module and dependencies
- internal/config/config.go - Application configuration
- .goreleaser.yml - Release configuration
- Makefile - Build automation

**Core Logic:**
- internal/core/services/report_service.go - Business logic
- internal/core/domain/models.go - Domain models
- internal/core/ports/interfaces.go - Interface definitions
- internal/adapters/cli/command.go - CLI commands
- internal/adapters/http/client.go - HTTP client
- internal/adapters/parsers/factory/factory.go - Parser factory

**Testing:**
- No test files currently exist
- examples/ contains test data for validation

**Documentation:**
- docs/framework-output-schema.md - JSON schemas
- README.md - Not present (missing)

## Naming Conventions

**Files:**
- snake_case.go - All Go source files
- kebab-case - Directory names
- UPPERCASE - Makefile targets

**Directories:**
- kebab-case - All directories
- Plural for collections - adapters/, parsers/, services/
- Singular for single-purpose - config/, domain/, ports/

**Special Patterns:**
- *test.go - Go test files (not currently present)
- factory/ - Factory pattern implementations
- internal/ - Private application code (Go convention)

## Where to Add New Code

**New Framework Parser:**
- Primary code: internal/adapters/parsers/{category}/{framework}/
- Tests: {framework}_test.go (co-located with parser)
- Registration: Add to internal/adapters/parsers/factory/factory.go

**New CLI Command:**
- Implementation: internal/adapters/cli/command.go
- Tests: command_test.go (co-located)

**New Service:**
- Implementation: internal/core/services/{service}.go
- Interfaces: Add to internal/core/ports/interfaces.go
- Types: Add to internal/core/domain/models.go

**New Configuration:**
- Implementation: Add field to Config struct in internal/config/config.go
- Environment loading: Add to LoadFromEnv() method
- CLI flags: Add in internal/adapters/cli/command.go

**Utilities:**
- Shared helpers: internal/{package}/util.go or similar
- HTTP utilities: internal/adapters/http/
- File utilities: internal/adapters/parsers/factory/

## Special Directories

**build/**
- Purpose: Compiled binary output
- Source: Generated via `make build`
- Committed: No (in .gitignore)

**examples/**
- Purpose: Test result examples for each framework
- Source: Manually curated sample files
- Committed: Yes

**internal/**
- Purpose: Private application code (Go convention)
- Source: All application source code
- Committed: Yes

---

*Structure analysis: 2026-01-13*
*Update when directory structure changes*
