# favro-mcp

A Model Context Protocol (MCP) server for [Favro](https://favro.com), written in Go. Speaks MCP over stdio, exposes Favro's REST API as typed tools, and ships with workflow tools designed for natural-language use (search, name→ID resolution, surgical description edits) so an LLM can act on Favro without burning the rate-limit budget on lookup roundtrips.

> **Unofficial.** Not affiliated with or endorsed by Favro.

## Status

Pre-release. See [CHANGELOG.md](./CHANGELOG.md) for what's shipped.

Active phase: **Phase 1** — auth subsystem & stdio handshake.

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

The full tool index will land at v1.0 (Phase 8).

## Development

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
