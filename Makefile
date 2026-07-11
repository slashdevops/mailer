GO            ?= go
GOLANGCI_LINT ?= golangci-lint
COVER_PROFILE ?= coverage.txt

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help.
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Tidy and verify go modules.
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: fmt
fmt: ## Format all Go sources.
	gofmt -w .

.PHONY: vet
vet: ## Run go vet.
	$(GO) vet ./...

.PHONY: build
build: ## Build all packages.
	$(GO) build ./...

.PHONY: test
test: ## Run tests with the race detector and coverage.
	$(GO) test -race -covermode=atomic -coverprofile=$(COVER_PROFILE) ./...

.PHONY: cover
cover: test ## Enforce the coverage threshold from .testcoverage.yml.
	$(GO) run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yml --profile=$(COVER_PROFILE)

.PHONY: cover-html
cover-html: test ## Open the HTML coverage report.
	$(GO) tool cover -html=$(COVER_PROFILE)

.PHONY: lint
lint: ## Run golangci-lint.
	$(GOLANGCI_LINT) run ./...

.PHONY: check
check: fmt vet test lint ## Run the full local verification suite.

.PHONY: clean
clean: ## Remove build and coverage artifacts.
	rm -f $(COVER_PROFILE)
	$(GO) clean ./...
