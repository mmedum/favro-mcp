# Security

Reporting a vulnerability: [SECURITY.md](../SECURITY.md). Never a public
issue.

## Trust boundaries

- **The person** runs the binary under their own account. They may also
  approve each tool call in their MCP client, but that is not a boundary
  this server relies on: hosts have auto-approve modes and the MCP spec
  says clients treat tool annotations as untrusted. What this server
  controls is which tools it registers and what it refuses.
- **The MCP client** (Claude Code, Claude Desktop, Cowork) speaks
  JSON-RPC over stdio. Stdout carries only protocol frames; every log
  goes to stderr through `slog`.
- **Favro** is the only network peer: `favro.com`. No telemetry, no crash
  reporting, no update check. The one exception is a maintainer command —
  `gates api-diff` fetches `favro.com/developer` to refresh the API
  snapshot, and it never runs in `make check`.

There is no multi-tenant boundary to cross. The server holds one token,
binds one organization at startup, and no tool accepts an
`organization_id`.

## Credentials

- HTTP Basic with an address and an API token, because Favro's REST API
  offers no OAuth flow at all. Both come from the environment or the OS
  keyring; `favro-mcp auth login` writes the keyring entries with the
  token input masked.
- **The token is never printed.** Not by a tool result, not by a log line
  at any level, not by `auth status`, and not by `doctor` in either of
  its modes. `doctor` prints its length and deliberately not a prefix: a
  prefix of a secret is part of a secret.
- Favro's API tokens carry the issuing user's own permissions and cannot
  be scoped down at issue time. The honest mitigation is to issue the
  token from a least-privileged service user rather than from an admin
  account.
- `favro-mcp auth logout` deletes the keyring entries. It cannot revoke
  the token at Favro — do that in Favro's own UI.

## What does not reach a log

Logs carry startup and shutdown facts and, at `debug`, one line per
request: the method, the redacted path, the names of the query
parameters, the attempt number and the redacted headers. There is no
response log at all — not a status code, not an elapsed time. Nothing
carries a payload, and nothing carries anything that identifies or
reconstructs the subject. Four things
used to, and each is now held by a test:

- The query string, which carries `cardCommonId`, `widgetCommonId` and
  `sequentialId`. The parameter *names* are logged instead.
- The path, which is `/cards/{cardId}` for every get-one endpoint. Ids in
  the path are masked.
- The `organizationId` header, which `Token.Apply` sets on every request.
- The startup line's `organization_id`, which named the tenant at `info`
  in the first line of every session.

A debug log is therefore safe to attach to a bug report. `favro_ping`
still returns the organization id, because a tool result goes to the
caller who asked for it; a log goes to whoever ends up holding the file.

## What `doctor` prints

`doctor` is a different surface from a log, and a more dangerous one,
because the issue form asks people to paste it in public. It is held to
the same rule by construction rather than by care: every line goes
through `internal/redact`, and the values it must not leak are
*registered* with the redactor by name rather than left to a pattern to
notice.

That distinction is the whole of it. Every pattern in `internal/redact`
is anchored on a shape a tenant's data takes — a 24-hex run, an address,
a link — so a value that is tenant data and takes some other shape passes
straight through. The organization ids `doctor` prints arrive from the
API rather than from configuration, and nothing but a regex stood between
them and a public issue until they were registered.

`--show-ids` prints ids and addresses in full, for the user's own screen,
and says on its own output that it is unsafe to share. The token is
scrubbed in that mode too.

## The transcript the live driver writes

`scripts/livefavro` drives the built binary against a real organization
and prints only through `internal/redact`; the `transcript` gate fails
the build if anything else reaches the terminal.

**What that redactor cannot do is measured rather than assumed.** On a
complete run — 122 steps, of which 104 reached Favro and 18 were skipped
for ids that organization does not have — it produced 56 KB in which
there were zero ids of either shape, zero addresses, zero app links and
zero signed URLs. It also left 54 card and board *names*, because a name
is ordinary words
and a pattern that caught it would catch the rest of the sentence. That
is acceptable for a maintainer's terminal showing them an organization
they already hold a token for. It is not acceptable in a file, and the
leak gate cannot catch it either. **Do not commit a transcript.**

## What can go wrong and what limits it

| Risk | Mitigation |
|---|---|
| A **delete-style** tool runs unattended | They are **not registered** unless `FAVRO_ENABLE_DESTRUCTIVE=true`. A host in an auto-approve mode runs an annotated tool without prompting, and the MCP spec says clients treat annotations as untrusted — so the only guarantee is absence from `tools/list`. Which tools those are is read from the annotation at registration, never from a list of names. This covers deletion and nothing else: `favro_update_card` replaces a description wholesale, and it, `favro_replace_in_card_description`, `favro_update_comment` and `favro_update_tags` are registered by default. They are not destructive in the MCP sense — they destroy no resource — but they do overwrite, and Favro has no undo. |
| A write happens during a preview | Every mutating tool takes `dry_run`, and the gate lives in the HTTP client rather than in each tool, so a dry run cannot reach `RoundTrip`. A test proves it per tool. |
| A typo creates a permanent org-global tag | The tag tools hard-fail on an unknown tag name rather than creating it. Favro's own `addTags` creates unknown tags, which turns a typo into something everyone in the organization then sees. |
| A write is reported as succeeding when Favro ignored it | Favro answers 200 for a request body it ignored, and its REST docs disagree with the live API in places. Writes send the documented shape; a tool whose write is unverified says so in its description; and the project's definition of done requires reading the resource back against a real organization before a phase ships. |
| Tenant data reaching the repository | `gates leaks` scans the working tree for addresses, 24-hex ids, keyed organization ids, app links and card references, and `gates leaks history` scans every blob and commit message. It caught the example card reference in the bug-report form's own first draft. |
| Secrets reaching the repository | gitleaks in `make check` and in CI, with `.gitleaks.toml`. The pre-commit hook runs the faster tenant-data scan (`gates leaks`) rather than gitleaks, so a committed credential is caught by `make check` or CI and not at the commit itself. Every fixture is generated rather than recorded from a live response. |
| A tampered release | Archives, the `.mcpb` bundle and `checksums.txt` are covered by a keyless cosign signature and by build provenance; an SBOM ships per archive. `gates mcpb` holds the eight configuration lines that make that true, because deleting any one of them ships an unsigned artifact while every other check stays green. See the README's "Verifying a download". |
| A release tag carrying a command | A tag name reaches a `run:` block, git permits `$`, backtick, `;` and `{}` in a ref name, and the release job holds `id-token: write`. The tag goes through the environment rather than into the script, the trigger admits only alphanumeric pre-release suffixes, and `gates release-notes` refuses a version that is not one. |
| A hung Favro endpoint | Every API call carries a 30s client timeout; the startup validation carries 5s and `doctor` caps its whole live half at 15s. |
| Regex denial of service | Go's RE2 engine, linear time. |

## Threats this server does not address

- **Anything the token can do in Favro, and anything the account can read
  on disk.** The first half is bounded by choosing the user the token
  belongs to. The second half is not bounded by the token at all — see
  the next bullet.
- **Reading local files.** `favro_upload_attachment` and
  `favro_upload_comment_attachment` take a `file_path` and read whatever
  the account running the server can read, with no root confinement and
  no allowlist — only a regular-file check and a size cap. Both are
  registered **by default**: they destroy nothing, so they are not behind
  `FAVRO_ENABLE_DESTRUCTIVE`, and `dry_run` is a preview rather than a
  guard.

  That is the tools' documented purpose, and it is also an exfiltration
  path: combined with the bullet below, a card description can tell a
  model to attach a credential file to a card, and Favro being the only
  network peer is what makes it a usable channel rather than what
  prevents one. **The containment is the account the server runs under.**
  Do not run it under an account whose filesystem you would not be
  willing to attach to a Favro card.
- **Prompt injection through Favro content.** A card description is
  untrusted text that reaches a model. This server renders it faithfully
  and does not interpret it; nothing here prevents a model from acting on
  instructions it reads in a card. Read that together with the bullet
  above rather than on its own: the worst case is not a stray Favro
  write.
- **A compromised MCP host.** The host holds the stdio pipe and decides
  which tools to call.
