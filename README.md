# favro-mcp

[![CI](https://github.com/mmedum/favro-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/mmedum/favro-mcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/mmedum/favro-mcp)](https://github.com/mmedum/favro-mcp/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/mmedum/favro-mcp.svg)](https://pkg.go.dev/github.com/mmedum/favro-mcp)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

A [Model Context Protocol](https://modelcontextprotocol.io) server for [Favro](https://favro.com), written in Go.

It speaks MCP over stdio and exposes Favro's REST API as 85 typed tools. Beyond
plain CRUD, it ships workflow tools built for natural-language use — search,
name→ID resolution, surgical description edits — so an LLM can act on Favro
without spending its rate-limit budget on lookup round-trips. Every mutating
tool supports `dry_run`.

> **Unofficial.** Not affiliated with or endorsed by Favro.

**Requirements:** Go 1.27 to build from source; a Favro account with an API
token. See [CHANGELOG.md](./CHANGELOG.md) for release history.

## Installation

### Claude Desktop bundle

Each tagged release publishes a `favro-mcp_<version>.mcpb` on the [GitHub Releases page](https://github.com/mmedum/favro-mcp/releases). Opening it installs the server into Claude Desktop and prompts for the three values it needs — your Favro email, an API token, and the organization id — so there is no config file to hand-edit.

The bundle carries a macOS universal binary, a Windows amd64 binary, and both Linux architectures behind a launcher that picks at startup. It is listed in `checksums.txt` and covered by the release signature; see [Verifying a download](#verifying-a-download).

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

Requires Go 1.27, the version `go.mod` declares. Under the default
`GOTOOLCHAIN=auto` an older toolchain downloads it on demand; under
`GOTOOLCHAIN=local` the build fails instead of downgrading.

## Verifying a download

Every release carries `checksums.txt`, an SBOM per archive, a keyless [cosign](https://docs.sigstore.dev/) signature over the checksum file, and build provenance attested by GitHub. The `.mcpb` bundle is in `checksums.txt` with the archives, so one signature covers all of it.

```bash
# 1. The file is what the release says it is.
sha256sum -c checksums.txt --ignore-missing

# 2. The checksum file came from this repository's release workflow.
cosign verify-blob checksums.txt \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp '^https://github\.com/mmedum/favro-mcp/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# 3. Which workflow, at which commit, built a given artifact.
gh attestation verify favro-mcp_<version>_<os>_<arch>.tar.gz --repo mmedum/favro-mcp
```

Builds are reproducible: `-trimpath` plus the commit's own timestamp, so rebuilding a tag gives byte-identical archives.

## Authentication

The server uses Favro's HTTP Basic Auth (user email + API token), scoped to one Favro organization at startup. Tools never accept `organization_id` — pass it once via env var or keyring and forget about it.

API tokens are user-scoped — for team installs, generate the token from a dedicated service-style Favro user with the minimum permissions you need, not from a personal account.

### Credential resolution

Two sources, checked in order; the first one that produces a complete `(email, token, organization_id)` triple wins:

1. **Environment variables** — `FAVRO_USER_EMAIL`, `FAVRO_API_TOKEN`, `FAVRO_ORGANIZATION_ID`.
2. **OS keyring** — populated once via `favro-mcp auth login` (cross-platform: macOS Keychain, Windows Credential Manager, Linux Secret Service).

Three more variables change how the server runs rather than who it runs as:

| Variable | Effect |
| --- | --- |
| `FAVRO_LOG_LEVEL` | `debug` / `info` (default) / `warn` / `error`. Logs go to stderr; stdout carries only the MCP protocol stream. |
| `FAVRO_MCP_SKIP_VALIDATE` | When set to anything non-empty, skips the startup call that checks the credentials against Favro. For offline testing; the server then fails on the first real tool call instead of at startup. |
| `FAVRO_ENABLE_DESTRUCTIVE` | Set to `true` to register the delete-style tools. Off by default, and "off" means they are absent from `tools/list` rather than guarded — a host in an auto-approve mode runs a tool without prompting, so not registering it is the only guarantee. Anything the value cannot be read as `true` leaves them off. |

### `auth` subcommands

```
favro-mcp auth login       # interactive: masked token input, writes to keyring
favro-mcp auth status      # show user/org (token never printed)
favro-mcp auth logout      # delete keyring entries
favro-mcp auth which       # print active credential source: env or keyring
favro-mcp doctor           # check credentials, org binding and API reachability
favro-mcp doctor --show-ids  # the same, unredacted, for your own screen
favro-mcp --version        # print version + commit
favro-mcp --dry-run        # process-wide override: forces all writes into dry-run
```

`favro-mcp auth login` is the one-shot setup for the keyring path. Re-run it to rotate the token.

`favro-mcp doctor` is the first thing to run when something does not work. It reports the build, which source the credentials came from, whether Favro accepts them, and whether the organization id names one the token can actually see — the failure that otherwise shows up as every tool returning `not_found`. Its output replaces ids and addresses with stable placeholders so it can be pasted into an issue; `--show-ids` prints the real values for your own screen, and says so.

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

85 tools covering every Favro REST resource, plus workflow tools built for
natural-language use. See **[docs/TOOLS.md](./docs/TOOLS.md)** for the full
reference.

The ones worth knowing first:

| Tool | What it does |
| --- | --- |
| `favro_search_cards` | Full-text search over card names and descriptions. Scope to a widget or collection. |
| `favro_get_card_full` | One card with every id resolved to a name — saves 4–7 follow-up calls. |
| `favro_resolve_*` | Turn a name into an id for tags, users, collections, widgets, columns, custom fields, groups. |
| `favro_append_card_description` | Surgical description edits that return a unified diff. |
| `favro_ping` | Local diagnostic: version, bound org, credential source. Never contacts Favro. |

## Troubleshooting

Run `favro-mcp doctor` first — it answers most of the rows below directly, and its output is safe to paste into an issue.

| Symptom | Diagnosis & fix |
| --- | --- |
| Every tool returns `[not_found]`, but the credentials are accepted | `FAVRO_ORGANIZATION_ID` names an organization this token cannot see. `favro-mcp doctor` reports this as a failed organization binding; `favro-mcp doctor --show-ids` lists the ids the token *can* see. |
| `authentication failed — check FAVRO_USER_EMAIL and FAVRO_API_TOKEN env vars` on startup | Either no credentials configured, or the token was revoked / rotated. Run `favro-mcp auth which` to confirm which source the server is reading, then re-run `favro-mcp auth login` (keyring path) or update the env vars. |
| `FAVRO_ORGANIZATION_ID is required` on startup | The server is single-org by design — it needs to know which org to scope every request to before it can start. Set `FAVRO_ORGANIZATION_ID` (or include it in `auth login`) and restart. |
| HTTP 429 / `rate limit exceeded` | Hit the per-org Favro rate limit. The client retries once honoring `Retry-After` (capped at 30s) and then surfaces a typed error with `retry_after_seconds`. Use `favro_rate_limit_status` to inspect the most recent `X-RateLimit-*` headers without spending another call. |
| `next_page` keeps coming back non-null | List tools never auto-aggregate. Each follow-up call must include `page` plus the same `request_id` (Favro routes paginated reads via `X-Favro-Backend-Identifier`); some tools also require resending the original filters. |
| `decodeJSONLenient` errors with `content-type: text/html, body-prefix: "<p>It looks like…"` | You hit a Favro endpoint that doesn't exist on the documented surface — Favro returns the SPA fallback page instead of a 404. The error includes status + content-type + body-prefix so you can diagnose in one round-trip. |
| A custom-field write returns 200 but the value doesn't change | Two known causes. First, the field may not be enabled on that widget — org-global custom fields still need per-widget enablement in the Favro UI, and writes to a field the widget doesn't have are accepted and ignored. Second, Favro ignores unknown body fields silently, so a wrong per-type shape looks exactly like success; `favro_set_card_custom_field` sends the shapes Favro's REST docs specify, but they have not all been confirmed against a live tenant. Re-read the card with `favro_get_card_full` to confirm. |
| Tool descriptions have stale data after a successful write | All cache invalidations are per-org-scoped and per-resource-type; if a write succeeds but the next read still shows old data, pass `force_refresh: true` to the list/resolve tool. |
| Linux launcher doesn't run from the plugin | The launcher is a bash script. If your shell can't exec it, point your `.mcp.json` directly at `${CLAUDE_PLUGIN_ROOT}/bin/linux-amd64/favro-mcp` (or `linux-arm64`) instead. |
| Windows | The bundled `bin/favro-mcp.cmd` shim exec's `bin\windows-amd64\favro-mcp.exe`. Windows hosts resolve `${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp` to the `.cmd` automatically via PATHEXT, so the standard `.mcp.json` config in [MCP host configuration](#mcp-host-configuration) works as-is. If your host doesn't honor PATHEXT, point `command` at `${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp.cmd` (or directly at `bin/windows-amd64/favro-mcp.exe`). |

## Documentation

| | |
|---|---|
| [docs/TOOLS.md](./docs/TOOLS.md) | Every tool, its inputs and what it returns. |
| [docs/configuration.md](./docs/configuration.md) | Every setting, the commands, and what happens at startup. |
| [docs/security.md](./docs/security.md) | Trust boundaries, what reaches a log, and what limits what. |
| [docs/development.md](./docs/development.md) | The gates, the test conventions, and how a release is cut. |
| [docs/architecture.md](./docs/architecture.md) | The design, the decided constraints, the evidence log and the phase plan. |
| [SECURITY.md](./SECURITY.md) | Reporting a vulnerability. |

## Development

See [docs/development.md](./docs/development.md) for the gates and the
test conventions, and [CONTRIBUTING.md](./CONTRIBUTING.md) for how to
get set up and open a pull request.

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
