BINARY_NAME := codedb
BUILD_DIR := bin

.PHONY: build install clean test test-v lint format help

## build: Build the codedb binary
build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/codedb/

## install: Install to $GOPATH/bin
install:
	go install ./cmd/codedb/

## clean: Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## test: Run tests
test:
	go test -race ./...

## test-v: Run all tests verbose
test-v:
	go test -race -v ./...

## lint: Run golangci-lint
lint:
	golangci-lint run ./...

## format: Format Go code
format:
	gofmt -w .
	goimports -w .

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
