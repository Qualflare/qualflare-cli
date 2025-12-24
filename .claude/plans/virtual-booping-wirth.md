# Makefile Commands for Example Files

## Summary

Add make commands to run the CLI tool with example report files. Create separated commands for each framework category and individual examples.

---

## File to Modify

`Makefile` - Add new targets at line ~163 (after help target)

---

## Commands to Add

### Validate All Examples
```makefile
validate-examples: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate examples/**/*
```

### Category Commands
```makefile
validate-unit: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate examples/unit/*

validate-bdd: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate examples/bdd/*

validate-e2e: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate examples/e2e/*

validate-api: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate examples/api/*

validate-security: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate examples/security/*
```

### Individual Framework Commands

**Unit:**
- `validate-junit`, `validate-pytest`, `validate-golang`, `validate-jest`, `validate-mocha`, `validate-rspec`, `validate-phpunit`

**BDD:**
- `validate-cucumber`, `validate-karate`

**E2E:**
- `validate-playwright`, `validate-cypress`, `validate-selenium`, `validate-testcafe`

**API:**
- `validate-newman`, `validate-k6`

**Security:**
- `validate-trivy`, `validate-snyk`, `validate-zap`, `validate-sonarqube`

---

## Implementation Steps

1. Add `EXAMPLES_DIR := examples` variable
2. Add `.PHONY` declarations for all new targets
3. Add `validate-examples` target to validate all
4. Add category targets (unit, bdd, e2e, api, security)
5. Add individual framework targets (19 total)
6. Update `help` target to show new commands

---

## Example Files Mapping

| Framework | File |
|-----------|------|
| junit | examples/unit/junit-example.xml |
| pytest | examples/unit/pytest-example.xml |
| golang | examples/unit/golang-example.json |
| jest | examples/unit/jest-example.json |
| mocha | examples/unit/mocha-example.json |
| rspec | examples/unit/rspec-example.json |
| phpunit | examples/unit/phpunit-example.xml |
| cucumber | examples/bdd/cucumber-example.json |
| karate | examples/bdd/karate-example.json |
| playwright | examples/e2e/playwright-example.json |
| cypress | examples/e2e/cypress-example.json |
| selenium | examples/e2e/selenium-example.json |
| testcafe | examples/e2e/testcafe-example.json |
| newman | examples/api/newman-example.json |
| k6 | examples/api/k6-example.json |
| trivy | examples/security/trivy-example.json |
| snyk | examples/security/snyk-example.json |
| zap | examples/security/zap-example.json |
| sonarqube | examples/security/sonarqube-example.json |