.PHONY: build build-linux build-linux-amd64 build-linux-arm64 test clean install fmt lint

BINARY_NAME = vmkit-agent
# vk-nzp: derive the version from the nearest git tag so `vmkit-agent version`
# reports something useful in every build path (dev, manual release, CI).
# Append `-dirty` if there are uncommitted changes, and fall back to `dev`
# outside a git checkout (e.g. when the source tarball is extracted on a VM).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DIR = bin

LDFLAGS = -ldflags "-X main.Version=$(VERSION)"

build:
	@echo "Building $(BINARY_NAME) version=$(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/vmkit-agent

build-linux: build-linux-amd64 build-linux-arm64
	@cp $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(BUILD_DIR)/$(BINARY_NAME)-linux
	@echo "Linux binaries ready: $(BUILD_DIR)/$(BINARY_NAME)-linux-{amd64,arm64} (version=$(VERSION))"

build-linux-amd64:
	@echo "Building $(BINARY_NAME)-linux-amd64 version=$(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build \
		$(LDFLAGS) \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 \
		./cmd/vmkit-agent

build-linux-arm64:
	@echo "Building $(BINARY_NAME)-linux-arm64 version=$(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build \
		$(LDFLAGS) \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 \
		./cmd/vmkit-agent

test:
	@echo "Running tests..."
	go test -v ./...

test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

install:
	@echo "Installing $(BINARY_NAME)..."
	go install $(LDFLAGS) ./cmd/vmkit-agent

fmt:
	@echo "Formatting code..."
	go fmt ./...

lint:
	@echo "Linting code..."
	golangci-lint run ./...

.DEFAULT_GOAL := build
