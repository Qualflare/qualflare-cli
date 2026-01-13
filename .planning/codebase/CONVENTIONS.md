# Coding Conventions

**Analysis Date:** 2026-01-13

## Naming Patterns

**Files:**
- snake_case.go - All Go source files (e.g., report_service.go, factory.go)
- Test files: {name}_test.go alongside source (not currently present)
- Config files: kebab-case or dotfile (Makefile, .goreleaser.yml)

**Functions:**
- Exported: PascalCase for public API (e.g., NewReportService, ParseTestResults)
- Unexported: camelCase for internal (e.g., detectFramework, parseFile)
- Async: No special prefix (Go uses same naming)
- Handlers: Not CLI-specific, use descriptive names (e.g., handleUpload)

**Variables:**
- camelCase for variables (e.g., parserFactory, testSuites)
- UPPER_SNAKE_CASE not used (Go uses iota for constants)
- No underscore prefix for private members

**Types:**
- PascalCase for all types (e.g., ReportService, Parser, Launch)
- Interfaces: PascalCase describing capability (e.g., Parser, ConfigProvider)
- No I prefix on interfaces

## Code Style

**Formatting:**
- Tool: gofmt (standard Go formatting)
- Line length: No strict limit, typically under 100-120
- Quotes: Double quotes for strings, backticks for raw strings
- Semicolons: Not used (Go inserts them)
- Indentation: Tabs (Go standard, displays as 4 spaces)

**Linting:**
- Tool: golangci-lint (available via make lint)
- Rules: Standard Go conventions
- Run: make lint, make vet

## Import Organization

**Order:**
1. Standard library (context, fmt, io, os, time)
2. Third-party packages (github.com/...)
3. Local packages (qualflare-cli/internal/...)

**Grouping:**
- Blank line between groups
- Not explicitly sorted (goimports handles this)

**Path Aliases:**
- No aliases used
- Full import paths always

## Error Handling

**Patterns:**
- Check errors immediately after function calls
- Wrap errors with context using fmt.Errorf and %w
- Custom error types for structured errors (e.g., APIError)
- Always check errors, never ignore

**Error Types:**
- Custom error structs (APIError in http/client.go)
- Error wrapping for context preservation
- Return early on errors (guard pattern)
- Logging at error boundaries, not deep in code

## Logging

**Framework:**
- No external logging framework
- stdout for normal output
- stderr for errors
- fmt.Printf, fmt.Println for output

**Patterns:**
- Log at service/adapters boundaries
- Include context in error messages
- Verbose mode for additional debugging (config.IsVerbose())

## Comments

**When to Comment:**
- Explain non-obvious business logic
- Document why something is done a certain way
- Exported functions should have godoc comments
- Complex algorithms need explanation

**JSDoc/TSDoc:**
- Use Go's godoc format for exported items
- // Comment style for notes
- /* */ block comments for package documentation

**TODO Comments:**
- Format: // TODO: description
- No username tracking (use git blame)
- Not currently present in codebase

## Function Design

**Size:**
- Prefer shorter functions (<50 lines)
- Extract helpers for complex logic
- One level of abstraction per function

**Parameters:**
- Multiple parameters OK in Go (no options object pattern)
- Context typically first parameter for methods
- Use structs for many related parameters

**Return Values:**
- Multiple return values common (result, error)
- Error is always last return value
- Explicit returns, no implicit returns

## Module Design

**Exports:**
- Exported = PascalCase (visible outside package)
- Unexported = camelCase (package private)
- No default exports in Go

**Barrel Files:**
- Not used (Go has explicit import paths)
- Each package imported directly

---

*Convention analysis: 2026-01-13*
*Update when patterns change*
