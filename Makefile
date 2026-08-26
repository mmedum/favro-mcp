.DEFAULT_GOAL := help

GO              ?= go
GOLANGCI_LINT   ?= golangci-lint
GOVULNCHECK     ?= govulncheck

MODULE          := github.com/mmedum/favro-mcp
BIN_DIR         := bin
BIN_NAME        := favro-mcp
BIN             := $(BIN_DIR)/$(BIN_NAME)

VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
VERSION_PKG     := $(MODULE)/internal/version
LDFLAGS         := -s -w -X $(VERSION_PKG).Tag=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT)
BUILD_FLAGS     := -trimpath -ldflags "$(LDFLAGS)"

.PHONY: help
help: ## show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: build
build: ## build the favro-mcp binary into ./bin
	@mkdir -p $(BIN_DIR)
	$(GO) build $(BUILD_FLAGS) -o $(BIN) ./cmd/favro-mcp

.PHONY: test
test: ## run unit tests with the race detector
	$(GO) test -race -coverprofile=coverage.out ./...

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## golangci-lint + gofumpt + goimports check
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt: ## format with gofumpt + goimports (via golangci-lint v2)
	$(GOLANGCI_LINT) fmt

.PHONY: fmt-check
fmt-check: ## fail if formatting is not clean
	@$(GOLANGCI_LINT) fmt --diff > /tmp/favro-mcp-fmt.diff || true
	@if [ -s /tmp/favro-mcp-fmt.diff ]; then \
		echo "Formatting changes detected — run 'make fmt' and commit:"; \
		cat /tmp/favro-mcp-fmt.diff; \
		rm -f /tmp/favro-mcp-fmt.diff; \
		exit 1; \
	fi
	@rm -f /tmp/favro-mcp-fmt.diff

.PHONY: tidy
tidy: ## go mod tidy
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: tidy-check
tidy-check: ## fail if go.mod / go.sum need updating
	$(GO) mod tidy -diff

# Binary mode, not source mode. govulncheck's source analysis runs on x/tools'
# go/types, which as of x/vuln v1.7.0 (x/tools v0.49.0) tops out at go1.26 and
# errors on the go1.27 stdlib. Binary mode reads the built artifact's symbol
# table instead, so it works on 1.27 today and scans exactly what ships. Revert
# to `$(GOVULNCHECK) ./...` once x/vuln ships an x/tools that understands 1.27.
.PHONY: vulncheck
vulncheck: build ## govulncheck (binary mode — see comment above)
	$(GOVULNCHECK) -mode=binary $(BIN)

.PHONY: ci
ci: lint vet test vulncheck ## run lint + vet + test + vulncheck (mirrors CI)

.PHONY: package-plugin
package-plugin: ## build a snapshot favro-mcp.plugin (requires goreleaser)
	goreleaser release --snapshot --clean --skip=publish
	scripts/package-plugin.sh

.PHONY: clean
clean: ## remove build artifacts
	rm -rf $(BIN_DIR) dist coverage.out coverage.html *.plugin
