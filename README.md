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

The full tool index will land at v1.0 (Phase 8).

## Development

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
