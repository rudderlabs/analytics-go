GO=go
GOLANGCI=github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.9.0

.PHONY: lint
lint: ## Run linters on all go files
	$(GO) run $(GOLANGCI) run -v

.PHONY: test
test: ## Run tests
	$(GO) test ./...
