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

.PHONY: all build clean test lint fmt vet run install help

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
