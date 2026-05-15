.PHONY: build build-linux test clean install

BINARY_NAME=vmkit-agent
VERSION?=0.14.0
BUILD_DIR=bin

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/vmkit-agent

build-linux:
	@echo "Building $(BINARY_NAME) for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build \
		-ldflags "-X main.Version=$(VERSION)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 \
		./cmd/vmkit-agent
	@cp $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(BUILD_DIR)/$(BINARY_NAME)-linux
	@echo "Linux binary ready at $(BUILD_DIR)/$(BINARY_NAME)-linux and $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64"

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
	go install ./cmd/vmkit-agent

fmt:
	@echo "Formatting code..."
	go fmt ./...

lint:
	@echo "Linting code..."
	golangci-lint run ./...

.DEFAULT_GOAL := build
