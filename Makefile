# Common dev tasks. CI runs the same gates from .github/workflows/ci.yml,
# and `gates parity` fails if these two lists stop agreeing — a comment
# claiming "everything CI runs" is the sentence that stops the next
# person checking.

GO        ?= go
# .exe on Windows, where a file without one cannot be executed at all.
EXE       := $(if $(filter Windows_NT,$(OS)),.exe,)
BIN_DIR   := bin
BIN       ?= $(BIN_DIR)/favro-mcp$(EXE)
COVER_MIN ?= 80

MODULE      := github.com/mmedum/favro-mcp
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
VERSION_PKG := $(MODULE)/internal/version
LDFLAGS     := -s -w -X $(VERSION_PKG).Tag=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT)
BUILD_FLAGS := -trimpath -ldflags "$(LDFLAGS)"

# The tools, pinned to the versions CI uses and fetched the way CI
# fetches them. Not whatever is on the PATH: a distribution's
# golangci-lint built with an older Go refuses this module outright — and
# says so as "can't load config", which names the wrong thing. `gates
# pins` holds each of these to exactly one version, and holds the
# gitleaks pin to the one CI installs.
GOLANGCI_LINT ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
GOVULNCHECK   ?= golang.org/x/vuln/cmd/govulncheck@v1.8.0
GOLICENSES    ?= github.com/google/go-licenses@v1.6.0
GITLEAKS      ?= github.com/zricethezav/gitleaks/v8@v8.30.1

.DEFAULT_GOAL := help

.PHONY: help
help: ## show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: build
build: ## build the favro-mcp binary into ./bin
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o $(BIN) ./cmd/favro-mcp

.PHONY: fmt
fmt: ## format with gofumpt + goimports (via golangci-lint v2)
	$(GO) run $(GOLANGCI_LINT) fmt

.PHONY: fmt-check
fmt-check: ## fail if formatting is not clean
	@$(GO) run ./scripts/gates fmt-check

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## golangci-lint
	$(GO) run $(GOLANGCI_LINT) run ./...

.PHONY: test
test: ## unit tests with the race detector and coverage
	$(GO) test -race -coverpkg=./internal/...,./cmd/... -coverprofile=coverage.out -covermode=atomic ./...

.PHONY: cover
cover: test ## enforce the per-package coverage floor
	@$(GO) run ./scripts/gates coverage coverage.out $(COVER_MIN)

.PHONY: tidy
tidy: ## go.mod and go.sum are what `go mod tidy` would write
	$(GO) mod tidy -diff
	$(GO) mod verify

.PHONY: vuln
vuln: ## govulncheck
	$(GO) run $(GOVULNCHECK) ./...

.PHONY: licenses
licenses: ## every dependency's licence is one we accept
# segmentio/asm is ignored by path, not by licence name. It relicensed
# to MIT No Attribution (MIT-0) in v1.2.1 — strictly more permissive than
# the MIT it carried before, and OSI-approved — and go-licenses v1.6.0's
# classifier, which is from 2023, does not recognise MIT-0 at all: it
# reports an empty licence name, so no --allowed_licenses value can
# satisfy it. Verified by reading the module's LICENSE. It arrives
# transitively through the MCP SDK's jsonschema dependency.
	$(GO) run $(GOLICENSES) check ./... --allowed_licenses=Apache-2.0,BSD-2-Clause,BSD-3-Clause,MIT,ISC --ignore github.com/segmentio/asm

.PHONY: secrets
secrets: ## credentials, as CI scans for them
	$(GO) run $(GITLEAKS) dir . --config .gitleaks.toml --no-banner

.PHONY: leaks
leaks: ## identifiers and tenant data in the working tree
	@$(GO) run ./scripts/gates leaks

.PHONY: leaks-history
leaks-history: ## every blob and every commit message in the history
	@$(GO) run ./scripts/gates leaks history

.PHONY: pins
pins: ## actions pinned by SHA, tool versions exact, shells pinned
	@$(GO) run ./scripts/gates pins

.PHONY: api-diff
api-diff: ## refetch the Favro API surface snapshot (network; maintainer only)
	@$(GO) run ./scripts/gates api-diff

.PHONY: api-coverage
api-coverage: ## every documented Favro endpoint has a verdict, and the reverse
	@$(GO) run ./scripts/gates api-coverage

.PHONY: api-fields
api-fields: ## every documented field is modelled, or waived with a reason
	@$(GO) run ./scripts/gates api-fields

.PHONY: classes
classes: ## the closed error vocabulary, against the document that names it
	@$(GO) run ./scripts/gates classes

.PHONY: parity
parity: ## `make check` and ci.yml run the same things
	@$(GO) run ./scripts/gates parity

.PHONY: plugin
plugin: ## the committed plugin manifest, against the files the packer will stage
	@$(GO) run ./scripts/gates plugin

.PHONY: schemas
schemas: build ## dump the tool schemas
	$(BIN) --dump-schemas > schemas.json

.PHONY: schema-diff
schema-diff: build ## diff the tool schemas against the last tag
	@$(GO) run ./scripts/gates schema-diff $(BIN)

.PHONY: smoke
smoke: build ## drive the binary over stdio
	@$(GO) run ./scripts/gates smoke $(BIN)

.PHONY: staleness
staleness: build ## the docs must match the code
	@$(GO) run ./scripts/gates staleness $(BIN)

.PHONY: hooks
hooks: ## point git at the repository's own hooks
	git config core.hooksPath .githooks

.PHONY: package-plugin
package-plugin: ## build a snapshot favro-mcp.plugin (requires goreleaser)
	goreleaser release --snapshot --clean --skip=publish
	$(GO) run ./scripts/gates plugin-pack

.PHONY: check
check: fmt-check vet tidy lint cover vuln licenses secrets leaks pins classes api-coverage api-fields parity plugin schema-diff smoke staleness ## everything CI runs

.PHONY: clean
clean: ## remove build artifacts
	$(RM) -r $(BIN_DIR) dist coverage.out coverage.html favro-mcp.plugin
