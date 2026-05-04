# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added (Phase 4 — NL workflow read layer)
- `internal/server`: `Resolver` struct holds a `*favro.Client` plus per-resource `cache.TTL[…]` caches (`tagCache` first; the other six follow in Phase 4.2). Constructed once per `mcp.Server` so cache state is process-wide. Cache TTLs follow plan §7 (5 minutes for slow-changing org metadata: tags, users, custom fields, groups; 60 seconds for collection-scoped lists in later sub-phases).
- `internal/server`: `Resolver.ResolveTag(ctx, name, limit, forceRefresh)` returns ranked `ResolvedTag` candidates (`{tagId, name, color, score}`). Score scale: exact match 1.0, prefix 0.7, substring 0.4, no match filtered. Tie-break by name ascending so ordering is deterministic. `limit <= 0` defaults to 10; `limit > 50` is capped at 50 per plan §6a.
- `internal/server`: `Resolver.invalidateTagCache()` for Phase 6 mutating tools to call after successful tag create / update / delete.
- MCP tool: `favro_resolve_tag` (name required; optional `limit` and `force_refresh`). Output is always a candidate list (possibly empty) so the LLM can disambiguate when more than one tag matches; an exact match scores 1.0 and sorts first. `cached: bool` in the output tells callers whether the tag list was served from the in-memory cache so they know whether a `force_refresh` follow-up might surface newly-created tags.

### Fixed
- `internal/favro`: split HTTP 403 out of `AuthError` into a new typed `ForbiddenError` (`Status`, `Path`). Previously both 401 and 403 mapped to `AuthError`, surfacing as `"Favro authentication failed — check FAVRO_USER_EMAIL and FAVRO_API_TOKEN"` even when the credentials were fine and the resource was simply not visible to the token (Favro returns 403 instead of 404 for permission-restricted or non-existent resources, intentionally avoiding existence leakage). The new `ForbiddenError` says `"Favro denied access at <path> (HTTP 403): the resource may not exist or the token lacks permission for it"` and is documented to be functionally equivalent to a not-found from the caller's perspective. Phase 4 workflow tools (search → fetch chains) would have amplified the misleading auth message into LLM dead-ends. `internal/auth/validate.go`'s startup credential check still maps 401||403 to auth-failed because at boot the broadest possible scope (`/organizations`) is unambiguously authentication.

### Changed
- CI: removed the `test-integration` job. It was gated to push-to-main / `integration-ok`-labeled PRs and ran zero tests anyway (no `*_integration_test.go` fixtures exist), so it was cruft. Wire-format coverage is currently done via the maintainer's pre-commit live MCP test; a recorded-fixture replay path can revive a real CI integration job later without ever calling Favro from CI. Repo secrets `FAVRO_USER_EMAIL` / `FAVRO_API_TOKEN` / `FAVRO_ORGANIZATION_ID` referenced by the removed job are no longer used in CI; rotate or delete them in GitHub Settings if you want.

### Added (Phase 3 — Read CRUD: organizations, users, collections, widgets, columns, cards, comments, tags, custom fields, groups)
- `internal/favro`: `Organization` / `SharedUser` types, `(*Client).GetJSON` helper, `ListOrganizations(ctx, page, requestID)`, `GetOrganization(ctx, id)`.
- `internal/favro`: `User` type, `ListUsers(ctx, page, requestID)`, `GetUser(ctx, userID)`.
- `internal/favro`: `Collection` type, `ListCollections(ctx, page, requestID)`, `GetCollection(ctx, collectionID)`.
- `internal/favro`: `Widget` type, `ListWidgets(ctx, page, requestID, collectionID)` (first resource with a filter), `GetWidget(ctx, widgetCommonID)`.
- `internal/favro`: `Column` type (incl. `CardCount`/`TimeSum`/`EstimationSum` aggregates), `ListColumns(ctx, page, requestID, widgetCommonID)`, `GetColumn(ctx, columnID)`. `widgetCommonID` is mandatory — Favro's /columns endpoint rejects unfiltered listings with HTTP 400; the client short-circuits empty input with `errMissingWidgetCommonID` to surface the requirement before any HTTP call.
- `internal/favro`: `Card` type (cardCommonId, cardId, sequentialId+prefix, name, description, parent/widget/column ids, position, archived, dates, tag ids, assignment {userId, completed} pairs), `CardAssignment`, `ListCardsFilter` struct (widgetCommonID/collectionID/cardCommonID/sequentialID/unique), `ListCards(ctx, page, requestID, filter)`, `GetCard(ctx, cardID)`. CustomFieldsValues and full assignment/tag resolution are deferred to Phase 4 workflow tools. `GetCard` accepts the per-widget cardId — Favro's GET /cards/{id} 403s on a cardCommonId (verified live); callers must use the list endpoint with `cardCommonId` filter for cross-widget lookup.
- `internal/favro`: `Comment` type (commentId, cardCommonId, userId, body, created/lastUpdated timestamps), `ListComments(ctx, page, requestID, cardCommonID)`, `GetComment(ctx, commentID)`. `cardCommonID` is mandatory — Favro's /comments endpoint scopes comments to a single card; the client short-circuits empty input with `errMissingCardCommonID`.
- `internal/favro`: `Tag` type (tagId, organizationId, name, color), `ListTags(ctx, page, requestID)`, `GetTag(ctx, tagID)`. Tags are org-global so `ListTags` takes no filter.
- `internal/favro`: `CustomField` type (customFieldId, organizationId, type, name, enabled, customFieldItems), `CustomFieldItem` (customFieldItemId, name, color, enabled), `ListCustomFields(ctx, page, requestID)`, `GetCustomField(ctx, customFieldID)`. Custom fields are org-global so the list endpoint takes no filter; `customFieldItems` is populated only for select-flavored types (`Single select`, `Multiple select`). The `type` field is Favro's display label (verified live values include `Text`, `Number`, `Date`, `Date created`, `Checkbox`, `Single select`, `Multiple select`, `Members`, `Tags`, `Rating`, `Link`, `Progress`, `Voting`, `Relations`, `Sequential ID`, `Timeline`); Favro extends this set without notice, so the field stays a plain string. Per-card values come via the card payload's `customFieldsValues` (Phase 4 / 6 territory).
- `internal/favro`: `Group` type (groupId, organizationId, name, members), `GroupMember` (userId, role), `ListGroups(ctx, page, requestID)`, `GetGroup(ctx, groupID)`. Groups are org-global so the list endpoint takes no filter; `members` is `{userId, role}` pairs (omitempty drops the field for groups without members).
- MCP tools: `favro_list_*` and `favro_get_*` for organizations/users/collections/widgets/columns/cards/comments/tags/custom fields/groups (all read-only; shared `listInput`/`listOutput[T]` shape — surfaces `next_page` and `request_id` for explicit pagination, never auto-aggregates). `favro_list_widgets` accepts optional `collection_id`; `favro_list_columns` requires `widget_common_id`; `favro_list_cards` accepts widget/collection/cardCommon/sequential filters plus `unique` for cross-widget dedup; `favro_list_comments` requires `card_common_id`; `favro_list_tags`, `favro_list_custom_fields`, and `favro_list_groups` take no filter (all org-global). Phase 3 read CRUD complete.

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
