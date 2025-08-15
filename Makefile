# Makefile for fleeting-plugin-incus

# Version information
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_INFO := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Go build flags
LDFLAGS := -X 'fleeting-plugin-incus.BuildInfo=$(BUILD_INFO)' \
           -X 'fleeting-plugin-incus.BuildDate=$(BUILD_DATE)' \
           -X 'fleeting-plugin-incus.GitCommit=$(GIT_COMMIT)'

# Build directory
BUILD_DIR := build
BINARY_NAME := fleeting-plugin-incus

.PHONY: all build clean install test version help

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/fleeting-plugin-incus

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)

# Install to system
install: build
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/

# Run tests
test:
	go test -v ./...

# Development build (without version info)
dev:
	@echo "Building development version..."
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/fleeting-plugin-incus

# Show help
help:
	@echo "Available targets:"
	@echo "  build    - Build the binary with version information"
	@echo "  dev      - Build development version (faster, no version info)"
	@echo "  clean    - Clean build artifacts"
	@echo "  install  - Install binary to /usr/local/bin"
	@echo "  version  - Show version information"
	@echo "  help     - Show this help"
