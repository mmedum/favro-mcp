# favro-mcp

A Model Context Protocol (MCP) server for [Favro](https://favro.com), written in Go. Speaks MCP over stdio, exposes Favro's REST API as typed tools, and ships with workflow tools designed for natural-language use (search, name→ID resolution, surgical description edits) so an LLM can act on Favro without burning the rate-limit budget on lookup roundtrips.

> **Unofficial.** Not affiliated with or endorsed by Favro.

## Status

Pre-release. See [CHANGELOG.md](./CHANGELOG.md) for what's shipped.

Active phase: **Phase 3** — Read CRUD surface (one Favro resource per commit).

## Authentication

The server uses Favro's HTTP Basic Auth (user email + API token). Two credential sources are supported, checked in this order:

1. **Environment variables** — `FAVRO_USER_EMAIL`, `FAVRO_API_TOKEN`, `FAVRO_ORGANIZATION_ID`. Win if set.
2. **OS keyring** — populated once via `favro-mcp auth login` (cross-platform: macOS Keychain, Windows Credential Manager, Linux Secret Service).

The server is locked to one organization at startup. Tools never accept `organization_id`.

API tokens are user-scoped — for team installs, generate the token from a dedicated service-style Favro user with the minimum permissions you need, not from a personal account.

## Rate limits

Favro enforces tier-based rate limits **per organization** (Lite ~100/hr, Standard ~1000/hr, Enterprise ~10000/hr). A misbehaving agent can exhaust the quota for everyone in your org. The server caches resolution lookups (tags, users, widgets, columns) in-memory and supports `force_refresh` on every list/resolve tool to bust the cache when needed.

## Building

```
make build
```

Requires Go 1.26+. The `toolchain` directive in `go.mod` will auto-bump collaborators to the matching minor.

## Running locally

After `favro-mcp auth login`:

```json
{
  "mcpServers": {
    "favro": { "command": "/path/to/favro-mcp" }
  }
}
```

For headless / CI environments, supply env vars instead:

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

## Tools

Phase 1 ships one tool — the rest follow in subsequent phases.

| Tool | Phase | What it does |
| --- | --- | --- |
| `favro_ping` | 1 | Read-only liveness check. Returns server version, the bound Favro organization id, and the active credential source (`env` or `keyring`). Does **not** contact Favro — it's a local diagnostic. |
| `favro_rate_limit_status` | 2 | Read-only. Reports the most recently observed Favro rate-limit headers (`X-RateLimit-Limit/Remaining/Reset`, plus `Retry-After` on 429). Does **not** contact Favro — surfaces what the client already saw on prior requests so the caller can decide whether to slow down. |
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
| `favro_get_card_full` | 4 | Read-only. Fetches one card with every id field dereferenced into a human-readable name: tags → tag names, assignees → user name + email, widgetCommonId → widget name, columnId → column name, parent collectionIds → collection names, customFieldId → field name + display value (for simple types: Text, Number, Date, Checkbox, Link, Single select, Multiple select; long-tail types pass through with the raw value attached). Saves 4–7 follow-up calls. Pass exactly one of `card_id` (per-widget), `card_common_id` (cross-widget), or `sequential_id` (integer of a 'BSC-123' ref). Comments off by default; `include_comments: true` fetches the first page (cap with `comment_limit`, default 20). |
| `favro_create_tag` | 5 | Mutating. Creates a new org-global tag. Required: `name`. Optional: `color` (palette: blue, red, green, lime, purple, cyan, brown, orange, gray, pink, yellow, slategray; omit to let Favro pick). Favro does not enforce name uniqueness — call `favro_resolve_tag` first if you want idempotent behavior. Successful live writes invalidate the org's tag cache. Pass `dry_run: true` to preview the request (method + URL + body + predicted state change) without contacting Favro; the binary's `--dry-run` flag forces dry-run process-wide. |
| `favro_delete_tag` | 5 | **Destructive.** Deletes an org-global tag by `tag_id` and removes it from every card it was applied to. Resolve via `favro_resolve_tag` if you only have the name. On success the tag cache is invalidated. Pass `dry_run: true` to preview the request without contacting Favro. |
| `favro_update_tag` | 5 | Mutating. Updates an existing tag's `name` and/or `color`; both fields optional. Changes propagate to every card the tag is applied to. On success the tag cache is invalidated. Pass `dry_run: true` to preview. |
| `favro_update_tags` | 7 | Mutating. Apply multiple tag updates in one tool call. Favro has no real bulk-tag endpoint (verified live, Phase 7.2 — `PUT /tags` returns the SPA fallback HTML), so this fans out to N parallel `PUT /tags/{tagId}` calls under the hood. Same total round-trips as N sequential `favro_update_tag` calls, but parallelized. Each entry needs `tag_id` plus at least one of `name` / `color`. Returns updated tags in input order. On the first per-entry error remaining in-flight requests cancel — partial successes may have already landed; the wrapped error names the offending tagId and index. Live success invalidates the tag cache. Pass `dry_run: true` to preview. |
| `favro_create_comment` | 5 | Mutating. Adds a new comment (markdown body) to the card identified by `card_common_id`. Pass `dry_run: true` to preview. Comments are not cached at the resolver layer, so no invalidation. |
| `favro_update_comment` | 5 | Mutating. Replaces the body of an existing comment by `comment_id`. Whole-body replace; surgical edit-in-place is deferred to Phase 6. Pass `dry_run: true` to preview. |
| `favro_delete_comment` | 5 | **Destructive.** Deletes a comment by `comment_id`. Pass `dry_run: true` to preview. |
| `favro_create_card` | 5 | Mutating. Creates a new card. Required: `name`. Optional: `widget_common_id` (omit to land on the authenticated user's todo list), `column_id`, `lane_id`, `parent_card_id`, `detailed_description` (markdown), `list_position` / `sheet_position` (`top` / `bottom` / `above:cardId` / `below:cardId` / numeric string), `assignment_ids`, `tag_ids` (raw IDs only — by-name auto-create is gated to Phase 6's `favro_add_tag_to_card`), `start_date`, `due_date`. Successful live writes invalidate the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_update_card` | 5 | Mutating. Updates a card by per-widget `card_id`. Every body field optional — pass at least one. Mirrors the favro layer kitchen-sink update: name, description (whole-body replace; surgical edits are Phase 6), widget/column/lane/parent moves, drag_mode, list_position / sheet_position, add/remove assignment_ids, add/remove tag_ids, start/due dates. Prefer `favro_move_card` for relocations and `favro_archive_card` / `favro_unarchive_card` for archive flips — clearer LLM intent. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_archive_card` | 5 | Mutating. Convenience over `favro_update_card` with `archive: true`. Reversible via `favro_unarchive_card`. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_unarchive_card` | 5 | Mutating. Convenience over `favro_update_card` with `archive: false`. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_move_card` | 5 | Mutating. Relocates a card to a different widget, column, and/or lane. At least one of `widget_common_id` / `column_id` / `lane_id` must be set; an empty move short-circuits with a typed error. Optional `drag_mode` (`commit` re-shuffles siblings, `move` doesn't). Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_delete_card` | 5 | **Destructive.** Deletes a card by per-widget `card_id`. Default `everywhere: false` removes only this widget's instance — other widgets sharing the same `cardCommonId` keep their copies. `everywhere: true` purges across every widget — irreversible. Returns the list of `cardIds` Favro deleted. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_create_collection` | 5 | Mutating. Creates a new collection. Required: `name`. Optional: `color`, `background`, `icon_name`, `public_sharing` (`off`/`organization`/`public`), `full_members_can_add_widgets`, `shared_to_users`. Live success invalidates the collection cache. Pass `dry_run: true` to preview. |
| `favro_update_collection` | 5 | Mutating. Updates a collection by `collection_id`. Every body field optional. `archive: true/false` toggles the archive flag. Live success invalidates the collection cache. Pass `dry_run: true` to preview. |
| `favro_delete_collection` | 5 | **Destructive.** Deletes a collection by `collection_id`. Favro does not cascade-delete widgets — widgets that lived only in the deleted collection may be left orphaned. Live success invalidates the collection / widget / search-cards caches. Pass `dry_run: true` to preview. |
| `favro_create_widget` | 5 | Mutating. Creates a new widget (board) inside a collection. Required: `collection_id`, `name`. Optional: `type` (`backlog`/`board`/`calendar`/`table`/`matrix` — Favro picks `backlog` when omitted), `color`, `breakdown_card_common_id`, `owner_role`, `edit_role`. Live success invalidates the widget cache. Pass `dry_run: true` to preview. |
| `favro_update_widget` | 5 | Mutating. Updates a widget by `widget_common_id`. Every body field optional. `archive: true/false` toggles the archive flag. Live success invalidates the widget cache. Pass `dry_run: true` to preview. |
| `favro_delete_widget` | 5 | **Destructive.** Deletes a widget by `widget_common_id`. Cards on the widget are removed; columns become inaccessible. Live success invalidates the widget / column / search-cards caches. Pass `dry_run: true` to preview. |
| `favro_create_column` | 5 | Mutating. Creates a column on a widget. Required: `widget_common_id`, `name`. Optional: `color`, `position` (0-based, omit to append). Live success invalidates the per-widget column cache. Pass `dry_run: true` to preview. |
| `favro_update_column` | 5 | Mutating. Updates a column by `column_id`. Every body field optional. Pass optional `widget_common_id` to scope cache invalidation; otherwise every cached column list is dropped on success. Pass `dry_run: true` to preview. |
| `favro_delete_column` | 5 | **Destructive.** Deletes a column by `column_id`. Favro rejects this with HTTP 400 if the column still has cards on it — move or archive the cards out first. Pass optional `widget_common_id` to scope cache invalidation. Pass `dry_run: true` to preview. |
| `favro_create_group` | 5 | Mutating. Creates a new org-global group. Required: `name`. Optional: `members` (each `{userId, role}`; role is `administrator` / `member` / `viewer`). Live success invalidates the group cache. Pass `dry_run: true` to preview. |
| `favro_update_group` | 5 | Mutating. Updates a group by `group_id`. `name` and `members` both optional. **`members`, when set, REPLACES the list** — there is no add/remove on this endpoint, so the LLM must compose the full intended list. Live success invalidates the group cache. Pass `dry_run: true` to preview. |
| `favro_delete_group` | 5 | **Destructive.** Deletes a group by `group_id`. Removes it from any sharing / assignment / custom-field references it appeared in. Live success invalidates the group cache. Pass `dry_run: true` to preview. |
| `favro_set_card_custom_field` | 5/7 | Mutating. Sets a single custom-field value on a card. Pass exactly one value-input field matching the resolved field's Type: Text → `text`; Number → `number`; Date → `date` (ISO 8601); Checkbox → `checkbox`; Single select → `single_select_item_id`; Members → `member_user_ids` (replaces the list, `[]` to clear); Status → `status_item_id`; Multiple select → `multi_select_item_ids` (replaces the selection); Rating → `rating_value` + `rating_total` (both required together); Link → `link_url` + optional `link_text`. Resolve customFieldId via `favro_resolve_custom_field`; userIds via `favro_resolve_user`; item ids via `favro_get_custom_field`. Still-deferred types (Tags, Timeline, Voting, Progress, Relations, Sequential ID, Date created) return a typed error. Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |
| `favro_append_card_description` | 6 | Mutating. Appends markdown text to a card's description (separated by a blank line, preserving paragraph boundaries). Reads with `descriptionFormat=markdown` so the existing body comes back in edit-correct form. Returns `{old, new, unified_diff}` so the LLM can confirm the change. Live success invalidates the search-cards cache. `dry_run: true` previews the diff without writing. |
| `favro_prepend_card_description` | 6 | Mutating. Inserts markdown text at the top of the description, followed by a blank-line separator. Same return shape as append. |
| `favro_replace_in_card_description` | 6 | Mutating. Replace text in the description. Default `count: 1` so a common substring doesn't accidentally rewrite every occurrence; pass `count: 0` to replace all. `use_regex: true` enables Go-regex with `$N` backrefs. Refuses to PUT when `find` matches nothing — surfaces a typed error so a typo doesn't silently no-op. Same return shape as append / prepend. |
| `favro_add_comment_to_card` | 6 | Mutating. Adds a comment to a card identified by one of `card_common_id` / `card_id` / `sequential_id` / `search_query`. With `search_query`, also pass `widget_common_id` OR `collection_id` for scope; the tool refuses ambiguous matches (top-2 search scores within 0.2) by returning the candidate list so the LLM can re-run with an explicit `card_common_id`. Comments aren't cached at the resolver layer. |
| `favro_add_tag_to_card` | 6 | Mutating. Adds an existing tag to a card by tag NAME. **Hard-fails on unknown names** — typo prevention is the whole point. To add a brand-new tag, call `favro_create_tag` first. To bypass the exact-match guard (or to use a tagId directly), use `favro_update_card` with `add_tag_ids`. |
| `favro_remove_tag_from_card` | 6 | Mutating. Removes a tag from a card by tag NAME. Same hard-fail semantics as add. |
| `favro_upload_attachment` | 7 | Mutating. Uploads a local file as an attachment on a card via raw-bytes POST. Inputs: `card_id`, `file_path` (absolute), optional `filename` (defaults to the file's basename). v0.1 supports local file paths only — base64-inline body is deferred. 8 MiB upload cap enforced locally. Returns the created attachment object `{name, fileURL}` (Favro echoes the attachment, not the updated Card — verified live). Live success invalidates the search-cards cache. Pass `dry_run: true` to preview. |

The full tool index will land at v1.0 (Phase 8).

## Development

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
