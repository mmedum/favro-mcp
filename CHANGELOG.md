# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Versions below 1.0.0 were never tagged — pre-1.0 development shipped straight to `main`, so `0.0.1` has no release to compare against.

## [Unreleased]

### Added
- `forbidigo` forbids `fmt.Print*` and naming `os.Stdout` outside `main`. Hard rule 3 restates an MCP spec MUST NOT — a stdio server must write nothing to stdout that is not a valid MCP message — and nothing enforced it: the tree was clean, so a stray print would have corrupted the JSON-RPC stream with no check failing. Found by a contributor sweep across the sibling servers.

## [2.0.0] - 2026-09-14

Adopts the shared Go MCP server standard the four sibling servers run:
fifteen gates, a Claude Desktop bundle, and a signed release. Three
breaking changes to the tool surface — see Removed and Changed.

### Added
- `docs/architecture.md`: the design, the platform constraints, the evidence log, and the A0–A8 plan that aligns this repository with the shared Go MCP server standard the four sibling servers run.
- `--dump-schemas` prints the whole tool surface as JSON, and `schemas.json` is committed: `make schemas` writes it, the `schema-diff` gate verifies it is current, so a wire change shows up in the pull request's diff rather than only on the machine that ran the gate.
- `rule8` gate: no tool input declares an `organization_id`. Hard rule 8 has said the server is single-org since it shipped and was held by nothing; `favro_get_organization` broke it. The list comes from the binary's own schema dump, so a tool added later is held by having been registered.
- `scripts/gates`, one Go binary holding nine checks that run in both `make check` and CI: the per-package coverage floor (80%), `leaks`, `pins`, `classes`, `parity`, `plugin`, `schema-diff`, `smoke` and `staleness`. Each has tests and each reports how much it read.
- `leaks` scans the working tree for tenant data — addresses, 24-hex Favro ids, keyed organization ids and tokens, app links, card references — and `leaks history` scans every blob and commit message. The rule has been in CLAUDE.md since phase 3 with nothing enforcing it.
- gitleaks, go-licenses and CodeQL in CI; `.gitleaks.toml`; `.githooks/pre-commit` (via `make hooks`) runs gofmt, vet and the leak scan before a commit.
- `FAVRO_ENABLE_DESTRUCTIVE`: set it to `true` to register the delete-style tools. See Removed.
- `internal/render`: the readable half of every tool result, and the closed error vocabulary — `invalid`, `not_found`, `auth`, `conflict`, `unavailable`, `unsupported`, `forbidden`, `rate_limited`, `ambiguous`.
- `classes` gate: the vocabulary in `internal/render/class.go` and the table in `docs/architecture.md` §6.2 must name each other, and a class no code returns fails too.
- `internal/config`: every `FAVRO_*` setting resolved once at startup, with the values it could not read reported rather than silently defaulted.
- `favro_list_webhooks` and `favro_delete_webhook`. There is deliberately no tool to create one: Favro's are *outgoing* webhooks, pointing at a URL that must outlive this process. The signing secret Favro returns is not modelled, so it cannot reach a result.
- `testdata/api-surface.json`, written by `make api-diff` from favro.com/developer: 88 endpoints and 15 resource field tables. `api-coverage` and `api-fields` gates hold it against `testdata/api-coverage.tsv` and `testdata/api-fields-waived.tsv` offline, in both directions, so an endpoint or field nobody decided about fails the build.
- `Card.sheetPosition`, `CardAttachment.thumbnailURL` and `CustomField.widgetCommonId` are modelled. The last is the field that says which widget a custom field is enabled on — writing to a field the widget has not enabled is accepted and ignored, and nothing in a response said so before.
- depguard rules holding the package dependency direction, one per package, naming what it may not import, plus a rule denying testify and go-difflib.
- `scripts/livefavro`: drives the built binary against a real organization, 122 steps over every tool, printing only through `internal/redact`. Every mutating step carries `dry_run`, so it writes nothing.
- `internal/redact`: stable placeholders for the values a transcript must not carry. The same id reads as the same `{id 1}` throughout a run, so an id can still be followed across calls.
- `transcript` and `live-cover` gates: the first fails if anything in the driver reaches the terminal without redacting, the second if any tool or option has no step. 309 of 332 options covered, 23 waived with a reason.
- `internal/service/diff.go`: a unified-diff generator, held to `diff -u` itself by test rather than to a golden. Its search is bounded by edit distance, so a one-line change at each end of a long description stays a small diff.
- `favro-mcp doctor`: reports the build, which source the credentials came from, whether Favro accepts them, and whether `FAVRO_ORGANIZATION_ID` names an organization the token can actually see — the failure that otherwise surfaces as every tool returning `not_found`. Output is redacted with stable placeholders so it can be pasted into an issue; `--show-ids` prints the real values and says it is unsafe to share. Neither mode prints the token.
- A `.mcpb` Claude Desktop bundle on every release, packed in Go by `gates mcpb-pack` from the universal binary's post hook — the one point where every binary exists and `checksums.txt` has not been written, which is what puts the bundle under the release signature.
- `mcpb` gate: the committed manifest against the files the packer stages. Beyond the referential checks, three the shared standard names and a sibling shipped without: an override for a platform `compatibility.platforms` does not claim, a claimed platform that spawns a staged file other than the one staged *for* it (deleting the `win32` override hands Windows the macOS binary and every other check still passes), and a launcher dispatching to names nobody stages. It also holds the manifest's `FAVRO_*` env keys to the names the source actually reads.
- Releases carry an SBOM per archive, a keyless cosign signature over `checksums.txt`, and `actions/attest-build-provenance` over the archives, the bundle and the checksum file. README documents verifying all three from outside.
- `redact.Redactor.Literal` registers a value the caller knows is tenant data under a kind, so it gets a stable `{id 2}` placeholder even when no pattern matches it. Every pattern in that package is anchored on a shape, so a value that is tenant data and takes some other shape passed straight through. `Redactor.Secrets` applies the secrets tier alone, for output meant to carry real ids.
- `docs/configuration.md`, `docs/development.md`, `docs/security.md` and `CODE_OF_CONDUCT.md`, with a table in the README pointing at each. Configuration carries every setting and what startup does with it; security carries the trust boundaries, what reaches a log, what `doctor` prints and what the live driver's redactor measurably cannot do.
- `staleness` reads ten documents instead of six and holds three more claims, each derived from the code rather than from a list typed into the checker: every gate in the registry is named in `docs/development.md`, every `FAVRO_*` the source reads is documented in `docs/configuration.md`, and a prose count of the delete-style tools must match the number the binary annotates.
- Issue forms for bugs and features, and `SECURITY.md`. The bug form asks for `doctor` output and says what not to paste; the leak gate caught the example in its own first draft.

### Changed
- `CLAUDE.md` restructured to the sibling shape — mission, hard rules, where things go, definition of done — and now points at `docs/architecture.md` for anything it used to summarise.
- `make ci` is now `make check`, and `gates parity` fails if it and `ci.yml` stop running the same set — every one of `check`'s twenty-two prerequisites, not just the gates, matched by what each recipe runs rather than by target name.
- Every GitHub Action is pinned to a full commit SHA with the version in a trailing comment, and every tool it installs is pinned to one exact version; `gates pins` holds both, plus the workflow-level `shell: bash` the Windows runner needs.
- govulncheck runs in source mode again, pinned to v1.8.0. The binary-mode workaround existed because v1.7.0's analysis could not parse the Go 1.27 stdlib; v1.8.0 can, and source mode analyses call paths rather than a symbol table.
- `scripts/changelog-section.sh` and `scripts/package-plugin.sh` are now `gates release-notes` and `gates plugin-pack`. The packer gains what the shell version could not have: the launcher's platform table is the table the gate reads, so a renamed binary fails on the commit that renames it rather than for every user of that platform.
- The licence check ignores `github.com/segmentio/asm` by path — it relicensed to MIT-0, which go-licenses v1.6.0's classifier does not recognise and no allowlist value can match.
- Every tool error is now `[class] actionable message`, from the closed vocabulary. A 429 carries `retry_after_seconds` in the text, since a failed call has no `structuredContent` to put it in.
- Tool results send a readable `content` block and a machine-readable `structuredContent` one. They used to be the same bytes: the SDK copies the marshalled output into a text block when a handler leaves `Content` unset, and every handler did.
- Package layout split to the shape the sibling servers use: `internal/favro` is the wire types alone and `internal/favroapi` the REST client; `internal/server` is split into `internal/tools` (the MCP surface), `internal/service` (resolution, search, the full-card fan-out, description editing) and `internal/server` (SDK wiring, two files). Direction runs one way — `server` → `tools` → `service` → `favroapi` → `favro` — with `config`, `cache`, `auth` and `render` as leaves.
- `internal/favroapi`'s typed errors name their own class, rather than having one read off them by a switch in `internal/render`. An eighth error type can no longer reach the fallback class in silence.
- The Resolver's cache invalidation is exported API (`InvalidateTagCache` and the rest). A write tool in another package has to call it, and the rule that it must was already the load-bearing one.
- Tests use the stdlib rather than testify: 2,173 assertions rewritten to `if got != want { t.Errorf(…) }`. `pmezard/go-difflib` is gone and `stretchr/testify` is no longer a direct dependency.
- The `unified_diff` that description-editor tools return is a correct unified diff now. go-difflib appended a synthetic empty line, which showed as a stray context line at the end of a hunk, and omitted `\ No newline at end of file`; both are fixed, so the output matches `diff -u` byte for byte.
- List tools are now genuinely 1-indexed, as their schema has always said. `page` went to Favro untouched and Favro counts from zero, so asking for page 1 returned the second page and the first was never seen — a valid page of real results with rows silently absent. `page` and `next_page` in responses count from one to match.
- Releases build a macOS universal binary, kept out of the ordinary archives by `ids` so the release page does not offer a fourth macOS download. A bundle manifest names a command per platform and has no key for the architecture, which is what makes it necessary.
- Builds are reproducible: `mod_timestamp` is the commit's own timestamp, so rebuilding a tag gives byte-identical archives and `sha256sum -c` on a rebuild means something.
- Both bundles share one zip writer, one version stamper and one binary-agrees-with-its-manifest check (`scripts/gates/bundle.go`); `plugin.go` is 87 lines shorter. The `.plugin` gains the atomic write the `.mcpb` had and stops shipping a manifest re-encoded from a Go map — which alphabetised the keys out of the order anyone wrote them in.
- A binary installed with `go install` reports its real version. `go install` applies no ldflags, so it said `dev (unknown)` and no bug report from one could be tied to a build; it falls back to the module version and VCS revision Go embeds, and `doctor` names which of the three sources answered.

### Removed
- `favro_get_organization` no longer takes `organization_id`; it returns the organization the server is bound to. Breaking, and the input never worked: Favro documents it as "the id of the organization to be retrieved. Required." and then ignores it, routing by the `organizationId` header instead — so any value, a malformed one included, returned the bound organization. A model asking for one organization was handed another with a 200 and nothing to indicate it. Use `favro_list_organizations` to see what the token can reach.
- The fourteen delete-style tools are no longer in `tools/list` by default; set `FAVRO_ENABLE_DESTRUCTIVE=true` to register them. Breaking, and deliberately so: a client-side prompt is not a safety layer, because a host in an auto-approve permission mode runs a tool annotated `destructiveHint` without asking and the MCP spec says clients treat tool annotations as untrusted. No tool input changed, and one environment variable restores the previous surface.

### Fixed
- A mistyped subcommand fails instead of starting the server. `favro-mcp zzz` ran the server and exited 0, because `flag` stops at the first non-flag argument and the stray word reached nothing — so a script driving the binary took a typo for success. Reported by a contributor against the sibling servers.
- The gates run on macOS and Windows. Two gates and a test handed `git grep -E` patterns using `\s` and `\b`, which POSIX ERE does not have, so on macOS they matched nothing and git's "no matches" exit was read as a broken repository; they grep a fixed substring and apply the pattern in Go now. The `.mcpb` test fixture staged a shell script named `favro-mcp.exe`, which Windows cannot execute, and builds a real binary instead. `GITLEAKS_VERSION` dropped its `v`, because the action prepends one and `vv8.30.1` is a 404.
- `make live` returns. The driver closed the read end of the server's stdin rather than the write end — `StdinPipe` sets `cmd.Stdin` to the read side and returns the write side, and a type assertion to a Closer succeeds on either — so the server never saw EOF, never exited, and `cmd.Wait` never returned. Every run printed its summary and then hung, holding the server process open. The run itself takes eight seconds.
- `redact.Redactor.Count` counted values that were registered rather than values that were replaced, so a report could claim it redacted three things while redacting none — inverting the one number that exists to tell "redacted nothing" from "stopped redacting".
- `favro_get_card`, `favro_list_cards` and `favro_get_card_full` failed with a protocol error on any card carrying a Vote, Members, Tags, Status or Multiple-select custom field. `json.RawMessage` is `[]byte`, so schema inference described those values as arrays of integers 0-255 and the SDK rejected the real payload. Found by a live read; no fixture could have caught it, because a fixture that sends what the schema claims agrees with the bug.
- The server exited 1 whenever a host closed the stdio pipe, which every host logs as a crash. The SDK reports a disconnect as JSON-RPC −32004 with the EOF only as message text, so `errors.Is(err, io.EOF)` never matched it; the code is matched now.
- Test fixtures used an address at a registrable domain (`e.com`); they use `example.test`.
- README documents `FAVRO_LOG_LEVEL` and `FAVRO_MCP_SKIP_VALIDATE`, which the binary has always read.
- The `User-Agent` header is sent again. A scripted rename during the package split rewrote the literal `"User-Agent"` into `"favro.User-Agent"`, so requests carried Go's default agent and a junk header; nothing asserted it, so no test failed.

### Security
- `docs/security.md` says that the attachment-upload tools read any file the account can read, that both are registered by default, and that the containment is therefore the account rather than the Favro token. No code change: that is what the tools are for, and the previous omission was the document implying a bound it does not have.
- A release tag can no longer carry a command substitution into the release job. `${{ github.ref_name }}` was interpolated into a `run:` line — an expression is substituted as text before bash parses it, and double quotes stop word-splitting but not `$(…)` — while the trigger admitted any suffix after a hyphen and git permits `$`, `` ` ``, `;` and `{}` in a ref name. Harmless-ish while the job only uploaded assets; this release adds `id-token: write` to it, so the same tag could have minted the repository's Sigstore identity and had a tampered artifact signed and attested under it. Three layers now: the tag goes through the environment, the trigger admits only alphanumeric suffixes, and `gates release-notes` refuses a version that is not one.
- The debug request log no longer carries the query string or the path's ids. Favro addresses everything by id in both halves of the URL — `cardCommonId`, `widgetCommonId` and `sequentialId` in the query, `/cards/{cardId}` in the path — so a debug log reconstructed which cards a session touched. The parameter names and the endpoint shape are logged instead.
- The `organizationId` header is redacted in the debug log and in dry-run records. `Token.Apply` sets it on every request, so it was reprinted on every debug line — and a test asserted that it passed through unredacted.
- The startup line no longer logs `organization_id`, which named the tenant at INFO in the first line of every session. `favro_ping` still returns it.

## [1.1.2] - 2026-08-27

Corrects the `favro_set_card_custom_field` description, which read as though its
writes were verified. No tool inputs changed.

### Added
- `cmd/favro-mcp` coverage 22% → 79%: log-level parsing, flag parsing, credential-resolution failures, every `auth` subcommand against a mocked keyring, and the binary's exit code on a failed start.

### Changed
- Release notes now lead with the version's `CHANGELOG.md` section, passed to goreleaser as `--release-header`; GitHub's generated PR list stays underneath. Published releases had been the PR list alone, so consumers saw internal phase numbering and never the changelog. `scripts/changelog-section.sh` extracts the section and fails the release if the version has none.
- Repo process, no effect on the shipped server: branch protection enforced on `main` (PR required, eight status checks, no bypass actors); `CONTRIBUTING.md` and the PR checklist corrected to match, the checklist now pointing at `docs/TOOLS.md` rather than a tool inventory that does not exist; `.mcp.json` gitignored, with the snippet documented in CONTRIBUTING instead.

### Fixed
- `favro_set_card_custom_field` now says in its description that the per-type body shapes are unconfirmed against a live tenant, that Favro answers 200 for a body it ignored, and that a field not enabled on the widget is accepted and discarded. README carried this caveat; the tool description an LLM actually reads did not.
- `README.md` said building from source needs Go 1.26+ and credited a `toolchain` directive in `go.mod` for bumping collaborators. `go.mod` declares `go 1.27.0` and has no `toolchain` directive, so both halves were wrong and contradicted the Go 1.27 requirement stated higher up the same file.
- `.goreleaser.yaml` carried a `sort` and five `filters.exclude` patterns that `use: github-native` ignores, so the `chore:` commits it claimed to drop appeared in every published release anyway. Removed, with a comment recording why.

## [1.1.1] - 2026-08-26

Docs, tests, and one attachment fix. No tool inputs changed.

### Added
- `TestMCP_AllTools_SmokeCallable`: calls all 83 tools over the MCP transport with minimal input, `dry_run: true` for mutating ones. Fails if a registered tool has no entry, so new tools can't skip coverage.
- `CLAUDE.md`: repo conventions for Claude Code — commands, layout, the Favro-returns-200-for-ignored-bodies trap, and the docs standards below.

### Changed
- `CHANGELOG.md` follows Keep a Changelog properly: standard categories in order, ISO dates, link references. Entries rewritten to one line each — 57KB to 8KB, same facts.
- `README.md` trimmed to a scannable overview with badges; the 83-row tool table moved to `docs/TOOLS.md`.
- `CONTRIBUTING.md` documents the changelog, versioning and release rules, and the Go 1.27 / golangci-lint ≥ v2.13.1 requirement.

### Fixed
- Attachment removal now strips the presigned query from the URL before sending. Favro re-mints `fileURL` on every read with a fresh `X-Amz-Signature`, so the value a caller reads back never matches what Favro stored — v1.1.0's "pass the fileURL" fix couldn't work either. Applies to `favro_remove_attachment` and `favro_update_comment`.

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

[Unreleased]: https://github.com/mmedum/favro-mcp/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/mmedum/favro-mcp/compare/v1.1.2...v2.0.0
[1.1.2]: https://github.com/mmedum/favro-mcp/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/mmedum/favro-mcp/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/mmedum/favro-mcp/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/mmedum/favro-mcp/releases/tag/v1.0.0
