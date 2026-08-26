# favro-mcp

A Model Context Protocol (MCP) server for [Favro](https://favro.com), written in Go. Speaks MCP over stdio, exposes Favro's REST API as typed tools, and ships with workflow tools designed for natural-language use (search, name→ID resolution, surgical description edits) so an LLM can act on Favro without burning the rate-limit budget on lookup roundtrips.

> **Unofficial.** Not affiliated with or endorsed by Favro.

## Status

**v1.1** — API-parity release. See [CHANGELOG.md](./CHANGELOG.md) for the full history phase by phase. 83 MCP tools registered covering every Favro REST resource — including tasks, tasklists, dependencies and activities — plus workflow tools (search, name resolution, surgical description edits) for natural-language use.

## Installation

### Cowork plugin (recommended)

Each tagged release publishes a single multi-arch `favro-mcp.plugin` zip on the [GitHub Releases page](https://github.com/mmedum/favro-mcp/releases). The bundle contains binaries for darwin amd64/arm64 and linux amd64/arm64, plus a launcher shim that picks the right one at runtime.

1. Download `favro-mcp.plugin` from the release.
2. Install it into your Cowork profile (drag-and-drop, or follow Cowork's plugin install flow).
3. Authenticate (see [Authentication](#authentication) below).
4. Restart Cowork — the `favro` MCP server will appear with all tools registered.

The bundled `.mcp.json` points at `${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp` so the install is relocatable; no host paths to edit.

### Standalone binary

For non-Cowork hosts (Claude Code via raw `.mcp.json`, MCP Inspector, custom integrations):

1. Download the matching `favro-mcp_<version>_<os>_<arch>.tar.gz` (or `.zip` on Windows) from the release.
2. Extract and place the `favro-mcp` binary somewhere on `$PATH` (or note its absolute path).
3. Authenticate.
4. Point your MCP host's config at the binary — see [MCP host configuration](#mcp-host-configuration) below.

### From source

```
git clone https://github.com/mmedum/favro-mcp
cd favro-mcp
make build       # → bin/favro-mcp
```

Requires Go 1.26+. The `toolchain` directive in `go.mod` auto-bumps collaborators to the matching minor.

## Authentication

The server uses Favro's HTTP Basic Auth (user email + API token), scoped to one Favro organization at startup. Tools never accept `organization_id` — pass it once via env var or keyring and forget about it.

API tokens are user-scoped — for team installs, generate the token from a dedicated service-style Favro user with the minimum permissions you need, not from a personal account.

### Credential resolution

Two sources, checked in order; the first one that produces a complete `(email, token, organization_id)` triple wins:

1. **Environment variables** — `FAVRO_USER_EMAIL`, `FAVRO_API_TOKEN`, `FAVRO_ORGANIZATION_ID`.
2. **OS keyring** — populated once via `favro-mcp auth login` (cross-platform: macOS Keychain, Windows Credential Manager, Linux Secret Service).

### `auth` subcommands

```
favro-mcp auth login       # interactive: masked token input, writes to keyring
favro-mcp auth status      # show user/org (token never printed)
favro-mcp auth logout      # delete keyring entries
favro-mcp auth which       # print active credential source: env or keyring
favro-mcp --version        # print version + commit
favro-mcp --dry-run        # process-wide override: forces all writes into dry-run
```

`favro-mcp auth login` is the one-shot setup for the keyring path. Re-run it to rotate the token.

### MCP host configuration

After the binary or plugin is in place and credentials are stored, register the server in your MCP host config.

**Default (keyring path, zero secrets in config)**:

```json
{
  "mcpServers": {
    "favro": { "command": "/path/to/favro-mcp" }
  }
}
```

**Headless / CI (env vars)** — passes secrets through from the shell, never literal:

```json
{
  "mcpServers": {
    "favro": {
      "command": "/path/to/favro-mcp",
      "env": {
        "FAVRO_USER_EMAIL": "${FAVRO_USER_EMAIL}",
        "FAVRO_API_TOKEN": "${FAVRO_API_TOKEN}",
        "FAVRO_ORGANIZATION_ID": "${FAVRO_ORGANIZATION_ID}"
      }
    }
  }
}
```

The Cowork plugin ships its own `.mcp.json` pointing at `${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp`, so for that install path no manual config is needed.

## Rate limits

Favro enforces tier-based rate limits **per organization** (Lite ~100/hr, Standard ~1000/hr, Enterprise ~10000/hr). A misbehaving agent can exhaust the quota for everyone in your org. The server caches resolution lookups (tags, users, widgets, columns) in-memory and supports `force_refresh` on every list/resolve tool to bust the cache when needed. List tools never auto-aggregate pages — each `next_page` requires an explicit follow-up call so the LLM can stop early.

## Dry-run

Every mutating tool accepts `dry_run: true` and returns the would-be HTTP request (method, URL, body) plus a state-diff prediction without contacting Favro. Read tools never accept `dry_run`.

For sandboxed or pre-autonomy testing, `--dry-run` on the binary forces dry-run mode process-wide regardless of per-call input.

## Tools

The **Phase** column records which build phase shipped the tool; `parity` marks tools added by the API-parity pass that re-checked this client against Favro's published REST docs.

| Tool | Phase | What it does |
| --- | --- | --- |
| `favro_ping` | 1 | Read-only liveness check. Returns server version, the bound Favro organization id, and the active credential source (`env` or `keyring`). Does **not** contact Favro — it's a local diagnostic. |
| `favro_rate_limit_status` | 2 | Read-only. Reports the most recently observed Favro rate-limit headers (`X-RateLimit-Limit/Remaining/Reset/Delay`, plus `Retry-After` on 429). Does **not** contact Favro — surfaces what the client already saw on prior requests so the caller can decide whether to slow down. A non-zero `throttle_delay_seconds` is the early warning: Favro is already stalling responses to refill the token bucket, and starts rejecting with 429 once the needed stall would exceed 10s. |
| `favro_list_organizations` | 3 | Read-only. Lists Favro organizations the API token can see. Optional `page` (1-indexed); surfaces `next_page` for explicit pagination — never auto-aggregates. |
| `favro_get_organization` | 3 | Read-only. Returns a single Favro organization by id. |
| `favro_list_users` | 3 | Read-only. Lists members of the bound Favro organization. Optional `page` + `request_id`. |
| `favro_get_user` | 3 | Read-only. Returns a single Favro user by id. |
| `favro_list_collections` | 3 | Read-only. Lists collections in the bound Favro organization. Optional `page` + `request_id`. |
| `favro_get_collection` | 3 | Read-only. Returns a single Favro collection by id. |
| `favro_list_widgets` | 3 | Read-only. Lists widgets (boards). Optional `collection_id` filter, plus `page` + `request_id`. |
| `favro_get_widget` | 3 | Read-only. Returns a single Favro widget by its widgetCommonId. |
| `favro_list_columns` | 3 | Read-only. Lists columns (status lanes) on a single widget. `widget_common_id` is required (Favro rejects unfiltered listings with 400); also accepts `page` + `request_id`. |
| `favro_get_column` | 3 | Read-only. Returns a single Favro column by its columnId. |
| `favro_list_cards` | 3 | Read-only. Lists Favro cards. Optional filters: `widget_common_id`, `collection_id`, `card_common_id`, `sequential_id` (integer of 'BSC-123' refs), plus `unique` to dedupe across widgets. Page + `request_id` for pagination. |
| `favro_get_card` | 3 | Read-only. Returns a single Favro card by its per-widget `cardId`. Favro's GET endpoint 403s on a `cardCommonId` — to fetch by cross-widget id, use `favro_list_cards` with the `card_common_id` filter. |
| `favro_list_comments` | 3 | Read-only. Lists comments on a single Favro card. `card_common_id` is required (Favro scopes /comments by the cross-widget cardCommonId); also accepts `page` + `request_id`. |
| `favro_get_comment` | 3 | Read-only. Returns a single Favro comment by its commentId. |
| `favro_list_tags` | 3 | Read-only. Lists all tags in the active organization (tags are org-global; no widget or card scope). Accepts `page` + `request_id`. |
| `favro_get_tag` | 3 | Read-only. Returns a single Favro tag by its tagId. |
| `favro_list_custom_fields` | 3 | Read-only. Lists all custom fields in the active organization (org-global). Each entry includes the field's `type` and, for select-flavored types, a `customFieldItems` list of legal options. Accepts `page` + `request_id`. |
| `favro_get_custom_field` | 3 | Read-only. Returns a single Favro custom field by its customFieldId. |
| `favro_list_groups` | 3 | Read-only. Lists all groups (named user collections) in the active organization. Each entry includes the group's `members` list (`{userId, role}` pairs). Accepts `page` + `request_id`. |
| `favro_get_group` | 3 | Read-only. Returns a single Favro group by its groupId. |
| `favro_resolve_tag` | 4 | Read-only. Resolves a tag name (or fragment) to ranked tagId candidates. Case-insensitive: exact match scores 1.0, prefix 0.7, substring 0.4. Default `limit` 10, max 50. Tag list is cached for 5 minutes; pass `force_refresh: true` to bypass. |
| `favro_resolve_user` | 4 | Read-only. Resolves a user name OR email (the better-of-the-two score) to ranked userId candidates. Email is included in each candidate so an LLM can disambiguate. 5-minute cache; same score scale as `favro_resolve_tag`. |
| `favro_resolve_collection` | 4 | Read-only. Resolves a collection name to ranked collectionId candidates. 60-second cache (collections are added/renamed mid-session). |
| `favro_resolve_widget` | 4 | Read-only. Resolves a widget name to ranked widgetCommonId candidates. Optional `collection_id` restricts results to widgets in that collection (applied client-side after the org-wide list is fetched). 60-second cache. |
| `favro_resolve_column` | 4 | Read-only. Resolves a column name to ranked columnId candidates **on a given widget** — `widget_common_id` is required because column names ('Doing', 'Done') repeat across widgets. Position is included in each candidate. 60-second per-widget cache. |
| `favro_resolve_custom_field` | 4 | Read-only. Resolves a custom field name to ranked customFieldId candidates. Each candidate includes the field's `type` ('Single select', 'Date', 'Members', etc.) for disambiguation. 5-minute cache. |
| `favro_resolve_group` | 4 | Read-only. Resolves a group name to ranked groupId candidates. 5-minute cache. |
| `favro_search_cards` | 4 | Read-only. Local full-text search over card name + description (Favro has no native FT search). Exactly one of `widget_common_id` / `collection_id` is required — Favro's `/cards` endpoint rejects unfiltered listings, so org-wide search isn't supported; resolve a widget or collection first if you don't know one. Markdown is stripped before matching. Score scale: name phrase +1.0, name token overlap up to +0.6, body phrase +0.5, body token overlap up to +0.5 (additive). 60-second per-(scope, include_archived) cache; pass `force_refresh: true` to bypass. `column_name` is intentionally omitted from each hit — chain to `favro_get_card_full` for per-card metadata. |
| `favro_get_card_full` | 4/7 | Read-only. Fetches one card with every id field dereferenced into a human-readable name: tags → tag names, assignees → user name + email, widgetCommonId → widget name, columnId → column name, parent collectionIds → collection names, customFieldId → field name + display value. Custom-field display values cover Text, Number, Date, Date created, Checkbox, Link (`text (url)`), Single select, Multiple select, Status (name + color), Members (resolved to user names), Rating (`n / 5`), Time (`14h 0m`), Timeline (`start → due`), Vote (resolved voter names, else a count), Color, Progress (percent), Tags (resolved to tag names), Sequential ID (auto-counter value), and Relations (count summary; raw IDs available via `raw`). Each formatter accepts both the shape Favro's docs describe and the one earlier revisions of this client assumed, so a value decodes whichever way Favro actually sends it. Unknown future types pass through with `dereferenced: false`. Saves 4–7 follow-up calls. Pass exactly one of `card_id` (per-widget), `card_common_id` (cross-widget), or `sequential_id` (integer of a 'BSC-123' ref). Comments off by default; `include_comments: true` fetches the first page (cap with `comment_limit`, default 20). |
| `favro_create_tag` | 5 | Mutating. Creates a new org-global tag. Required: `name`. Optional: `color` (palette: blue, red, green, lime, purple, cyan, brown, orange, gray, pink, yellow, slategray; omit to let Favro pick). Favro does not enforce name uniqueness — call `favro_resolve_tag` first if you want idempotent behavior. Successful live writes invalidate the org's tag cache. Pass `dry_run: true` to preview the request (method + URL + body + predicted state change) without contacting Favro; the binary's `--dry-run` flag forces dry-run process-wide. |
| `favro_delete_tag` | 5 | **Destructive.** Deletes an org-global tag by `tag_id` and removes it from every card it was applied to. Resolve via `favro_resolve_tag` if you only have the name. On success the tag cache is invalidated. Pass `dry_run: true` to preview the request without contacting Favro. |
| `favro_update_tag` | 5 | Mutating. Updates an existing tag's `name` and/or `color`; both fields optional. Changes propagate to every card the tag is applied to. On success the tag cache is invalidated. Pass `dry_run: true` to preview. |
| `favro_update_tags` | 7 | Mutating. Apply multiple tag updates in one tool call. Favro has no real bulk-tag endpoint (verified live, Phase 7.2 — `PUT /tags` returns the SPA fallback HTML), so this fans out to N parallel `PUT /tags/{tagId}` calls under the hood. Same total round-trips as N sequential `favro_update_tag` calls, but parallelized. Each entry needs `tag_id` plus at least one of `name` / `color`. Returns updated tags in input order. On the first per-entry error remaining in-flight requests cancel — partial successes may have already landed; the wrapped error names the offending tagId and index. Live success invalidates the tag cache. Pass `dry_run: true` to preview. |
| `favro_create_comment` | 5 | Mutating. Adds a new comment (markdown body) to the card identified by `card_common_id`. Pass `dry_run: true` to preview. Comments are not cached at the resolver layer, so no invalidation. |
| `favro_update_comment` | 5 | Mutating. Replaces the body of an existing comment by `comment_id`. Whole-body replace; surgical edit-in-place is deferred to Phase 6. `remove_attachments` detaches files by their `fileURL`. Pass `dry_run: true` to preview. |
| `favro_delete_comment` | 5 | **Destructive.** Deletes a comment by `comment_id`. Pass `dry_run: true` to preview. |
| `favro_create_card` | 5 | Mutating. Creates a new card. Required: `name`. Optional: `widget_common_id` (omit to land on the authenticated user's todo list), `column_id`, `lane_id`, `parent_card_id`, `detailed_description` (markdown), `list_position` / `sheet_position` (`top` / `bottom` / `above:cardId` / `below:cardId` / numeric string), `assignment_ids`, `tag_ids` (raw IDs only — by-name auto-create is gated to Phase 6's `favro_add_tag_to_card`), `start_date`, `due_date`. Successful live writes invalidate the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_update_card` | 5 | Mutating. Updates a card by per-widget `card_id`. Every body field optional — pass at least one. Mirrors the favro layer kitchen-sink update: name, description (whole-body replace; surgical edits are Phase 6), widget/column/lane/parent moves, drag_mode, list_position / sheet_position, add/remove assignment_ids, add/remove tag_ids, start/due dates. Prefer `favro_move_card` for relocations and `favro_archive_card` / `favro_unarchive_card` for archive flips — clearer LLM intent. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_archive_card` | 5 | Mutating. Convenience over `favro_update_card` with `archive: true`. Reversible via `favro_unarchive_card`. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_unarchive_card` | 5 | Mutating. Convenience over `favro_update_card` with `archive: false`. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_move_card` | 5 | Mutating. Relocates a card to a different widget, column, and/or lane. At least one of `widget_common_id` / `column_id` / `lane_id` must be set; an empty move short-circuits with a typed error. Optional `drag_mode` (`commit` re-shuffles siblings, `move` doesn't). Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_delete_card` | 5 | **Destructive.** Deletes a card by per-widget `card_id`. Default `everywhere: false` removes only this widget's instance — other widgets sharing the same `cardCommonId` keep their copies. `everywhere: true` purges across every widget — irreversible. Returns the list of `cardIds` Favro deleted. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_create_collection` | 5 | Mutating. Creates a new collection. Required: `name`. Optional: `color`, `background`, `icon_name`, `public_sharing` (`users`/`organization`/`public`), `full_members_can_add_widgets`, `star_page`, `share_to_users`. Note the wire asymmetry Favro documents: members are *read* under `sharedToUsers` but *invited* under `shareToUsers` — posting the read-shaped key is accepted and silently drops the invites. Live success invalidates the collection cache. Pass `dry_run: true` to preview. |
| `favro_update_collection` | 5 | Mutating. Updates a collection by `collection_id`. Every body field optional. `archive: true/false` toggles the archive flag. Membership uses two separate lists: `share_to_users` invites people who aren't in the collection yet, `members` re-roles or (with `delete: true`) removes people who already are. Live success invalidates the collection cache. Pass `dry_run: true` to preview. |
| `favro_delete_collection` | 5 | **Destructive.** Deletes a collection by `collection_id`. Favro does not cascade-delete widgets — widgets that lived only in the deleted collection may be left orphaned. Live success invalidates the collection / widget / search-cards caches. Pass `dry_run: true` to preview. |
| `favro_create_widget` | 5 | Mutating. Creates a new widget (board) inside a collection. Required: `collection_id`, `name`. Optional: `type` (`backlog`/`board`/`calendar`/`table`/`matrix` — Favro picks `backlog` when omitted), `color`, `breakdown_card_common_id`, `owner_role`, `edit_role`. Live success invalidates the widget cache. Pass `dry_run: true` to preview. |
| `favro_update_widget` | 5 | Mutating. Updates a widget by `widget_common_id`. Every body field optional. `archive: true/false` toggles the archive flag and **requires `collection_id`** — a widget can belong to several collections and Favro scopes the archive to one of them; the client rejects a bare archive before any HTTP work. Live success invalidates the widget cache. Pass `dry_run: true` to preview. |
| `favro_delete_widget` | 5 | **Destructive.** Deletes a widget by `widget_common_id`. Cards on the widget are removed; columns become inaccessible. Optional `collection_id` scopes the delete to one collection; omitting it deletes every instance across all collections the widget belongs to. Live success invalidates the widget / column / search-cards caches. Pass `dry_run: true` to preview. |
| `favro_create_column` | 5 | Mutating. Creates a column on a widget. Required: `widget_common_id`, `name`. Optional: `color`, `position` (0-based, omit to append). Live success invalidates the per-widget column cache. Pass `dry_run: true` to preview. |
| `favro_update_column` | 5 | Mutating. Updates a column by `column_id`. Every body field optional. Pass optional `widget_common_id` to scope cache invalidation; otherwise every cached column list is dropped on success. Pass `dry_run: true` to preview. |
| `favro_delete_column` | 5 | **Destructive.** Deletes a column by `column_id`. Favro rejects this with HTTP 400 if the column still has cards on it — move or archive the cards out first. Pass optional `widget_common_id` to scope cache invalidation. Pass `dry_run: true` to preview. |
| `favro_create_group` | 5 | Mutating. Creates a new org-global group. Required: `name`. Optional: `members` (each `{userId, role}`; role is `administrator` / `member` / `viewer`). Live success invalidates the group cache. Pass `dry_run: true` to preview. |
| `favro_update_group` | 5 | Mutating. Updates a group by `group_id`. `name` and `members` both optional. **`members`, when set, REPLACES the list** — compose the full intended list and drop anyone who should leave. (Favro's docs describe add/remove deltas with a per-entry `delete` flag, but a live test observed whole-list replacement; sending the full list is correct under either reading.) Each entry identifies a person by `userId` or `email` plus a role. Live success invalidates the group cache. Pass `dry_run: true` to preview. |
| `favro_delete_group` | 5 | **Destructive.** Deletes a group by `group_id`. Removes it from any sharing / assignment / custom-field references it appeared in. Live success invalidates the group cache. Pass `dry_run: true` to preview. |
| `favro_set_card_custom_field` | 5/7/parity | Mutating. Sets a single custom-field value on a card. Supply exactly one *kind* of value input, matching the resolved field's Type: Text → `text`; Number → `number`; Date → `date` (ISO 8601); Checkbox → `checkbox`; Vote → `vote`; Color → `color`; Single select → `single_select_item_id`; Status → `status_item_id`; Multiple select → `multi_select_item_ids` (replaces the selection); Members → `add_member_user_ids` / `remove_member_user_ids`; Tags → `add_tag_ids` / `remove_tag_ids`; Rating → `rating_value` (Favro fixes the scale at 0–5); Link → `link_url` + optional `link_text`; Timeline → `timeline_start_date` + `timeline_due_date`; Time → `time_report_ms`. Members and Tags are add/remove deltas — Favro's API has no replace operation for them. Only the by-id tag forms are exposed: Favro's `addTags` takes names and creates unknown ones, which is the typo foot-gun the card-level tag tools hard-fail to prevent. Progress and Sequential ID are calculated server-side and reject writes. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_append_card_description` | 6 | Mutating. Appends markdown text to a card's description (separated by a blank line, preserving paragraph boundaries). Reads with `descriptionFormat=markdown` so the existing body comes back in edit-correct form. Returns `{old, new, unified_diff}` so the LLM can confirm the change. Live success invalidates the search-cards cache. `dry_run: true` previews the diff without writing. |
| `favro_prepend_card_description` | 6 | Mutating. Inserts markdown text at the top of the description, followed by a blank-line separator. Same return shape as append. |
| `favro_replace_in_card_description` | 6 | Mutating. Replace text in the description. Default `count: 1` so a common substring doesn't accidentally rewrite every occurrence; pass `count: 0` to replace all. `use_regex: true` enables Go-regex with `$N` backrefs. Refuses to PUT when `find` matches nothing — surfaces a typed error so a typo doesn't silently no-op. Same return shape as append / prepend. |
| `favro_add_comment_to_card` | 6 | Mutating. Adds a comment to a card identified by one of `card_common_id` / `card_id` / `sequential_id` / `search_query`. With `search_query`, also pass `widget_common_id` OR `collection_id` for scope; the tool refuses ambiguous matches (top-2 search scores within 0.2) by returning the candidate list so the LLM can re-run with an explicit `card_common_id`. Comments aren't cached at the resolver layer. |
| `favro_add_tag_to_card` | 6 | Mutating. Adds an existing tag to a card by tag NAME. **Hard-fails on unknown names** — typo prevention is the whole point. To add a brand-new tag, call `favro_create_tag` first. To bypass the exact-match guard (or to use a tagId directly), use `favro_update_card` with `add_tag_ids`. |
| `favro_remove_tag_from_card` | 6 | Mutating. Removes a tag from a card by tag NAME. Same hard-fail semantics as add. |
| `favro_upload_attachment` | 7 | Mutating. Uploads a local file as an attachment on a card via raw-bytes POST. Inputs: `card_id`, `file_path` (absolute), optional `filename` (defaults to the file's basename) and `mime_type` (omit to let Favro infer from the extension). Local file paths only — base64-inline body is deferred. 8 MiB upload cap enforced locally. Returns the created attachment object `{name, fileURL}` (Favro echoes the attachment, not the updated Card — verified live). Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_upload_comment_attachment` | parity | Mutating. Same contract as `favro_upload_attachment`, addressed by `comment_id` — the file lands on a comment rather than on the card itself. |
| `favro_remove_attachment` | parity | **Destructive.** Detaches files from a card. Favro has no per-attachment DELETE; removal rides on `removeAttachments` in `PUT /cards/{cardId}`, and the list is matched by attachment **URL**, not display name — pass the `fileURL` values from `favro_get_card_full` or from an upload response. Favro returns 200 whether or not anything matched, so verify by re-reading the card. Not yet verified against a live tenant. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_list_tasks` | parity | Read-only. Lists checklist items on a card. Favro calls checklist items "tasks" and the checklists that hold them "tasklists". `card_common_id` required (cross-widget identity, not `card_id`); `task_list_id` narrows to one checklist. `favro_get_card_full` already reports done/total counts — reach for this when the item names matter. Optional `page` + `request_id`. |
| `favro_get_task` | parity | Read-only. Returns a single checklist item by `task_id`. |
| `favro_create_task` | parity | Mutating. Adds a checklist item to an existing checklist (`task_list_id`). Items belong to a checklist, not directly to a card — create one with `favro_create_tasklist` first. Omit `position` to append. Pass `dry_run: true` to preview. |
| `favro_update_task` | parity | Mutating. Updates a checklist item by `task_id`. `completed: true` ticks it, `completed: false` un-ticks it. Pass `dry_run: true` to preview. |
| `favro_delete_task` | parity | **Destructive.** Deletes a checklist item by `task_id`. Pass `dry_run: true` to preview. |
| `favro_list_tasklists` | parity | Read-only. Lists the checklists on a card. `card_common_id` required. Optional `page` + `request_id`. |
| `favro_get_tasklist` | parity | Read-only. Returns a single checklist by `task_list_id`. |
| `favro_create_tasklist` | parity | Mutating. Adds a checklist to a card. Pass `tasks` to seed it with items in the same request instead of one `favro_create_task` call per item. Pass `dry_run: true` to preview. |
| `favro_update_tasklist` | parity | Mutating. Renames or reorders a checklist. Its items are managed separately via the task tools. Pass `dry_run: true` to preview. |
| `favro_delete_tasklist` | parity | **Destructive.** Deletes a checklist and every item in it. Pass `dry_run: true` to preview. |
| `favro_list_dependencies` | parity | Read-only. Lists a card's before/after links to other cards. Not paginated — Favro returns the full list in one response. |
| `favro_add_dependencies` | parity | Mutating. ADDS dependencies to a card, keeping the ones already there. `is_before: true` means the linked card must be done BEFORE this one. Returns the resulting full list. Pass `dry_run: true` to preview. |
| `favro_replace_dependencies` | parity | **Destructive.** REPLACES a card's dependency list — every existing link is removed and the supplied set becomes the whole list. Use `favro_add_dependencies` to add without clearing. Pass `dry_run: true` to preview. |
| `favro_update_dependency` | parity | Mutating. Flips the direction of one existing dependency. Returns the resulting full list. Pass `dry_run: true` to preview. |
| `favro_delete_dependency` | parity | **Destructive.** Removes one dependency link. The cards themselves are untouched. Pass `dry_run: true` to preview. |
| `favro_delete_all_dependencies` | parity | **Destructive.** Removes every dependency link from a card. Pass `dry_run: true` to preview. |
| `favro_list_card_activities` | parity | Read-only. Reads a card's activity history — who changed what, and when. Answers "when did this move to Done", "who reassigned this", "what changed since Friday". `card_id` is the per-widget id, NOT `card_common_id`. Narrow with `since` / `until` (ISO 8601) rather than paging through everything. Which fields an entry carries depends on its `type`; `by_user_id` resolves via `favro_get_user`. Optional `page` + `request_id`. |

## Troubleshooting

| Symptom | Diagnosis & fix |
| --- | --- |
| `authentication failed — check FAVRO_USER_EMAIL and FAVRO_API_TOKEN env vars` on startup | Either no credentials configured, or the token was revoked / rotated. Run `favro-mcp auth which` to confirm which source the server is reading, then re-run `favro-mcp auth login` (keyring path) or update the env vars. |
| `FAVRO_ORGANIZATION_ID is required` on startup | The server is single-org by design — it needs to know which org to scope every request to before it can start. Set `FAVRO_ORGANIZATION_ID` (or include it in `auth login`) and restart. |
| HTTP 429 / `rate limit exceeded` | Hit the per-org Favro rate limit. The client retries once honoring `Retry-After` (capped at 30s) and then surfaces a typed error with `retry_after_seconds`. Use `favro_rate_limit_status` to inspect the most recent `X-RateLimit-*` headers without spending another call. |
| `next_page` keeps coming back non-null | List tools never auto-aggregate. Each follow-up call must include `page` plus the same `request_id` (Favro routes paginated reads via `X-Favro-Backend-Identifier`); some tools also require resending the original filters. |
| `decodeJSONLenient` errors with `content-type: text/html, body-prefix: "<p>It looks like…"` | You hit a Favro endpoint that doesn't exist on the documented surface — Favro returns the SPA fallback page instead of a 404. The error includes status + content-type + body-prefix so you can diagnose in one round-trip. |
| A custom-field write returns 200 but the value doesn't change | Two known causes. First, the field may not be enabled on that widget — org-global custom fields still need per-widget enablement in the Favro UI, and writes to a field the widget doesn't have are accepted and ignored. Second, Favro ignores unknown body fields silently, so a wrong per-type shape looks exactly like success; `favro_set_card_custom_field` sends the shapes Favro's REST docs specify, but they have not all been confirmed against a live tenant. Re-read the card with `favro_get_card_full` to confirm. |
| Tool descriptions have stale data after a successful write | All cache invalidations are per-org-scoped and per-resource-type; if a write succeeds but the next read still shows old data, pass `force_refresh: true` to the list/resolve tool. |
| Linux launcher doesn't run from the plugin | The launcher is a bash script. If your shell can't exec it, point your `.mcp.json` directly at `${CLAUDE_PLUGIN_ROOT}/bin/linux-amd64/favro-mcp` (or `linux-arm64`) instead. |
| Windows | The bundled `bin/favro-mcp.cmd` shim exec's `bin\windows-amd64\favro-mcp.exe`. Windows hosts resolve `${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp` to the `.cmd` automatically via PATHEXT, so the standard `.mcp.json` config in [MCP host configuration](#mcp-host-configuration) works as-is. If your host doesn't honor PATHEXT, point `command` at `${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp.cmd` (or directly at `bin/windows-amd64/favro-mcp.exe`). |

## Development

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
