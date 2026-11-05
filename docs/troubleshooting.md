# Troubleshooting

What to do when something does not work, then the maintenance tasks.
Setting up in the first place is the README's
[Authentication](../README.md#authentication). Start with these two
commands:

```bash
favro-mcp doctor
favro-mcp auth which
```

`doctor` reports the build and where its version came from, which source
the credentials resolved from, whether Favro accepts them, whether
`FAVRO_ORGANIZATION_ID` names an organization the token can see, and the
settings (D-59). A failed check prints `FAIL`, its name and the reason,
and `doctor` exits non-zero. Its output replaces ids and addresses with
stable placeholders, so it can be pasted into a public issue. `--show-ids`
prints the real values for your own screen, and says it is unsafe to
share. Neither mode prints the token (D-47, D-48).

`auth which` prints the source that won, `env` or `keyring`. When none
did, it prints `(no credentials configured)` and exits non-zero.

Run both with the environment your MCP client gives the server, since
each checks what it is given.

## The client says the server failed to connect

**Cause:** the server exits non-zero at startup when it cannot resolve
credentials, when Favro rejects them, or when a removed setting is still
turned on (D-58). A client shows that as a failed connection. The reason
is on stderr, in the client's log for the server.

**Fix:** run `favro-mcp doctor`, then find the logged line below.
`FAVRO_MCP_SKIP_VALIDATE=true` skips only the call to Favro, for tests
that never reach it; the server then fails on its first real tool call
instead.

## `no Favro credentials found in environment or keyring`

The server logs it as `could not resolve Favro credentials`, with the
hint ``set FAVRO_USER_EMAIL / FAVRO_API_TOKEN / FAVRO_ORGANIZATION_ID, or
run `favro-mcp auth login` ``.

**Cause:** none of the three variables is set, and the keyring holds no
entry.

**Fix:** run `favro-mcp auth login`, or set all three variables in the
server's environment.

## `missing credential field(s): …`

**Cause:** one or two of `FAVRO_USER_EMAIL`, `FAVRO_API_TOKEN` and
`FAVRO_ORGANIZATION_ID` are set, not all three. The message names them
as `email`, `API token` and `organization id`. A partial set is an error
rather than a reason to fall back to the keyring, so the server never
runs the keyring's token under another address (D-55).

**Fix:** set the missing ones, or unset all three to use the keyring.
`favro-mcp doctor` marks each one that is not set.

## `invalid credential field(s) — contains CR/LF: …`

**Cause:** a value carries a line break, often from a pasted token.

**Fix:** set it again without the line break.

## `parse keyring payload: …`

**Cause:** the keyring entry is damaged. It is reported rather than
skipped, so the server does not fall back to something else (D-55).

**Fix:** run `favro-mcp auth logout`, then `favro-mcp auth login`.

## `authentication failed — Favro rejected the email and API token`

The server logs it as `Favro credentials rejected at startup`, with the
hint ``check FAVRO_USER_EMAIL and FAVRO_API_TOKEN, or run `favro-mcp auth
login` again``. A tool call Favro rejects the same way returns
`[auth] Favro authentication failed (HTTP 401) — check FAVRO_USER_EMAIL
and FAVRO_API_TOKEN`.

**Cause:** Favro answered 401 or 403. The token was deleted or replaced,
or the address is not the one the token belongs to.

**Fix:** `favro-mcp auth which` says which source the server reads. Run
`favro-mcp auth login` again for the keyring, or update the variables.

## `organization binding … is not among the … this token can see`

`doctor` prints this as a failed check. The server itself starts, and
its tools answer `[not_found]` with nothing saying why.

**Cause:** `FAVRO_ORGANIZATION_ID` names an organization the token
cannot see.

**Fix:** `favro-mcp doctor --show-ids` lists the ids the token can see.
Set `FAVRO_ORGANIZATION_ID` to the right one, or run
`favro-mcp auth login` again.

## `FAVRO_REQUIRE_PROMPT was removed: …`

The server logs it and refuses to start, and `doctor` reports it.

**Cause:** the setting is set to something other than `false`. It made
the server ask the person before some writes. The server asks nobody,
and the client's own approval decides each call (D-31, D-32). A removed
setting that turned a guard on is refused rather than ignored, so a
deployment does not quietly lose what it was set to stop.

**Fix:** unset it. Leave `FAVRO_ENABLE_DESTRUCTIVE` off for a client
that runs with nobody watching, and list the writes you want to approve
under your client's ask rules, as the README's
[Approving calls](../README.md#approving-calls) shows.

## `FAVRO_INTERACTION_HINT was removed and is ignored: …`

The server logs it as a warning and starts.

**Cause:** the setting is set to something other than `true`. It took
Claude Code's `requiresUserInteraction` mark off the widest destructive
tools. Those tools always carry it, so Claude Code prompts on every call
of them and refuses them where it cannot prompt, as in `claude -p`
(D-34).

**Fix:** unset it.

## A tool you expect is missing

**Cause:** the destructive tools are registered only with
`FAVRO_ENABLE_DESTRUCTIVE=true` (D-29). The upload tools are registered
only when `FAVRO_UPLOAD_DIR` is an absolute path to a directory that
exists (D-38). A value the server cannot use leaves them off, and the
server logs why at startup:

- `ignoring unreadable FAVRO_ENABLE_DESTRUCTIVE "…" — delete-style tools stay unregistered; set it to true or false`
- `ignoring FAVRO_UPLOAD_DIR: "…" is not an absolute path — the upload tools stay unregistered`,
  or `is not a directory`, or `cannot be read`

**Fix:** set the switch to `true`, or the directory to an absolute path
that exists, and restart the server. `doctor`'s settings section shows
the switch and every value the server could not read.

## `[rate_limited] Favro rate limit exceeded (HTTP 429)`

When Favro sent a wait, the message ends `(retry_after_seconds=N)`.

**Cause:** the hour's calls ran out. The README's
[Rate limits](../README.md#rate-limits) gives the limits. The client
retries a 429 once when it carries a `Retry-After` of 30 seconds or less.
Favro's reference documents no `Retry-After`, so a 429 without one is
returned at once (D-41).

**Impact:** if Favro counts the limit per organization, an agent that
spends it stops everyone in the organization until the hour refills.

**Workaround:** wait, then retry. `favro_rate_limit_status` reports the
last `X-RateLimit-*` headers without spending a call (D-42).

**Resolution:** spend fewer calls. Resolve a name once and pass the id,
page only as far as you need, and prefer `favro_get_card_full` to
several reads of one card.

## `[ambiguous_outcome] Favro … may have been applied: …`

The message ends `Do not repeat it; read the resource first to see
whether it took effect`.

**Cause:** a write's answer never came back. Favro answered with a 5xx,
or the connection was lost after the request went out. Favro does not
deduplicate writes, so the client sends a write once (D-41).

**Impact:** repeating the call can make a second card, comment or
attachment.

**Fix:** read the resource to see whether the write landed, and repeat
it only if it did not.

## `[unavailable] Favro accepted the write and did not perform it: …`

**Cause:** Favro answered a card move with 200, and the card's column
came back unchanged (D-22). The same call does the same thing again.

**Fix:** do not retry. Report it with the tool's name and the whole
error, as [Reporting a problem](#reporting-a-problem) says.

## `the "…" field is not on the card after the write`

`favro_set_card_custom_field` reports this in `notes` on a successful
result.

**Cause:** Favro answers 200 for a custom-field write it discarded. It
does that when the card's board has not enabled the field, which a field
defined for the whole organization still needs (D-24).

**Fix:** enable the field on the board in Favro, then write again. If
the write cleared the field, its absence is expected.

## A read after a write shows the old value

**Cause:** names and search results are cached in the server. A write
through this server invalidates the caches it changed (D-09), but a
change made in Favro's app or by someone else is not seen until the
entry expires (D-08).

**Fix:** pass `force_refresh: true` to the list, resolve or search tool.

## `[invalid] favro: decode response from … (status=…, content-type="text/html…", body-prefix=…)`

**Cause:** Favro answered with a web page rather than JSON. It answers a
path it does not serve with its web app's page rather than a 404, so
the server called a path Favro does not have.

**Fix:** report it. The error carries the status, the content type and
the start of the body; check that start for anything from your
organization before you paste it.

## The plugin's launcher does not start on Linux

**Cause:** the launcher the plugin runs is a bash script.

**Fix:** point `command` in your `.mcp.json` at
`${CLAUDE_PLUGIN_ROOT}/bin/linux-amd64/favro-mcp`, or `linux-arm64`,
instead.

## The plugin does not start on Windows

**Cause:** the plugin runs `bin\windows-amd64\favro-mcp.exe` through a
`.cmd` shim. A Windows client finds the shim for
`${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp` through `PATHEXT`, and one that
ignores `PATHEXT` does not.

**Fix:** point `command` at `${CLAUDE_PLUGIN_ROOT}/bin/favro-mcp.cmd`,
or at the `.exe` itself.

## Tasks

### Rotating the token

Create a new token in Favro under My Profile → API tokens, then store
it:

```bash
favro-mcp auth login
```

This replaces the keyring entries. With the variables, update
`FAVRO_API_TOKEN` instead. Then delete the old token in Favro.

### Revoking access

```bash
favro-mcp auth logout
```

This deletes the keyring entries. It cannot revoke the token at Favro,
so delete the token there too, under My Profile → API tokens. It does
not touch `FAVRO_API_TOKEN` in an environment.

### Suspected exposure

If a token may have leaked, such as in a shell history, a log or a
shared config:

1. Delete the token in Favro, under My Profile → API tokens.
2. Create a new one, and run `favro-mcp auth login` on each machine that
   should keep working.
3. Run `favro-mcp auth logout` on any machine that should not.

**Impact:** until step 1, anyone holding the token acts in Favro as the
user it belongs to, in every organization that user can reach.

### Moving to another machine

Install the binary there and run `favro-mcp auth login`. A keyring entry
does not move with your files.

### Reporting a problem

Open an [issue](https://github.com/mmedum/favro-mcp/issues) with:

- the output of `favro-mcp doctor`, without `--show-ids`, and of
  `favro-mcp --version`;
- how you installed it and which client runs it;
- the tool you called, what you expected, and the `[class] message` you
  got;
- a debug log from `FAVRO_LOG_LEVEL=debug` if it helps. It carries the
  method, the path's shape and the query's parameter names, never a
  payload or an id (D-46).

Never paste a tool result, card text, an id, an address or a token.
Describe a card by its role, such as "a card on a board I can write to".
A suspected vulnerability goes through [`SECURITY.md`](../SECURITY.md),
privately.
