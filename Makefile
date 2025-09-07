# Variables
BINARY_NAME=acc
GO=go
GOFLAGS=-v
LDFLAGS=-s -w
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "v1.0.0")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Build variables
BUILD_DIR=build
DIST_DIR=dist

# Platforms for cross-compilation
PLATFORMS=darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

# Default target
.DEFAULT_GOAL := build

# Build the binary
.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	@$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS) -X main.version=$(VERSION)" -o $(BINARY_NAME) main.go
	@echo "Build complete: ./$(BINARY_NAME)"

# Install the binary to $GOPATH/bin
.PHONY: install
install:
	@echo "Installing $(BINARY_NAME)..."
	@$(GO) install $(GOFLAGS) -ldflags "$(LDFLAGS) -X main.version=$(VERSION)"
	@echo "Installed to $$(go env GOPATH)/bin/$(BINARY_NAME)"

# Run the application
.PHONY: run
run:
	@$(GO) run main.go extract

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)
	@rm -rf $(DIST_DIR)
	@rm -f coverage.out
	@echo "Clean complete"

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	@$(GO) test -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	@$(GO) test -v -cover -coverprofile=coverage.out ./...
	@$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	@$(GO) fmt ./...
	@echo "Format complete"

# Lint code
.PHONY: lint
lint:
	@echo "Linting code..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install it with:"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

# Run go vet
.PHONY: vet
vet:
	@echo "Running go vet..."
	@$(GO) vet ./...

# Download dependencies
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	@$(GO) mod download
	@$(GO) mod tidy
	@echo "Dependencies downloaded"

# Update dependencies
.PHONY: update-deps
update-deps:
	@echo "Updating dependencies..."
	@$(GO) get -u ./...
	@$(GO) mod tidy
	@echo "Dependencies updated"

# Build for all platforms
.PHONY: build-all
build-all:
	@echo "Building for all platforms..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		output=$(DIST_DIR)/$(BINARY_NAME)-$${platform%/*}-$${platform#*/}; \
		if [ "$${platform%/*}" = "windows" ]; then output="$${output}.exe"; fi; \
		echo "Building for $${platform}..."; \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} $(GO) build \
			-ldflags "$(LDFLAGS) -X main.version=$(VERSION)" \
			-o $${output} main.go; \
	done
	@echo "Cross-platform build complete"

# Create release archives
.PHONY: release
release: clean build-all
	@echo "Creating release archives..."
	@cd $(DIST_DIR) && for file in *; do \
		if [ -f "$${file}" ]; then \
			tar czf "$${file}.tar.gz" "$${file}"; \
			rm "$${file}"; \
		fi; \
	done
	@echo "Release archives created in $(DIST_DIR)/"

# Development build with race detector
.PHONY: dev
dev:
	@echo "Building with race detector..."
	@$(GO) build -race -o $(BINARY_NAME)-dev main.go
	@echo "Development build complete: ./$(BINARY_NAME)-dev"

# Check for security vulnerabilities
.PHONY: security
security:
	@echo "Checking for vulnerabilities..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "gosec not installed. Install it with:"; \
		echo "  go install github.com/securego/gosec/v2/cmd/gosec@latest"; \
	fi

# Generate documentation
.PHONY: docs
docs:
	@echo "Generating documentation..."
	@$(GO) doc -all > API.md
	@echo "Documentation generated: API.md"

# Show help
.PHONY: help
help:
	@echo "AI Code Context Extractor - Makefile Commands"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build          Build the binary for current platform"
	@echo "  install        Install the binary to \$$GOPATH/bin"
	@echo "  run            Run the application"
	@echo "  clean          Remove build artifacts"
	@echo "  test           Run tests"
	@echo "  test-coverage  Run tests with coverage report"
	@echo "  fmt            Format code"
	@echo "  lint           Lint code (requires golangci-lint)"
	@echo "  vet            Run go vet"
	@echo "  deps           Download dependencies"
	@echo "  update-deps    Update dependencies"
	@echo "  build-all      Build for all platforms"
	@echo "  release        Create release archives"
	@echo "  dev            Build with race detector"
	@echo "  security       Check for vulnerabilities (requires gosec)"
	@echo "  docs           Generate documentation"
	@echo "  help           Show this help message"

# Quick test build and run
.PHONY: quick
quick: build
	@echo ""
	@echo "Testing the build..."
	@./$(BINARY_NAME) extract --path . --format markdown | head -50
	@echo ""
	@echo "... (output truncated)"