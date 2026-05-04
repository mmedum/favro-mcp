# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added (Phase 3 — Read CRUD: organizations, users, collections, widgets, columns)
- `internal/favro`: `Organization` / `SharedUser` types, `(*Client).GetJSON` helper, `ListOrganizations(ctx, page, requestID)`, `GetOrganization(ctx, id)`.
- `internal/favro`: `User` type, `ListUsers(ctx, page, requestID)`, `GetUser(ctx, userID)`.
- `internal/favro`: `Collection` type, `ListCollections(ctx, page, requestID)`, `GetCollection(ctx, collectionID)`.
- `internal/favro`: `Widget` type, `ListWidgets(ctx, page, requestID, collectionID)` (first resource with a filter), `GetWidget(ctx, widgetCommonID)`.
- `internal/favro`: `Column` type (incl. `CardCount`/`TimeSum`/`EstimationSum` aggregates), `ListColumns(ctx, page, requestID, widgetCommonID)`, `GetColumn(ctx, columnID)`. `widgetCommonID` is mandatory — Favro's /columns endpoint rejects unfiltered listings with HTTP 400; the client short-circuits empty input with `errMissingWidgetCommonID` to surface the requirement before any HTTP call.
- MCP tools: `favro_list_*` and `favro_get_*` for organizations/users/collections/widgets/columns (all read-only; shared `listInput`/`listOutput[T]` shape — surfaces `next_page` and `request_id` for explicit pagination, never auto-aggregates). `favro_list_widgets` accepts optional `collection_id`; `favro_list_columns` requires `widget_common_id`.

### Added (Phase 2 — Favro client foundation)
- `internal/favro` package: `Client` with HTTP Basic Auth, retry (single 429 retry honoring `Retry-After` ≤ 30s; 5xx exponential backoff 250ms/1s/4s × 3), typed errors (`AuthError` / `RateLimitError` / `NotFoundError` / `ValidationError` / `TransientError` / `APIError`), per-request `WithDryRun`/process-wide `ForceDryRun` gates that short-circuit POST/PUT/DELETE/PATCH and return a redacted `*DryRunRecord`, `Authorization`-header redaction in slog debug output and dry-run records.
- `Paginate[T]` generic helper drives Favro's two-step pagination protocol (sets `X-Favro-Backend-Identifier` from the prior response's `requestId`).
- `RateLimitSnapshot` observation: every Favro response feeds a goroutine-safe tracker; surfaced via the new `favro_rate_limit_status` MCP tool.
- `internal/cache` package: generic `TTL[V]` cache — goroutine-safe, lazy expiry on Get, `Invalidate` / `InvalidatePrefix` / `Clear` / `Sweep`. Foundation for Phase 4+ resolver caches.
- `internal/server`: `New` now takes a `*favro.Client`; `favro_rate_limit_status` registered alongside `favro_ping`.
- Tests: 90.9% coverage on `internal/favro`, 100% on `internal/cache`. New regression: `TestMCP_RateLimitStatus_AfterObservation` proves the rate-limit tool reports observed Favro headers correctly.

### Added (Phase 1 — auth + stdio handshake)
- `internal/auth` package: `Token` with HTTP Basic Auth + `organizationId` header `Apply`, `Source` interface, `EnvSource` (FAVRO_USER_EMAIL / FAVRO_API_TOKEN / FAVRO_ORGANIZATION_ID), `KeyringSource` (OS keyring with active-account pointer), `ResolveToken` (env → keyring), `Validator` (live `GET /organizations` with 5s timeout).
- `internal/server` package: `New(ResolvedToken, version)` builds an `*mcp.Server`, registers `favro_ping` tool. Output never carries email or API token; pinned by a regression test.
- `cmd/favro-mcp`: stdio MCP server entry point, plus `auth login` (masked token via `golang.org/x/term`), `auth status`, `auth logout`, `auth which`, `--version`, `--help`, `--dry-run` (no-op until Phase 5).
- Stderr-only logging via `log/slog`; level controllable via `FAVRO_LOG_LEVEL`.
- Startup live-validation hits `GET /organizations` and exits non-zero on 401/403; `FAVRO_MCP_SKIP_VALIDATE=1` bypasses it for test harnesses.
- Tests: 87% coverage on auth, 100% on server, in-memory MCP transport assertions for `tools/list` and `tools/call favro_ping`.

## [0.0.1] — 2026-05-03

### Added
- Repo bootstrap: `go.mod` (Go 1.26 + toolchain), `.gitignore`, `.editorconfig`, `NOTICE`.
- `README.md`, `CONTRIBUTING.md`, this changelog.
- `.golangci.yml` with the production linter set.
- `Makefile` with `lint`, `test`, `build`, `fmt`, `ci`, `tidy` targets.
- GitHub Actions: `ci.yml` (lint + multi-OS unit tests + vulncheck + build), `release.yml` (tag-driven goreleaser pipeline placeholder).
- Dependabot config for Go modules and GitHub Actions.
- PR template with dry-run and docs checkboxes.
