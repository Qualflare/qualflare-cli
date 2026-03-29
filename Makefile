# Qualflare CLI Makefile

# Build variables
BINARY_NAME := qf
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X qualflare-cli/internal/version.Version=$(VERSION) \
	-X qualflare-cli/internal/version.Commit=$(COMMIT) \
	-X qualflare-cli/internal/version.BuildDate=$(BUILD_DATE)"

# Go variables
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GO := go

# Directories
BUILD_DIR := build
CMD_DIR := cmd
EXAMPLES_DIR := examples

.PHONY: all build clean test lint fmt vet run install help
.PHONY: validate-examples validate-unit validate-bdd validate-e2e validate-api validate-security
.PHONY: validate-junit validate-pytest validate-golang validate-jest validate-mocha validate-rspec validate-phpunit
.PHONY: validate-cucumber validate-karate
.PHONY: validate-playwright validate-cypress validate-selenium validate-testcafe
.PHONY: validate-newman validate-k6
.PHONY: validate-trivy validate-snyk validate-zap validate-sonarqube
.PHONY: collect-examples collect-unit collect-bdd collect-e2e collect-api collect-security
.PHONY: collect-junit collect-pytest collect-golang collect-jest collect-mocha collect-rspec collect-phpunit
.PHONY: collect-cucumber collect-karate
.PHONY: collect-playwright collect-cypress collect-selenium collect-testcafe
.PHONY: collect-newman collect-k6
.PHONY: collect-trivy collect-snyk collect-zap collect-sonarqube

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "Binary built at $(BUILD_DIR)/$(BINARY_NAME)"

# Build for all platforms
build-all: build-linux build-darwin build-windows

build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)

build-darwin:
	@echo "Building for macOS..."
	GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./$(CMD_DIR)
	GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)

build-windows:
	@echo "Building for Windows..."
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	@echo "Tests completed"

# Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated at coverage.html"

# Run short tests only
test-short:
	@echo "Running short tests..."
	$(GO) test -v -short ./...

# Run linter
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "Code formatted"

# Run go vet
vet:
	@echo "Running vet..."
	$(GO) vet ./...

# Run the binary
run: build
	@./$(BUILD_DIR)/$(BINARY_NAME)

# Install the binary to GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GO) install $(LDFLAGS) ./$(CMD_DIR)
	@echo "$(BINARY_NAME) installed to $(shell go env GOPATH)/bin"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Update dependencies
update-deps:
	@echo "Updating dependencies..."
	$(GO) get -u ./...
	$(GO) mod tidy

# Generate mocks (if needed)
generate:
	@echo "Running go generate..."
	$(GO) generate ./...

# Check for security vulnerabilities
security:
	@echo "Checking for vulnerabilities..."
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "govulncheck not installed. Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
	fi

# Pre-commit checks
pre-commit: fmt vet lint test

# Show version info
version:
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"

# Help target
help:
	@echo "Qualflare CLI Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all          Build the binary (default)"
	@echo "  build        Build the binary for current OS/arch"
	@echo "  build-all    Build for all platforms (linux, darwin, windows)"
	@echo "  clean        Remove build artifacts"
	@echo "  test         Run all tests with coverage"
	@echo "  test-short   Run short tests only"
	@echo "  test-coverage Generate HTML coverage report"
	@echo "  lint         Run golangci-lint"
	@echo "  fmt          Format code with go fmt"
	@echo "  vet          Run go vet"
	@echo "  run          Build and run the binary"
	@echo "  install      Install binary to GOPATH/bin"
	@echo "  deps         Download dependencies"
	@echo "  update-deps  Update dependencies"
	@echo "  security     Check for security vulnerabilities"
	@echo "  pre-commit   Run all pre-commit checks"
	@echo "  version      Show version info"
	@echo "  help         Show this help"
	@echo ""
	@echo "Example Validation (local only):"
	@echo "  validate-examples   Validate all example files"
	@echo "  validate-unit       Validate unit test examples"
	@echo "  validate-bdd        Validate BDD examples"
	@echo "  validate-e2e        Validate E2E examples"
	@echo "  validate-api        Validate API examples"
	@echo "  validate-security   Validate security examples"
	@echo "  validate-<framework> Validate specific framework (e.g., validate-junit)"
	@echo ""
	@echo "Example Collect (requires QF_API_KEY, QF_API_ENDPOINT):"
	@echo "  collect-examples     Collect all example files"
	@echo "  collect-unit         Collect unit test examples"
	@echo "  collect-bdd          Collect BDD examples"
	@echo "  collect-e2e          Collect E2E examples"
	@echo "  collect-api          Collect API examples"
	@echo "  collect-security     Collect security examples"
	@echo "  collect-<framework>  Collect specific framework (e.g., collect-junit)"

# =============================================================================
# Example Validation Targets
# =============================================================================

# Validate all examples
validate-examples: build
	@echo "Validating all examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/* $(EXAMPLES_DIR)/bdd/* $(EXAMPLES_DIR)/e2e/* $(EXAMPLES_DIR)/api/* $(EXAMPLES_DIR)/security/*

# Category targets
validate-unit: build
	@echo "Validating unit test examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/*

validate-bdd: build
	@echo "Validating BDD examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/bdd/*

validate-e2e: build
	@echo "Validating E2E examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/e2e/*

validate-api: build
	@echo "Validating API examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/api/*

validate-security: build
	@echo "Validating security examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/security/*

# Unit test framework targets
validate-junit: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/junit-example.xml

validate-pytest: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/pytest-example.xml

validate-golang: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/golang-example.json

validate-jest: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/jest-example.json

validate-mocha: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/mocha-example.json

validate-rspec: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/rspec-example.json

validate-phpunit: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/unit/phpunit-example.xml

# BDD framework targets
validate-cucumber: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/bdd/cucumber-example.json

validate-karate: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/bdd/karate-example.json

# E2E framework targets
validate-playwright: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/e2e/playwright-example.json

validate-cypress: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/e2e/cypress-example.json

validate-selenium: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/e2e/selenium-example.json

validate-testcafe: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/e2e/testcafe-example.json

# API framework targets
validate-newman: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/api/newman-example.json

validate-k6: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/api/k6-example.json

# Security framework targets
validate-trivy: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/security/trivy-example.json

validate-snyk: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/security/snyk-example.json

validate-zap: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/security/zap-example.json

validate-sonarqube: build
	@./$(BUILD_DIR)/$(BINARY_NAME) validate $(EXAMPLES_DIR)/security/sonarqube-example.json

# =============================================================================
# Example Collect Targets (requires QF_API_KEY, QF_API_ENDPOINT)
# =============================================================================

# Collect all examples
collect-examples: build
	@echo "Collecting all examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/* $(EXAMPLES_DIR)/bdd/* $(EXAMPLES_DIR)/e2e/* $(EXAMPLES_DIR)/api/* $(EXAMPLES_DIR)/security/*

# Category targets
collect-unit: build
	@echo "Collecting unit test examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/*

collect-bdd: build
	@echo "Collecting BDD examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/bdd/*

collect-e2e: build
	@echo "Collecting E2E examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/e2e/*

collect-api: build
	@echo "Collecting API examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/api/*

collect-security: build
	@echo "Collecting security examples..."
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/security/*

# Unit test framework targets
collect-junit: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/junit-example.xml

collect-pytest: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/pytest-example.xml

collect-golang: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/golang-example.json

collect-jest: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/jest-example.json

collect-mocha: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/mocha-example.json

collect-rspec: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/rspec-example.json

collect-phpunit: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/unit/phpunit-example.xml

# BDD framework targets
collect-cucumber: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/bdd/cucumber-example.json

collect-karate: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/bdd/karate-example.json

# E2E framework targets
collect-playwright: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/e2e/playwright-example.json

collect-cypress: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/e2e/cypress-example.json

collect-selenium: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/e2e/selenium-example.json

collect-testcafe: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/e2e/testcafe-example.json

# API framework targets
collect-newman: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/api/newman-example.json

collect-k6: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/api/k6-example.json

# Security framework targets
collect-trivy: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/security/trivy-example.json

collect-snyk: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/security/snyk-example.json

collect-zap: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/security/zap-example.json

collect-sonarqube: build
	@./$(BUILD_DIR)/$(BINARY_NAME) collect $(EXAMPLES_DIR)/security/sonarqube-example.json
