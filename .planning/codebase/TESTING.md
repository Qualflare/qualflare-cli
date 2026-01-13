# Testing Patterns

**Analysis Date:** 2026-01-13

## Test Framework

**Runner:**
- Not detected - No formal testing framework currently in use

**Assertion Library:**
- Not applicable - No test framework

**Run Commands:**
```bash
make test              # Run all tests (currently no tests)
make test-coverage     # Coverage report (via go test -cover)
```

## Test File Organization

**Location:**
- Not established - No test files exist
- Expected pattern: *_test.go alongside source files (Go convention)

**Naming:**
- source_test.go - Go convention for test files
- Not currently present in codebase

**Structure:**
```
internal/
  core/
    services/
      report_service.go
      (report_service_test.go - NOT PRESENT)
    domain/
      models.go
      (models_test.go - NOT PRESENT)
  adapters/
    cli/
      command.go
      (command_test.go - NOT PRESENT)
```

## Test Structure

**Suite Organization:**
- Not established - No tests exist

**Patterns:**
- Not applicable

## Mocking

**Framework:**
- Not detected

**Patterns:**
- Not applicable

**What to Mock:**
- HTTP client calls (when testing ReportService)
- File system reads (when testing parsers)
- Environment variables (when testing config)

**What NOT to Mock:**
- Not applicable

## Fixtures and Factories

**Test Data:**
- examples/ directory contains sample test results for each framework
- Used for manual validation and parser development
- Not automated into test suite

**Location:**
- examples/unit/ - Unit test framework examples
- examples/e2e/ - E2E framework examples
- examples/api/ - API testing examples
- examples/security/ - Security tool examples

## Coverage

**Requirements:**
- No enforced coverage target

**Configuration:**
- Via go test -coverprofile=coverage.out in Makefile
- HTML report available via make test-coverage

**View Coverage:**
```bash
make test-coverage    # Generate coverage.out
go tool cover -html=coverage.out  # View HTML report
```

## Test Types

**Unit Tests:**
- Not present - Need to be added
- Should test: parsers, services, config

**Integration Tests:**
- Not present - Need to be added
- Should test: HTTP client with mock server, full workflow

**E2E Tests:**
- Not present
- Manual testing with examples/ currently

## Common Patterns

**Not Applicable:**
- No test patterns exist in codebase

## Recommended Testing Structure

**To Be Implemented:**

```
internal/
  core/
    services/
      report_service_test.go
    domain/
      models_test.go
    ports/
      interfaces_test.go (if needed)
  adapters/
    cli/
      command_test.go
    http/
      client_test.go
    parsers/
      factory/
        factory_test.go
      unit/
        junit/
          junit_test.go
        # ... other parser tests
    config/
      config_test.go
```

**Test Utilities to Create:**
- internal/testutil/ - Test helpers and fixtures
- internal/testutil/mocks/ - Mock implementations

**Test Data Strategy:**
- Use existing examples/ files as test fixtures
- Create additional edge case examples
- Mock HTTP responses for API testing

---

*Testing analysis: 2026-01-13*
*Update when test patterns change*
