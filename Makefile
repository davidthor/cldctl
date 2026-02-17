# cldctl Makefile

BINARY_NAME=cldctl
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w -X github.com/davidthor/cldctl/internal/cli.Version=$(VERSION) -X github.com/davidthor/cldctl/internal/cli.Commit=$(COMMIT) -X github.com/davidthor/cldctl/internal/cli.BuildDate=$(BUILD_DATE)"

.PHONY: all build clean test lint install playground-wasm

all: build

## Build the binary
build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/cldctl

## Build for all platforms
build-all:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/cldctl
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/cldctl
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/cldctl
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/cldctl
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/cldctl

## Clean build artifacts
clean:
	rm -rf bin/
	go clean

## Run tests
test:
	go test -v ./...

## Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## Run integration tests (requires CLERK_DOMAIN, CLERK_PUBLISHABLE_KEY, CLERK_SECRET_KEY)
test-integration:
	go test -tags=integration -v -timeout=10m ./testdata/integration/...

## Run linter
lint:
	golangci-lint run ./...

## Install the binary
install: build
	cp bin/$(BINARY_NAME) $(GOPATH)/bin/

## Run the application
run:
	go run ./cmd/cldctl

## Download dependencies
deps:
	go mod download
	go mod tidy

## Format code
fmt:
	go fmt ./...

## Generate mocks (if using mockery)
generate:
	go generate ./...

WASM_OUT=docs/assets/playground.wasm

## Build WASM module for playground
playground-wasm:
	@mkdir -p docs/assets
	GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o $(WASM_OUT) ./cmd/playground-wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" docs/assets/wasm_exec.js
	@echo "WASM module built: $(WASM_OUT) ($$(du -h $(WASM_OUT) | cut -f1))"

## Show help
help:
	@echo "Available targets:"
	@echo "  build       - Build the binary"
	@echo "  build-all   - Build for all platforms"
	@echo "  clean       - Clean build artifacts"
	@echo "  test        - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  test-integration - Run integration tests (requires Clerk env vars)"
	@echo "  lint        - Run linter"
	@echo "  install     - Install the binary"
	@echo "  run         - Run the application"
	@echo "  deps        - Download dependencies"
	@echo "  fmt         - Format code"
	@echo "  playground-wasm - Build WASM module for docs playground"
	@echo "  help        - Show this help"
