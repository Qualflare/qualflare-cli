# Architecture

**Analysis Date:** 2026-01-13

## Pattern Overview

**Overall:** Clean Architecture (Hexagonal/Ports & Adapters)

**Key Characteristics:**
- Dependency inversion - Core layer doesn't depend on external libraries
- Interface-based design - Ports define contracts, adapters implement
- Domain-centric - Business logic isolated from infrastructure
- Single responsibility - Each layer has clear purpose

## Layers

**Domain Layer:**
- Purpose: Core business logic and domain models
- Contains: Test result models, framework definitions, domain types
- Location: internal/core/domain/models.go
- Depends on: Go standard library only
- Used by: All layers (domain is the center)

**Ports Layer:**
- Purpose: Interface definitions for external interactions
- Contains: Parser, ReportSender, ConfigProvider, Logger interfaces
- Location: internal/core/ports/interfaces.go
- Depends on: Domain layer
- Used by: Adapters (implement these interfaces)

**Services Layer:**
- Purpose: Business logic orchestration
- Contains: ReportService (test result processing workflow)
- Location: internal/core/services/report_service.go
- Depends on: Ports (interfaces) and Domain
- Used by: CLI adapter

**Adapters Layer:**
- Purpose: External interface implementations
- Contains: CLI commands, HTTP client, framework parsers
- Location: internal/adapters/
- Depends on: Ports (implements interfaces)
- Used by: Application entry point

**Infrastructure Layer:**
- Purpose: Configuration and cross-cutting concerns
- Contains: Config management, version info
- Location: internal/config/, internal/version/
- Depends on: Go standard library
- Used by: All layers

## Data Flow

**CLI Command Execution:**

1. User runs: `qf upload results.xml --project my-app`
2. Cobra parses args and flags (cmd/main.go)
3. Config initialized from environment + flags (internal/config/config.go)
4. ParserFactory detects framework or uses specified one (internal/adapters/parsers/factory/)
5. ReportService orchestrates parsing (internal/core/services/report_service.go)
6. Parser reads file and produces domain model (internal/adapters/parsers/*)
7. HTTP client sends JSON to Qualflare API (internal/adapters/http/client.go)
8. Result returned via exit code and stdout/stderr

**State Management:**
- Stateless - No persistent state
- Each command execution is independent
- Configuration via environment variables only

## Key Abstractions

**Parser:**
- Purpose: Convert test result files to domain model
- Examples: internal/adapters/parsers/unit/junit/, internal/adapters/parsers/e2e/playwright/
- Pattern: Each framework has dedicated parser implementing Parser interface

**ParserFactory:**
- Purpose: Framework detection and parser instantiation
- Location: internal/adapters/parsers/factory/factory.go
- Pattern: Factory with registry pattern

**ReportService:**
- Purpose: Orchestrate test result processing workflow
- Location: internal/core/services/report_service.go
- Pattern: Service layer with dependency injection

**ReportSender (HTTP Client):**
- Purpose: Communicate with Qualflare API
- Location: internal/adapters/http/client.go
- Pattern: Adapter with retry logic and error handling

**ConfigProvider:**
- Purpose: Configuration management
- Location: internal/config/config.go
- Pattern: Provider interface with environment variable backing

## Entry Points

**CLI Entry:**
- Location: cmd/main.go
- Triggers: User runs `qf` command
- Responsibilities: Dependency injection, Cobra initialization, command registration

**CLI Commands:**
- Location: internal/adapters/cli/command.go
- Triggers: Matched subcommand (upload, validate, etc.)
- Responsibilities: Input validation, service invocation, output formatting

## Error Handling

**Strategy:** Custom error types with structured information

**Patterns:**
- APIError wrapper for HTTP failures (internal/adapters/http/client.go)
- Error wrapping with fmt.Errorf and %w
- Retry logic for transient failures
- User-friendly error messages for known error codes

## Cross-Cutting Concerns

**Logging:**
- stdout for normal output
- stderr for errors
- No external logging framework

**Validation:**
- Framework validation in parser factory
- File existence checks before parsing
- API response validation

**Authentication:**
- API key via QF_TOKEN header
- No session management (stateless)

**Retry Logic:**
- Exponential backoff with jitter
- Configurable max retries, base delay, max delay
- Retry-after header support for rate limiting

---

*Architecture analysis: 2026-01-13*
*Update when major patterns change*
