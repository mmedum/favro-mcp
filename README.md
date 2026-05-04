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

The full tool index will land at v1.0 (Phase 8).

## Development

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
