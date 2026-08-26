# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Versions below 1.0.0 were never tagged — pre-1.0 development shipped straight to `main`, so `0.0.1` has no release to compare against.

## [Unreleased]

### Added
- `TestMCP_AllTools_SmokeCallable`: calls all 83 tools over the MCP transport with minimal input, `dry_run: true` for mutating ones. Fails if a registered tool has no entry, so new tools can't skip coverage.
- `CLAUDE.md`: repo conventions for Claude Code — commands, layout, the Favro-returns-200-for-ignored-bodies trap, and the docs standards below.

### Changed
- `CHANGELOG.md` follows Keep a Changelog properly: standard categories in order, ISO dates, link references. Entries rewritten to one line each — 57KB to 8KB, same facts.
- `README.md` trimmed to a scannable overview with badges; the 83-row tool table moved to `docs/TOOLS.md`.
- `CONTRIBUTING.md` documents the changelog, versioning and release rules, and the Go 1.27 / golangci-lint ≥ v2.13.1 requirement.

### Fixed
- `favro_move_card` needed a destination but didn't say so in its schema — found by the smoke test.

## [1.1.0] - 2026-08-26

Re-checked the client against Favro's REST docs and a live tenant. The custom-field layer was built on a wire contract Favro doesn't implement, three write paths could never have worked, and four documented resources had no client at all. 64 tools → 83. Go 1.27 clears three stdlib CVEs.

This release removes tool inputs, which normally calls for a major bump. It's a minor because every removed input sat on a code path that silently did nothing — no working behaviour changes shape. Calls using the old names now fail loudly instead of being quietly ignored. See **Removed**.

### Added
- Tasks and Tasklists (checklists): 10 tools. `favro_create_tasklist` seeds items in one request.
- Dependencies: 6 tools. `favro_add_dependencies` keeps existing links; `favro_replace_dependencies` clears first.
- Activities: `favro_list_card_activities`, with optional `since` / `until`.
- `favro_upload_comment_attachment`. Both upload paths gained the optional `mimeType`.
- `favro_remove_attachment`, now that `removeAttachments` is sent the identifier Favro actually matches on.
- Card create/update: `dependencies`, `tasklists`, `customFields` on create, `descriptionFormat` as a query param, and the add/remove forms for dependencies, tasklists and favro attachments.
- `Card` decodes `todoListUserId`, `todoListCompleted`, `dependencies`. `Group` decodes `creatorUserId`, `memberCount`.
- `favro_update_comment` gained `remove_attachments`. `GroupMember` gained `email` and `delete`.
- `favro_rate_limit_status` reports `throttle_delay_seconds` from `X-RateLimit-Delay` — Favro stalls responses before it starts rejecting with 429.
- `Card.CustomFields()` reads both `customFieldsValues` and `customFields`; the docs and this client disagree on which key Favro sends.
- 6 more markdown fixtures: nested lists, blockquotes, fence-in-fence, unicode/emoji, task lists, inline HTML.
- `bin/favro-mcp.cmd` in the plugin zip so Windows resolves the launcher via PATHEXT.

### Changed
- Go 1.26 → 1.27. Deps: go-sdk 1.7.0, testify 1.12.1, x/sync 0.22, x/term 0.45.
- Actions: checkout v7, setup-go v7, codecov v7.
- `govulncheck` runs in binary mode — its source analysis can't parse the 1.27 stdlib yet (x/vuln v1.7.0). Revert to `govulncheck ./...` when it can. `golangci-lint` must now be ≥ v2.13.1.
- Read formatters accept both the documented custom-field shape and the old one, so a value decodes either way. Writes send the documented shape only.
- Custom-field type strings re-grounded against a live `/customfields` call: Favro emits `Voting`, not the documented `Vote`; `Single select`, `Date created`, `Sequential ID` and `Relations` are real but undocumented. Both spellings are accepted.
- CI: dropped three dead early-phase guards. `test-mcp` no longer swallows failures behind `|| echo`.

### Removed
- `favro_set_card_custom_field`: `member_user_ids` (use `add_member_user_ids` / `remove_member_user_ids`) and `rating_total` (Favro fixes the scale at 0–5).
- `favro_create_collection` and `favro_update_collection`: `shared_to_users` (use `share_to_users` for invites, `members` for role changes).

### Fixed
- Custom fields: Number and Rating travel in `total`, not `value`.
- Custom fields: Status, Multiple select and Single select put item ids in `value`, not `customFieldItemIds`.
- Custom fields: Members takes a `members` object of add/remove deltas; Tags takes `tags`; Link takes `link` ({url, text}); Timeline takes `timeline`.
- Custom fields: Vote reads back as an array of userIds, not a bool.
- Collections: invites go in `shareToUsers`, role changes in `members`. The client sent the read-shaped `sharedToUsers`, which Favro accepts and ignores — every share was silently dropped.
- Widgets: archiving requires `collectionId`. `UpdateWidgetRequest` had no such field, so archive could never work. `DeleteWidget` gained the optional `collectionId` too.
- Attachments: `removeAttachments` matches on attachment URL, not display name.
- `completeAssignments` is an array of per-user flips, not a bool.
- `CardCustomFieldValue.Total` is `*float64` so an explicit 0 is distinguishable from unset.
- `Collection.publicSharing: "users"` is documented and is the default; a note added in 1.0.0 claimed otherwise.

### Security
- Go 1.27 clears [GO-2026-5037](https://pkg.go.dev/vuln/GO-2026-5037), [GO-2026-5039](https://pkg.go.dev/vuln/GO-2026-5039) and [GO-2026-5856](https://pkg.go.dev/vuln/GO-2026-5856), all reachable through `auth.Validator.Validate` and `favro.drainAndClose`. CI had been red on these since July.

## [1.0.0] - 2026-05-07

First stable release. Full CRUD over every Favro REST resource, workflow tools for natural-language use (search, name resolution, surgical description edits), dry-run on every mutating tool, and a multi-arch `favro-mcp.plugin` bundle. 64 tools.

### Added
- Auth: env and keyring credential sources, `auth login` / `status` / `logout` / `which`, startup validation against `GET /organizations`. Stderr-only logging.
- Favro client: HTTP Basic auth, single 429 retry honoring `Retry-After`, typed errors, redacted logging, `Paginate[T]`, rate-limit tracking. Generic `TTL[V]` cache.
- Read tools: `favro_list_*` and `favro_get_*` for organizations, users, collections, widgets, columns, cards, comments, tags, custom fields, groups.
- Resolvers: `favro_resolve_*` for the seven named resources, ranked, cached, with `force_refresh`. Users match on name or email; columns require a widget scope.
- `favro_search_cards`: local full-text search with markdown stripped, 60s scoped corpus cache. Requires a widget or collection — Favro rejects unscoped card listings.
- `favro_get_card_full`: one card with every id dereferenced to a name, saving 4–7 follow-up calls.
- Write tools for tags, comments, cards, collections, widgets, columns and groups, plus `favro_set_card_custom_field` and `favro_update_tags`.
- Dry-run on every mutating tool: returns the would-be request and a state-diff without contacting Favro. `--dry-run` forces it process-wide.
- Description editors: `favro_append/prepend/replace_in_card_description`, returning `{old, new, unified_diff}`.
- `favro_add_comment_to_card`, `favro_add_tag_to_card`, `favro_remove_tag_from_card`. The tag tools hard-fail on unknown names to prevent typo-created tags.
- `favro_upload_attachment`, 8 MiB cap enforced before any HTTP.
- Releases: goreleaser cross-builds 5 platforms; `scripts/package-plugin.sh` assembles the `.plugin` bundle with an arch-detecting launcher.

### Changed
- Bulk tag updates are a client-side parallel fan-out: `PUT /tags` (no id) isn't a real endpoint, it returns Favro's SPA fallback HTML with status 200.
- Column moves send `widgetCommonId` + `columnId` + `listPosition` + `dragMode` together, or Favro silently no-ops them.

### Fixed
- `Card.Position` / `ListPosition` are `float64` — Favro uses fractional positions.
- `Collection.fullMembersCanAddGuests` was never a real field; it's `fullMembersCanAddWidgets`.
- HTTP 403 split out of `AuthError` into `ForbiddenError`.
- A caller-supplied `Content-Type` now overrides the JSON default instead of being comma-joined.
- `listPosition` / `sheetPosition` are typed as JSON numbers; strings 400.
- `DELETE /cards/{id}` decodes as a bare array of cardIds.
- `POST /cards/{id}/attachment` decodes as the attachment object, not the card.

## [0.0.1] - 2026-05-03

### Added
- Repo bootstrap: `go.mod`, `.golangci.yml`, `Makefile`, `NOTICE`, `README.md`, `CONTRIBUTING.md`.
- GitHub Actions: `ci.yml` (lint, multi-OS tests, vulncheck, build) and `release.yml`.
- Dependabot for Go modules and Actions. PR template.

[Unreleased]: https://github.com/mmedum/favro-mcp/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/mmedum/favro-mcp/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/mmedum/favro-mcp/releases/tag/v1.0.0
