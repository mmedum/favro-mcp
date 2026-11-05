# favro-mcp

[![CI](https://github.com/mmedum/favro-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/mmedum/favro-mcp/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/mmedum/favro-mcp)](https://github.com/mmedum/favro-mcp/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/mmedum/favro-mcp/v4.svg)](https://pkg.go.dev/github.com/mmedum/favro-mcp/v4)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

Favro as MCP tools. Find, read and change cards, boards and collections from Claude or any MCP client.

A single Go binary that speaks [Model Context Protocol](https://modelcontextprotocol.io)
over stdio. It runs as a subprocess of your client, on your own machine,
against one [Favro](https://favro.com) organization, with your own address
and API token. There is no server to host and no shared deployment.

It is for a person who works in Favro and wants an assistant to do that
work with them: find a card by what it says, read it with every id turned
into a name, move it, edit its description in place, tag it and comment on
it. Beyond Favro's REST API, it resolves names to ids, searches card text
and reads a whole card in one tool call, so a model spends its calls on the work
rather than on lookups.

> **Unofficial.** Not affiliated with or endorsed by Favro.

## Goals and non-goals

Goals:

- **Nothing is lost or exposed by a call nobody meant.** The tools that
  delete or open a collection wider exist only when you turn them on, and
  each call must say `confirm: true`. The ordinary writes share only inside
  the organization, the upload tools read one directory, and a mistyped tag
  name never becomes a tag.
- **Results say what Favro did.** A write Favro may accept and drop is read
  back, and the result says what it found. A write whose answer was lost is
  reported as such and never sent twice.
- **The rate limit is spent on work.** Names resolve through a cache, one
  call is one page, and a full card is one tool call rather than one per id.
- **Your organization stays yours.** Every call runs with your token, only
  to Favro, with no telemetry. Logs never carry a payload, an id or the
  organization.

Non-goals:

- **More than one organization per server.** It binds one at startup, and
  no tool takes another.
- **Administering Favro.** SCIM provisioning and creating or changing an
  organization are out, and webhooks are listed and deleted, never
  registered.
- **Fetching every page for you.** Each list call returns one page and the
  token for the next.
- **Limiting what the token can do.** A token carries its user's
  permissions; choose the user.
- **Asking you before a call.** Your client approves each call; the server
  asks nothing.

## Installation

### Claude Desktop bundle

Each tagged release publishes a `favro-mcp_<version>.mcpb` on the [GitHub Releases page](https://github.com/mmedum/favro-mcp/releases). Opening it installs the server into Claude Desktop and prompts for what it needs: your Favro address, an API token and the organization id. There is no config file to edit by hand.

It also asks for the settings a config file would carry. The destructive tools (`enable_destructive`) stay off unless you turn them on, and so do uploads until you pick an upload folder (`upload_dir`). This server asks nobody before a call. Claude Desktop approves each tool, and a tool you always allow then runs unasked, so leave `enable_destructive` off unless you need its tools.

The bundle carries a macOS universal binary, a Windows amd64 binary, and both Linux architectures behind a launcher that picks at startup. It is listed in `checksums.txt` and covered by the release signature; see [Verifying a download](#verifying-a-download).

### Cowork plugin (recommended)

Each tagged release publishes a single multi-arch `favro-mcp.plugin` zip on the [GitHub Releases page](https://github.com/mmedum/favro-mcp/releases). The bundle contains binaries for darwin-amd64, darwin-arm64, linux-amd64, linux-arm64 and windows-amd64. On macOS and Linux a launcher shim picks the right one at runtime, and on Windows a `.cmd` shim runs the windows-amd64 binary, which Windows on arm64 (windows-arm64) runs under emulation.

1. Download `favro-mcp.plugin` from the release.
2. Install it into your Cowork profile (drag-and-drop, or follow Cowork's plugin install flow).
3. Authenticate (see [Authentication](#authentication) below).
4. Restart Cowork, and the `favro` MCP server appears with its tools registered.

The bundled `.mcp.json` points at `${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp` so the install is relocatable; no host paths to edit.

### Standalone binary

For non-Cowork hosts (Claude Code via raw `.mcp.json`, MCP Inspector, custom integrations):

1. Download the matching `favro-mcp_<version>_<os>_<arch>.tar.gz` (or `.zip` on Windows) from the release.
2. Extract and place the `favro-mcp` binary somewhere on `$PATH` (or note its absolute path).
3. Authenticate.
4. Point your MCP host's config at the binary — see [MCP host configuration](#mcp-host-configuration) below.

### From source

```bash
go install github.com/mmedum/favro-mcp/v4/cmd/favro-mcp@latest
```

Or build a clone:

```bash
git clone https://github.com/mmedum/favro-mcp
cd favro-mcp
make build       # → bin/favro-mcp
```

Either needs the Go version `go.mod` declares. Under the default
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

These variables change how the server runs rather than who it runs as:

| Variable | Effect |
| --- | --- |
| `FAVRO_LOG_LEVEL` | `debug` / `info` (default) / `warn` / `error`. Logs go to stderr; stdout carries only the MCP protocol stream. |
| `FAVRO_MCP_SKIP_VALIDATE` | Set to `true` to skip the startup call that checks the credentials against Favro. For offline testing; the server then fails on the first real tool call instead of at startup. `false`, `0` and anything it cannot read as true leave the check on. |
| `FAVRO_ENABLE_DESTRUCTIVE` | Set to `true` to register the destructive tools: the deletes, `favro_remove_attachment`, `favro_replace_dependencies` and `favro_widen_collection_sharing`. Off by default, and "off" means they are absent from `tools/list` rather than guarded — a host in an auto-approve mode runs a tool without prompting, so not registering it is the only guarantee. Anything the value cannot be read as `true` leaves them off. Each call must also pass `confirm: true`. |
| `FAVRO_UPLOAD_DIR` | The one directory the upload tools read files from, as an absolute path. Unset, and they are not registered. Nothing outside it is read, and a link out of it is refused. |

Every variable, the credentials included, is read without the spaces around its value, and spaces alone count as unset.

`FAVRO_REQUIRE_PROMPT` and `FAVRO_INTERACTION_HINT` are removed; unset
them. [docs/configuration.md](./docs/configuration.md) has every setting.

### Approving calls

Your MCP client approves each call; the server does not ask you itself.
claude.ai and Codex ask before any write that changes or removes
something, which says `destructiveHint: true`. A write that only
adds, such as a new card, comment or attachment, says false. VS Code asks
before every tool that writes.

In Claude Code, these tools ask on every call whatever your rules say:
`favro_delete_tag`, `favro_delete_group`, `favro_delete_widget` and
`favro_delete_collection`, whose loss reaches past the one thing deleted,
and `favro_widen_collection_sharing`, which opens a collection to the
whole organization or to anyone with its link. Where no prompt can be
shown, as in `claude -p`, Claude Code refuses them. For the rest, list
the calls you want to approve one by one under `permissions.ask` in your
settings, with the name you gave the server in place of `favro`. For
example, the writes that share, publish, send a file or delete:

```json
{
  "permissions": {
    "ask": [
      "mcp__favro__favro_create_collection", "mcp__favro__favro_update_collection",
      "mcp__favro__favro_widen_collection_sharing",
      "mcp__favro__favro_create_group", "mcp__favro__favro_update_group",
      "mcp__favro__favro_upload_attachment", "mcp__favro__favro_upload_comment_attachment",
      "mcp__favro__favro_delete_tag", "mcp__favro__favro_delete_group",
      "mcp__favro__favro_delete_widget", "mcp__favro__favro_delete_collection",
      "mcp__favro__favro_delete_column", "mcp__favro__favro_delete_card",
      "mcp__favro__favro_delete_comment", "mcp__favro__favro_delete_task",
      "mcp__favro__favro_delete_tasklist", "mcp__favro__favro_delete_webhook",
      "mcp__favro__favro_remove_attachment", "mcp__favro__favro_replace_dependencies",
      "mcp__favro__favro_delete_dependency", "mcp__favro__favro_delete_all_dependencies"
    ]
  }
}
```

What holds whatever the client does:

- **Destructive tools only on request.** They exist only with
  `FAVRO_ENABLE_DESTRUCTIVE=true`, and each call must pass
  `confirm: true`; `dry_run: true` previews without it.
- **Sharing stays inside the organization.** The ordinary collection,
  group and widget writes share only with members of the organization,
  named by userId, and with groups of members. An invitation by email,
  a new admin, someone outside the organization, a group that holds
  someone outside it, guests as a widget's owners or editors, and a
  public or organization-wide collection are refused.
  `favro_widen_collection_sharing` opens a collection, and takes the
  collection's name as well as its id, so the prompt shows which one.
- **No wider delete by omission.** `favro_delete_widget` takes
  `collection_id` or `everywhere: true`, and refuses a call with neither.

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

`favro-mcp doctor` is the first thing to run when something does not work; [docs/troubleshooting.md](./docs/troubleshooting.md) says what it reports. Its output is safe to paste into an issue; `--show-ids` output is not.

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

Favro limits API calls on two levels, as its [reference](https://favro.com/developer/#rate-limiting-and-throttling) describes. User-level routes (get a user, list organizations, create an organization) allow 50 calls an hour. Every other route is an organization route, and its limit depends on the organization's plan: Trial 100, Lite 0, Standard 1000 and Enterprise 10000 calls an hour. The reference's table labels these numbers per user, while its text ties them to the organization's plan. Favro delays a call to let its tokens replenish, and fails it with 429 once the delay would pass 10 seconds. A misbehaving agent can exhaust that quota, and if Favro counts it per organization, it does so for everyone in your org. The server caches resolution lookups (tags, users, widgets, columns) in-memory and supports `force_refresh` on every list/resolve tool to bust the cache when needed. List tools never auto-aggregate pages — each `next_page` requires an explicit follow-up call so the LLM can stop early.

## Dry-run

Every mutating tool accepts `dry_run: true` and returns the request it would send (method, URL, body) and the change it predicts, without sending a write. A tool may still read to build the preview. Read tools never accept `dry_run`.

For sandboxed or pre-autonomy testing, `--dry-run` on the binary forces dry-run mode process-wide regardless of per-call input.

## Tools

Tools for every Favro resource the server covers, plus workflow tools built
for natural-language use. **[docs/TOOLS.md](./docs/TOOLS.md)** is the full
reference, and says what each `[class]` of error means.

The ones worth knowing first:

| Tool | What it does |
| --- | --- |
| `favro_search_cards` | Full-text search over card names and descriptions. Scope to a widget or collection. |
| `favro_get_card_full` | One card with every id resolved to a name, in one tool call rather than one per id. |
| `favro_resolve_*` | Turn a name into an id for tags, users, collections, widgets, columns, custom fields, groups. |
| `favro_append_card_description` | Surgical description edits that return a unified diff. |
| `favro_ping` | Local diagnostic: version, bound org, credential source. Never contacts Favro. |

## Troubleshooting

Run `favro-mcp doctor` first. [docs/troubleshooting.md](./docs/troubleshooting.md)
lists each error the server logs or returns, what causes it and what to do,
then rotating and revoking the token.

## Documentation

- [docs/TOOLS.md](./docs/TOOLS.md): every tool, its inputs and what it
  returns, and the error classes.
- [docs/configuration.md](./docs/configuration.md): every setting, the
  commands, and what happens at startup.
- [docs/troubleshooting.md](./docs/troubleshooting.md): symptoms and their
  fixes, then rotating and revoking the token.
- [docs/security.md](./docs/security.md): the threat model.
- [docs/architecture.md](./docs/architecture.md): how it works, and the
  design decisions a contributor should not undo.
- [docs/release.md](./docs/release.md): how a release is cut and checked.
- [CONTRIBUTING.md](./CONTRIBUTING.md): building, the gates, the live run
  and the pull request flow.
- [CHANGELOG.md](./CHANGELOG.md): what each release changed.

## Contributing

Questions and bugs go in
[issues](https://github.com/mmedum/favro-mcp/issues); pull requests are
welcome. [CONTRIBUTING.md](./CONTRIBUTING.md) covers building, the gates and
the pull request flow, and `make check` is what has to pass.

## Security

[SECURITY.md](./SECURITY.md) says how to report a vulnerability. Please do
not open a public issue for one.

## License

Apache 2.0 — see [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
