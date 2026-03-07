BINARY_NAME := codedb
BUILD_DIR := bin
CGO_ENABLED := 0

.PHONY: build build-cgo install clean test test-cgo test-v lint format help

## build: Build the codedb binary (no CGO, no tree-sitter symbols)
build:
	CGO_ENABLED=$(CGO_ENABLED) go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/codedb/

## build-cgo: Build with CGO enabled (includes tree-sitter symbol extraction)
build-cgo:
	CGO_ENABLED=1 go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/codedb/

## install: Install to $GOPATH/bin
install:
	CGO_ENABLED=$(CGO_ENABLED) go install ./cmd/codedb/

## clean: Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## test: Run tests without CGO
test:
	CGO_ENABLED=$(CGO_ENABLED) go test -race ./...

## test-cgo: Run all tests including tree-sitter symbol tests
test-cgo:
	CGO_ENABLED=1 go test -race ./...

## test-v: Run all tests verbose with CGO
test-v:
	CGO_ENABLED=1 go test -race -v ./...

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
