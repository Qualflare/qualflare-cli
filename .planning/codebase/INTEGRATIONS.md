# External Integrations

**Analysis Date:** 2026-01-13

## APIs & External Services

**Qualflare API:**
- Primary integration - Test result collection and analysis
  - SDK/Client: Custom HTTP client (internal/adapters/http/client.go)
  - Auth: QF_TOKEN header with API key from QF_API_KEY env var
  - Endpoints used: /api/v1/collect (configurable via QF_API_ENDPOINT)
  - Default: http://127.0.0.1:8001/api/v1/collect
  - Retry logic: Exponential backoff with jitter, max 3 retries
  - Supported error codes: ENVIRONMENT_NOT_FOUND, MILESTONE_NOT_FOUND, VALIDATION_FAILED

## Data Storage

**Databases:**
- Not detected - CLI tool with no persistent storage

**File Storage:**
- Not detected - Stateless CLI tool

**Caching:**
- Not detected - No caching implemented

## Authentication & Identity

**Auth Provider:**
- None - Stateless CLI with API key authentication

**OAuth Integrations:**
- None

## Monitoring & Observability

**Error Tracking:**
- None - Error logging to stderr/stdout only

**Analytics:**
- None

**Logs:**
- stdout/stderr - No external logging service

## CI/CD & Deployment

**Hosting:**
- GitHub Releases - Binary distribution
  - Deployment: Automatic via GoReleaser on git tag
  - Environment vars: Configured locally per environment

**CI Pipeline:**
- GitHub Actions - .github/workflows/ci.yml, .github/workflows/release.yml
  - Workflows: CI tests, release automation
  - Secrets: GitHub repository secrets

**Package Distribution:**
- Homebrew Tap: qualflare/homebrew-tap
- Docker Registry: ghcr.io/qualflare/qf
- GitHub Releases: Source for binary downloads

## Environment Configuration

**Development:**
- Required env vars: QF_API_KEY, QF_API_ENDPOINT
- Optional env vars: QF_ENVIRONMENT, QF_BRANCH, QF_COMMIT
- Secrets location: Environment variables only
- Mock/stub services: Default localhost endpoint (127.0.0.1:8001)

**Staging:**
- Not applicable - No staging environment defined

**Production:**
- Secrets management: Environment variables
- Failover/redundancy: Not applicable (CLI tool)

## Webhooks & Callbacks

**Incoming:**
- None

**Outgoing:**
- HTTP POST to Qualflare API - Test result submission

## Framework Parsers

The CLI integrates with 16+ testing frameworks through dedicated parsers:

**Unit Testing:**
- JUnit (XML) - internal/adapters/parsers/unit/junit/
- Pytest (XML) - internal/adapters/parsers/unit/pytest/
- Go Test (JSON) - internal/adapters/parsers/unit/golang/
- Jest (JSON) - internal/adapters/parsers/unit/jest/
- Mocha (JSON) - internal/adapters/parsers/unit/mocha/
- RSpec (JSON) - internal/adapters/parsers/unit/rspec/
- PHPUnit (XML) - internal/adapters/parsers/unit/phpunit/

**BDD Frameworks:**
- Cucumber (JSON) - internal/adapters/parsers/bdd/cucumber/
- Karate (JSON) - internal/adapters/parsers/bdd/karate/

**E2E Frameworks:**
- Playwright (JSON) - internal/adapters/parsers/e2e/playwright/
- Cypress (JSON) - internal/adapters/parsers/e2e/cypress/
- Selenium (JSON) - internal/adapters/parsers/e2e/selenium/
- TestCafe (JSON) - internal/adapters/parsers/e2e/testcafe/

**API Testing:**
- Newman (JSON) - internal/adapters/parsers/api/newman/
- k6 (JSON) - internal/adapters/parsers/api/k6/

**Security Tools:**
- Trivy (JSON) - internal/adapters/parsers/security/trivy/
- Snyk (JSON) - internal/adapters/parsers/security/snyk/
- OWASP ZAP (JSON) - internal/adapters/parsers/security/zap/
- SonarQube (JSON) - internal/adapters/parsers/security/sonarqube/

## Schema Documentation

- Framework Output Schema: docs/framework-output-schema.md
  - Comprehensive JSON schemas for all supported frameworks

---

*Integration audit: 2026-01-13*
*Update when adding/removing external services*
