# Configuration

Everything is an environment variable, because MCP clients pass
`command`, `args` and `env` to a stdio server and nothing else. There are
no configuration files: the three credential fields can also come from
the OS keyring, which `favro-mcp auth login` writes.

Every variable is read without the spaces around its value, and one of
spaces alone is unset, as an empty one is. So `true` with a space before
it turns `FAVRO_ENABLE_DESTRUCTIVE` on, and a form field cleared to a
space means the default.

| Variable | Default | Meaning |
|---|---|---|
| `FAVRO_USER_EMAIL` | keyring, else unset | The address you sign in to Favro with. It is the username half of HTTP Basic auth. |
| `FAVRO_API_TOKEN` | keyring, else unset | A Favro API token, created under My Profile → API tokens. The password half. |
| `FAVRO_ORGANIZATION_ID` | keyring, else unset | The organization every request is scoped to. This server is single-org and no tool takes another. |
| `FAVRO_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`, case-insensitive. Logs go to stderr. A value this server cannot read falls back to `info` and says so. |
| `FAVRO_ENABLE_DESTRUCTIVE` | `false` | Register the destructive tools: the deletes, and `favro_widen_collection_sharing`. See below — "off" means absent from `tools/list`, not guarded. Anything unreadable as a bool leaves them off. |
| `FAVRO_UPLOAD_DIR` | unset | The one directory the upload tools read files from. Unset, and they are not registered. It must be an absolute path to a directory that exists; anything else is logged at startup and leaves uploads off. See below. |
| `FAVRO_REQUIRE_PROMPT` | removed | It refused the writes that asked the person when the client could not ask. The server asks nobody now, so the setting is gone: set to anything but `false`, the server refuses to start rather than quietly allow what it was set to stop, and `doctor` reports it. Unset it, and leave `FAVRO_ENABLE_DESTRUCTIVE` off for a client that runs with nobody watching. |
| `FAVRO_INTERACTION_HINT` | removed | Set to `false`, it took Claude Code's `requiresUserInteraction` mark off the writes the server asked about. The five marked tools now always carry the mark, so Claude Code prompts on every call of them and refuses them where it cannot prompt. Set to anything but `true`, it is ignored with a warning; unset it. |
| `FAVRO_MCP_SKIP_VALIDATE` | unset | Any value but spaces skips the startup credential check. For protocol-only tests that never reach Favro. |

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
`FAVRO_ENABLE_DESTRUCTIVE` and `FAVRO_UPLOAD_DIR`, because it is a record of every schema this
binary can serve; whether a tool is registered is a deployment decision
rather than a wire one.

## The destructive flag

There are fifteen destructive tools: the deletes, and
`favro_widen_collection_sharing`. Unset, they are not in
`tools/list` at all. That is the
point: a client-side confirmation prompt is not a safety layer, because a
host in an auto-approve permission mode runs a tool annotated
`destructiveHint` without asking anyone, and the MCP spec says clients
treat tool annotations as untrusted. The only tool that cannot run
unattended is the one that was never registered.

Which tools those are is read from the annotation at registration, never
from a list of names, so a delete tool added later is gated by having
been written rather than by somebody remembering to add it.

Registered, each one still refuses a call without `confirm: true`, unless
it is a dry run. That is a sign the caller meant the call, not approval:
the model sets it.

`dry_run` is not a second guard for this. It is a per-call preview,
useful for checking what a write would send; it does not stop a host from
calling the tool without it.

## The upload directory

`favro_upload_attachment` and `favro_upload_comment_attachment` send a
local file to Favro, so they exist only when `FAVRO_UPLOAD_DIR` names a
directory, and read inside it and nowhere else. `file_path` is relative
to it, or absolute within it. A `..` or a symbolic link that leaves it is
refused before anything is read. Use a directory that holds only what you
would attach to a card.

## Approving calls

The server asks the person nothing; the MCP client approves each call.
claude.ai and Codex prompt by `destructiveHint`, which the fifteen
destructive tools carry, and VS Code prompts on every tool without
`readOnlyHint`, which only the reads carry.

Five tools carry Claude Code's `requiresUserInteraction`, so Claude Code
prompts on every call of them whatever its rules say, and refuses them
where it cannot prompt: `favro_delete_tag`, `favro_delete_group`,
`favro_delete_widget`, `favro_delete_collection` and
`favro_widen_collection_sharing`. For the rest, list the calls you want
to approve under `permissions.ask`; the README has an example.

The ordinary collection, group and widget writes share only inside the
organization: members by userId, and groups whose members all are. An
email, `type: 'organization'`, a new admin or group administrator,
someone whose organization role is not `administrator` or `fullMember`,
a group that holds such a person or cannot be read, `owner_role:
'guests'` or `edit_role: 'guests'`, and `public_sharing: 'organization'`
or `'public'` are refused `[blocked]`. Opening a collection is
`favro_widen_collection_sharing`, which needs the destructive flag.

## Startup behavior

The server refuses to start while a removed setting is turned on
(`FAVRO_REQUIRE_PROMPT`). Then it resolves credentials, validates them
against Favro with one `GET /organizations`, and serves. **If any step
fails it logs the reason and exits non-zero** rather than starting and
failing per call.
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
