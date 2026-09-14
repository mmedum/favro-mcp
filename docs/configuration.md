# Configuration

Everything is an environment variable, because MCP clients pass
`command`, `args` and `env` to a stdio server and nothing else. There are
no configuration files: the three credential fields can also come from
the OS keyring, which `favro-mcp auth login` writes.

| Variable | Default | Meaning |
|---|---|---|
| `FAVRO_USER_EMAIL` | keyring, else unset | The address you sign in to Favro with. It is the username half of HTTP Basic auth. |
| `FAVRO_API_TOKEN` | keyring, else unset | A Favro API token, created under My Profile → API tokens. The password half. |
| `FAVRO_ORGANIZATION_ID` | keyring, else unset | The organization every request is scoped to. This server is single-org and no tool takes another. |
| `FAVRO_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`, case-insensitive. Logs go to stderr. A value this server cannot read falls back to `info` and says so. |
| `FAVRO_ENABLE_DESTRUCTIVE` | `false` | Register the delete-style tools. See below — "off" means absent from `tools/list`, not guarded. Anything unreadable as a bool leaves them off. |
| `FAVRO_MCP_SKIP_VALIDATE` | unset | Any non-empty value skips the startup credential check. For protocol-only tests that never reach Favro. |

## Credential resolution

Environment first, then the OS keyring — macOS Keychain, Windows
Credential Manager, Linux Secret Service.

A source is skipped only when it holds *nothing*. With all three
variables unset the environment is "not configured" and the keyring is
tried; with one or two of them set the triple is incomplete, and that is
an error that stops resolution rather than a reason to fall through. So
exporting `FAVRO_USER_EMAIL` alone does not quietly run with the
keyring's token under a different address — it fails, naming the fields
that are missing. The same applies to a corrupt keyring entry: it is
reported, not skipped.

`favro-mcp auth which` prints which source won, and exits non-zero when
none did — though it says only "(no credentials configured)".
`favro-mcp doctor` is the one that explains: when the environment is
partly set it names the fields that are missing, and when nothing is
configured anywhere it names the three variables to set.

`favro-mcp auth login` writes the keyring entries and is the one-shot
setup for that path; re-run it to rotate the token.

## Commands

```
favro-mcp                     run as an MCP server over stdio
favro-mcp --dry-run           run the server with every write forced into dry-run
favro-mcp --version           print version and commit
favro-mcp --dump-schemas      print every tool schema as JSON and exit
favro-mcp auth login          store credentials in the OS keyring (token input masked)
favro-mcp auth status         show the active user and organization; the token is never printed
favro-mcp auth logout         delete the keyring entries
favro-mcp auth which          print the active credential source: env or keyring
favro-mcp doctor              check credentials, the organization binding and API reachability
favro-mcp doctor --show-ids   the same report with ids and addresses in full
```

`doctor` is the first thing to run when something does not work. It
reports the build and where its version came from, which source the
credentials resolved from, whether Favro accepts them, and whether
`FAVRO_ORGANIZATION_ID` names an organization the token can actually see
— the failure that otherwise surfaces as every tool returning
`[not_found]` with nothing saying why.

Its output is written to be pasted into a public issue: ids and addresses
are replaced with stable placeholders, so the report still shows that the
id you are bound to is or is not one of the ids the token can see.
`--show-ids` prints the real values for your own screen and says it is
unsafe to share. **Neither mode prints the token.**

`--dump-schemas` reports the whole surface regardless of
`FAVRO_ENABLE_DESTRUCTIVE`, because it is a record of every schema this
binary can serve; whether a tool is registered is a deployment decision
rather than a wire one.

## The destructive flag

There are fourteen delete-style tools. Unset, they are not in
`tools/list` at all. That is the
point: a client-side confirmation prompt is not a safety layer, because a
host in an auto-approve permission mode runs a tool annotated
`destructiveHint` without asking anyone, and the MCP spec says clients
treat tool annotations as untrusted. The only tool that cannot run
unattended is the one that was never registered.

Which tools those are is read from the annotation at registration, never
from a list of names, so a delete tool added later is gated by having
been written rather than by somebody remembering to add it.

`dry_run` is not a second guard for this. It is a per-call preview,
useful for checking what a write would send; it does not stop a host from
calling the tool without it.

## Startup behaviour

The server resolves credentials, then validates them against Favro with
one `GET /organizations`, then serves. **If either step fails it logs the
reason and exits non-zero** rather than starting and failing per call.
`FAVRO_MCP_SKIP_VALIDATE` skips the live half.

A host shows that as "failed to connect", which says less than it could —
the reason is on stderr, and `favro-mcp doctor` is the command that
explains it. See `docs/architecture.md` §17b for why this server exits
where the sibling servers start anyway.

## Rate limits

Favro publishes per-hour request budgets per organization. The client
tracks what Favro returns and `favro_rate_limit_status` reports it; a 429
comes back as a `[rate_limited]` error naming `retry_after_seconds`.
Pagination is never auto-aggregated — list tools surface `next_page` and
require an explicit follow-up — which is the main reason a session does
not burn a budget by accident.
