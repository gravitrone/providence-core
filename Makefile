SHELL := /bin/bash

.PHONY: help build vet lint fmt test coverage changelog hooks clean install setup bench-bridge

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Build binary (./providence)
	go build -o providence ./cmd/providence/

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint
	@command -v golangci-lint > /dev/null 2>&1 || { echo "install golangci-lint: brew install golangci-lint"; exit 1; }
	golangci-lint run ./...

fmt: ## Format all Go files
	gofmt -w .

test: ## Run tests with race detection
	go test -race -count=1 ./...

coverage: ## Run tests with coverage report
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1
	@rm -f coverage.out

changelog: ## Generate changelog from conventional commits
	@command -v git-cliff > /dev/null 2>&1 && git-cliff -o CHANGELOG.md || echo "install git-cliff: cargo install git-cliff"

hooks: ## Install git hooks
	@if [ -d .git ]; then \
		cp scripts/pre-commit .git/hooks/pre-commit && \
		chmod +x .git/hooks/pre-commit && \
		echo "pre-commit hook installed"; \
	else \
		echo "no .git directory found"; \
	fi

bench-bridge: ## Run latency benchmarks for the macOS bridge
	go test -bench=Benchmark -benchmem -benchtime=3s ./internal/bridge/macos/...

clean: ## Clean build artifacts
	rm -f providence coverage.out
	rm -rf dist/

install: hooks ## Install git hooks + verify deps
	go mod tidy
	@echo "deps verified, hooks installed"

install-bin: build ## Install providence to /usr/local/bin
	cp providence /usr/local/bin/providence
	@echo "installed - run 'providence' from anywhere"

setup: install build ## Full setup from fresh clone
	@echo "setup complete - run ./providence to launch"
