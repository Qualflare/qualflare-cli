# Codebase Concerns

**Analysis Date:** 2026-01-13

## Tech Debt

**Missing Test Suite:**
- Issue: No test files exist in the entire codebase
- Files: No *_test.go files anywhere
- Why: Not prioritized during initial development
- Impact: High risk of regressions, difficult to refactor safely
- Fix approach: Implement comprehensive test suite starting with critical paths (parsers, services)

**Large Files:**
- Issue: Several files exceed 250 lines
- Files:
  - internal/adapters/cli/command.go (383 lines)
  - internal/adapters/parsers/factory/factory.go (358 lines)
  - internal/adapters/http/client.go (291 lines)
  - internal/core/domain/models.go (295 lines)
- Why: Organic growth without refactoring
- Impact: Difficult to navigate, harder to test
- Fix approach: Split into smaller focused modules

**Missing Environment Configuration Template:**
- Issue: No .env.example file despite using environment variables
- Files: Missing from root directory
- Why: Not documented during development
- Impact: Difficult for developers to understand required configuration
- Fix approach: Add .env.example with all QF_* variables documented

**Code Duplication in Parsers:**
- Issue: Similar XML parsing patterns repeated across parsers
- Files: internal/adapters/parsers/unit/junit/, internal/adapters/parsers/unit/pytest/, and others
- Why: Each parser independently implements similar logic
- Impact: Maintenance burden, inconsistent bug fixes
- Fix approach: Extract common XML parsing utilities to shared package

## Known Bugs

**No known bugs documented**

## Security Considerations

**API Request Logging:**
- Risk: Full request body logged including potential sensitive data
- Files: internal/adapters/http/client.go (around line 83)
- Current mitigation: None - full body logged in verbose mode
- Recommendations: Redact or truncate sensitive fields in logs

**API Key Validation:**
- Risk: API key passed without validation
- Files: internal/config/config.go
- Current mitigation: None - API key used as-is
- Recommendations: Add basic format validation for API keys

**Default Development Endpoint:**
- Risk: Default endpoint points to localhost (127.0.0.1:8001)
- Files: internal/config/config.go
- Current mitigation: Requires explicit QF_API_ENDPOINT for production
- Recommendations: Make default explicit or require configuration

## Performance Bottlenecks

**Sequential File Processing:**
- Problem: Each file parsed sequentially without batching
- Files: internal/core/services/report_service.go
- Measurement: Not measured
- Cause: Sequential loop over file paths
- Improvement path: Process files concurrently with goroutines

**Full File Loading:**
- Problem: Large files loaded entirely into memory
- Files: All parser implementations
- Measurement: 1MB limit in HTTP client response reading
- Cause: JSON/XML unmarshaling requires full document
- Improvement path: Consider streaming parsers for very large files

**No Caching:**
- Problem: No caching of parsed results or API responses
- Files: N/A (system-wide)
- Measurement: N/A
- Cause: Stateless design
- Improvement path: Not applicable for CLI use case

## Fragile Areas

**Parser Factory:**
- Why fragile: All parsers registered in one large function
- Files: internal/adapters/parsers/factory/factory.go
- Common failures: Missing parser registration, import failures
- Safe modification: Add tests for each parser registration
- Test coverage: No tests (critical gap)

**Framework Detection:**
- Why fragile: File extension-based detection can be ambiguous
- Files: internal/adapters/parsers/factory/factory.go (DetectFramework methods)
- Common failures: Multiple parsers support same extension
- Safe modification: Add content-based detection as fallback
- Test coverage: No tests

**Error Code Mapping:**
- Why fragile: API error codes hardcoded in switch statement
- Files: internal/adapters/http/client.go
- Common failures: New error codes not handled
- Safe modification: Make error code mapping data-driven
- Test coverage: No tests

## Scaling Limits

**Not applicable** - CLI tool with no persistent state

## Dependencies at Risk

**No dependencies at risk detected** - Minimal dependencies, all actively maintained

## Missing Critical Features

**Test Coverage:**
- Problem: No automated tests
- Current workaround: Manual testing with examples/
- Blocks: Safe refactoring, confident releases
- Implementation complexity: Medium (requires test infrastructure setup)

**Documentation:**
- Problem: No README.md in repository
- Current workaround: Code inspection
- Blocks: Easy onboarding for contributors
- Implementation complexity: Low

**.env.example:**
- Problem: No template for environment configuration
- Current workaround: Reading source code
- Blocks: Easy local development setup
- Implementation complexity: Low

## Test Coverage Gaps

**Entire Codebase:**
- What's not tested: Everything
- Risk: Regressions, undetected bugs
- Priority: Critical
- Difficulty to test: Medium - need to design test structure

**Parsers:**
- what's not tested: All 16+ framework parsers
- Risk: Parser regressions breaking framework support
- Priority: High
- Difficulty to test: Low - have example files ready

**HTTP Client:**
- What's not tested: Retry logic, error handling, request formatting
- Risk: API communication failures
- Priority: High
- Difficulty to test: Medium - need mock HTTP server

**CLI Commands:**
- What's not tested: Argument parsing, command routing
- Risk: UX issues
- Priority: Medium
- Difficulty to test: Low - standard CLI testing patterns

## Positive Observations

**Well-Structured Architecture:**
- Clean separation of concerns with ports/adapters pattern
- Good use of interfaces for dependency inversion
- Domain layer isolated from external dependencies

**Proper Error Handling:**
- Structured error types (APIError)
- Error wrapping with context
- User-friendly error messages

**No Hardcoded Secrets:**
- Configuration via environment variables
- No sensitive data in source code

**Consistent Code Style:**
- Follows Go conventions
- Clean naming patterns
- Good organization

---

*Concerns audit: 2026-01-13*
*Update as issues are fixed or new ones discovered*
