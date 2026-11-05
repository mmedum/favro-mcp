# Configuration

Everything is an environment variable, because MCP clients pass
`command`, `args` and `env` to a stdio server and nothing else (D-57).
There are no configuration files: the three credential fields can also
come from the OS keyring, which `favro-mcp auth login` writes.

Every variable is read without the spaces around its value, and one of
spaces alone is unset, as an empty one is. So `true` with a space before
it turns `FAVRO_ENABLE_DESTRUCTIVE` on, and a form field cleared to a
space means the default.

| Variable | Default | Meaning |
|---|---|---|
| `FAVRO_USER_EMAIL` | keyring, else unset | The address you sign in to Favro with. It is the username half of HTTP Basic auth (D-54). |
| `FAVRO_API_TOKEN` | keyring, else unset | A Favro API token, created under My Profile → API tokens. The password half. |
| `FAVRO_ORGANIZATION_ID` | keyring, else unset | The organization every request is scoped to. The server binds one organization, and no tool takes another (D-02). |
| `FAVRO_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`, case-insensitive. Logs go to stderr. A value this server cannot read falls back to `info` and says so. |
| `FAVRO_ENABLE_DESTRUCTIVE` | `false` | Register the destructive tools. "Off" means absent from `tools/list`, not guarded; see below. Anything unreadable as a bool leaves them off. |
| `FAVRO_UPLOAD_DIR` | unset | The one directory the upload tools read files from. Unset, and they are not registered. It must be an absolute path to a directory that exists; see below. |
| `FAVRO_REQUIRE_PROMPT` | removed | Set to anything but `false`, the server refuses to start, and `doctor` reports it (D-32). Unset it. |
| `FAVRO_INTERACTION_HINT` | removed | Ignored. Set to anything but `true`, the server warns that it is (D-32). Unset it. |
| `FAVRO_MCP_SKIP_VALIDATE` | `false` | Set to `true` to skip the startup credential check, for protocol-only tests that never reach Favro. `false` and `0` keep the check; anything else is logged at startup and keeps it too. |

[Troubleshooting](troubleshooting.md) gives the message each setting
logs when the server cannot use it, and what to do.

## Credential resolution

Environment first, then the OS keyring: macOS Keychain, Windows
Credential Manager or Linux Secret Service (D-55).

A source is skipped only when it holds nothing. With all three
variables unset, the environment is not configured and the keyring is
tried. With one or two of them set, the triple is incomplete, and that
is an error that stops resolution rather than a reason to fall through.
So exporting `FAVRO_USER_EMAIL` alone never runs the keyring's token
under a different address. A damaged keyring entry is reported, not
skipped.

`favro-mcp auth login` writes the keyring entries and is the one-time
setup for that path; run it again to rotate the token.

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

`doctor` is the first thing to run when something does not work;
[troubleshooting.md](troubleshooting.md) says what it reports (D-59).

When it serves, the binary takes only `--dry-run` and `--dump-schemas`;
every setting is a variable. `--dump-schemas` reports the whole surface
regardless of `FAVRO_ENABLE_DESTRUCTIVE` and `FAVRO_UPLOAD_DIR`, because
it is a record of every schema this binary can serve (D-66). Whether a
tool is registered is a deployment decision rather than a wire one.

## The destructive flag

There are fifteen destructive tools: the deletes,
`favro_remove_attachment`, `favro_replace_dependencies` and
`favro_widen_collection_sharing`. Unset, they are not in `tools/list` at
all (D-29). A client's confirmation prompt is not a safety layer: a
client in an auto-approve mode runs a tool annotated `destructiveHint`
without asking anyone, and the MCP specification says clients treat tool
annotations as untrusted. A tool that was never registered cannot run
unattended.

Which tools those are is read at registration from whether a tool takes
`confirm`, never from a list of names. A delete tool added later is
gated by having been written that way, not by someone remembering to add
it.

Registered, each one still refuses a call without `confirm: true` as
`[blocked]`, unless it is a dry run (D-30). That is a sign the caller
meant the call, not approval: the model sets it.

`dry_run` is not a second guard for this. It is a per-call preview of
what a write would send, and it does not stop a client from calling the
tool without it (D-28).

## The upload directory

`favro_upload_attachment` and `favro_upload_comment_attachment` send a
local file to Favro, so they exist only when `FAVRO_UPLOAD_DIR` names a
directory, and read inside it and nowhere else (D-38). `file_path` is
relative to it, or absolute within it. A `..` or a symbolic link that
leaves it is refused before anything is read. Use a directory that holds
only what you would attach to a card.

## Approving calls

The server asks the person nothing; the MCP client approves each call
(D-31). The README's [Approving calls](../README.md#approving-calls)
says what each client asks before, and gives Claude Code's
`permissions.ask` rules. [`TOOLS.md`](TOOLS.md) says which writes share
only inside the organization (D-35).

## Startup behavior

The server refuses to start while a removed setting is turned on
(`FAVRO_REQUIRE_PROMPT`). Then it resolves credentials, validates them
against Favro with one `GET /organizations`, and serves (D-32, D-55).
**If any step fails, it logs the reason to stderr and exits non-zero**,
rather than starting and failing every call (D-58).
`FAVRO_MCP_SKIP_VALIDATE` skips the call to Favro.
