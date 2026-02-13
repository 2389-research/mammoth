# ABOUTME: Build, test, and development automation for mammoth.
# ABOUTME: Provides targets for building, testing, linting, and development workflows.

BINARY_NAME := mammoth
CMD_DIR := ./cmd/mammoth
BUILD_DIR := ./bin
MODULE := github.com/2389-research/mammoth

# Build flags
LDFLAGS := -s -w
GO := go
GOFLAGS := -count=1

.PHONY: all build test test-verbose test-race lint vet fmt clean help wg-status

all: test build ## Run tests then build

build: ## Build the mammoth binary
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

test: ## Run all tests
	$(GO) test $(GOFLAGS) ./...

test-verbose: ## Run all tests with verbose output
	$(GO) test $(GOFLAGS) -v ./...

test-race: ## Run all tests with race detector
	$(GO) test $(GOFLAGS) -race ./...

test-llm: ## Run only LLM SDK tests
	$(GO) test $(GOFLAGS) ./llm/...

test-agent: ## Run only agent loop tests
	$(GO) test $(GOFLAGS) ./agent/...

test-attractor: ## Run only pipeline runner tests
	$(GO) test $(GOFLAGS) ./attractor/...

cover: ## Generate test coverage report
	$(GO) test $(GOFLAGS) -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint: vet ## Run linters
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed (go install honnef.co/go/tools/cmd/staticcheck@latest)"

vet: ## Run go vet
	$(GO) vet ./...

fmt: ## Format all Go source files
	gofmt -s -w .

fmt-check: ## Check formatting without modifying files
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR) coverage.out coverage.html

tidy: ## Tidy and verify go.mod
	$(GO) mod tidy
	$(GO) mod verify

wg-status: ## Show workgraph task status
	@wg status

wg-ready: ## Show ready workgraph tasks
	@wg ready

check: fmt-check vet test ## Run all checks (CI-safe)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'
