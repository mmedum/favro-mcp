# CLAUDE.md — favro-mcp project instructions

Project-specific rules for Claude Code in this repository. The user's
global instructions still apply; this file adds to them.

## Mission

A Go MCP server for Favro, distributed to other people. The design, the
evidence log, the decided constraints and the phase plan live in
`docs/architecture.md`. Read it before changing the tool surface, the
error vocabulary, the write path, or the package layout.

This server is one of five that run on the shared standard in
`~/.claude/mcp-server-standard.md`; the siblings are google-chat-mcp,
google-docs-mcp, google-drive-mcp and google-sheets-mcp. Where this one
deviates, `docs/architecture.md` §17b says why. The alignment programme
that closes the remaining gaps is §16, phases A0–A8 — **check which phase
is current before starting work that a later phase is going to move.**

## Hard rules

1. **Nothing tenant-specific, ever.** No organization names or ids, card
   names, ids or sequential ids, user names or email addresses, tokens,
   or content from a real Favro tenant — in code, fixtures, tests, tool
   descriptions, commit messages, PR bodies, the changelog or the docs.
   Refer to test resources by role. Fixtures are generated, never
   recorded from a live response.
2. **A 200 from Favro is not confirmation.** Favro answers 200 for a
   request body it ignored, and its REST docs disagree with the live API
   in places. Verify a write by reading the resource back. Reads are
   tolerant — accept the documented shape *and* any shape previously
   observed, preferring the documented one (`Card.CustomFields()`,
   `Tasklist.Title()`, `Activity.CommonID()` exist for this). Writes send
   the documented shape, and the tool description says so when it is
   unverified.
3. **Stdout carries only MCP JSON-RPC frames.** Logs go to stderr through
   `slog`. Never `fmt.Println` on the server path. Never log a payload,
   and never log anything that identifies or reconstructs the subject.
4. **Every mutating tool takes `dry_run`** and has a test proving dry-run
   never reaches `RoundTrip`. The gate lives in the client, not in the
   tool.
5. **Every registered tool needs a row in `smokeToolInputs`**
   (`internal/tools/smoke_test.go`) or the smoke test fails.
6. **Pagination is never auto-aggregated.** List tools surface
   `next_page` and require an explicit follow-up.
7. **Tag tools hard-fail on unknown tag names** rather than creating
   them. Favro's own `addTags` creates unknown tags, which turns a typo
   into a permanent org-global one.
8. **Single-org.** The server binds `FAVRO_ORGANIZATION_ID` at startup
   and no tool takes an `organization_id`.
9. **Own wire types, raw REST.** There is no generated Favro client and
   there will not be one. The dependency direction is
   `server` → `tools` → `service` → `favroapi` → `favro`, with `config`,
   `cache`, `auth` and `render` as leaves; depguard fails the build on an
   import that runs uphill.
10. **No auto-commit, no auto-push.** Never push a tag without being
    asked.
11. **Destructive tools are registered only when
    `FAVRO_ENABLE_DESTRUCTIVE=true`**, and which ones is read from the
    `DestructiveHint` annotation in `addTool` — never from a list of
    names. `dry_run` is not a second guard for this: a host in an
    auto-approve mode runs an annotated tool without prompting.
12. **Every error a tool returns is `[class] actionable message`**, from
    the closed vocabulary in `internal/render`. The class comes from the
    error's type, never from matching its text; a new sentinel is built
    with `classed(render.Class…, …)`. Adding or removing a class means
    editing `docs/architecture.md` §6.2 in the same commit, or the
    `classes` gate fails.
13. **`content` and `structuredContent` are never the same bytes.** The
    SDK makes them identical if a handler leaves `Content` nil;
    `addTool` fills the readable half from `internal/render`. A new
    output shape that needs a better summary implements
    `render.Summarizer`.
14. **Any rule adopted from the standard needs three things**: make it a
    test, derive its list from the code rather than typing the list out,
    and have the checker assert a floor on how much it read. "Found
    nothing" and "looked at nothing" print the same sentence otherwise.

## Where things go

- `cmd/favro-mcp/` — server default, `auth` subcommands, `--version`,
  `--dry-run`.
- `internal/auth/` — credential resolution: env → OS keyring; `Token.Apply`.
- `internal/favro/` — the wire types, one file per resource. Imports
  nothing.
- `internal/favroapi/` — the REST client, one file per resource. No MCP.
- `internal/config/` — every `FAVRO_*` setting, resolved once at startup.
- `internal/cache/` — the TTL cache the resolver runs on.
- `internal/service/` — orchestration, and no MCP imports. Two
  non-obvious pieces:
  - `resolver.go` — the name→ID caches every `favro_resolve_*` tool and
    most write tools go through. **A write tool must invalidate the right
    cache on success**, which is why the `Invalidate*` methods are
    exported rather than internal.
  - `full_card.go` — the parallel dereferencing fan-out behind
    `favro_get_card_full`.
- `internal/render/` — the readable half of every result, and the closed
  error vocabulary. Imports nothing, which is what lets
  `internal/favroapi`'s errors name their own class.
- `internal/tools/` — the MCP surface, one file per area, plus
  `register.go` (the one list of what is registered) and `registry.go`
  (`addTool`, where the destructive gate and both result halves live).
- `internal/server/` — SDK wiring and the schema dump, and nothing else.
- `internal/version/` — the build stamp.
- `testdata/` — `api-surface.json` is machine-owned (`make api-diff`,
  network); `api-coverage.tsv` and `api-fields-waived.tsv` are the
  hand-written verdicts the offline gates hold it against. **Every
  Favro endpoint and every documented field needs a decision**, even if
  the decision is "out" with a reason.
- `scripts/livefavro/` — the live driver: `make live` runs every tool
  against a real organization. Every mutating step carries `dry_run`.
  It prints only through `internal/redact`, and the `transcript` gate
  fails the build if anything else reaches the terminal. **Do not commit
  a transcript** — the redactor removes ids and addresses, not names.
- `scripts/gates/` — this repository's own checks, as Go. **One language,
  and no shell** (standard §1). A new gate goes in the registry in
  `main.go`, gets a test, and asserts a floor on how much it read.
- `docs/architecture.md` design and plan; `docs/TOOLS.md` the tool
  reference; `README.md` stays scannable.

## Commands

```
make check     # everything CI runs — before every commit
make test      # unit tests with the race detector
make build     # ./bin/favro-mcp
make fmt       # gofumpt + goimports
make hooks     # point git at .githooks, so the leak scan runs at commit time
make help      # every target, with what it does
```

`make check` is the whole gate: fmt-check, vet, tidy, lint, cover, vuln,
licenses, secrets, leaks, pins, classes, api-coverage, api-fields,
transcript, live-cover, parity, plugin, schema-diff, smoke, staleness.
The thirteen gates among those are `scripts/gates`, one Go binary with a
registry, and `gates parity` fails if `make check` and
`ci.yml` stop running the same set — so adding a gate means adding it in
both places, and the registry's `gate: true` flag is what says a gate
belongs in both.

Go 1.27. `go.mod` is the single source of truth — every CI job resolves
via `go-version-file: go.mod`. Don't add a job pinned to an older minor.
Tool versions are pinned in the Makefile and `gates pins` holds them to
exactly one version each; look the current one up before changing a pin,
rather than writing one from memory.

## Definition of done

`make check` green, tests for the new behaviour, `/simplify` and
`/security-review` over the pending changes with findings resolved or
explained, and — because unit tests cannot catch what hard rule 2
describes — **live verification against a real organization before the
commit**: build, reconnect the MCP server, exercise the new tools, fix
what the wire contract actually turns out to be.

A phase ends with a commit — not a modified working tree — so the work
survives a cleared session and the diff can be reviewed as a unit. Say
what and why in the message. Pushing, opening the pull request and
tagging stay with the maintainer; ask. Then wait for an explicit "start
phase N" before the next one begins.

## Docs and releases

- **CHANGELOG.md** follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/):
  `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`, in
  that order, newest version first, ISO dates, link references at the
  bottom.
- **One line per change.** Say what changed, and why only when it isn't
  obvious. Investigation detail belongs in the commit body or the PR;
  deep context belongs in a code comment next to the code. Keep the
  specifics — tool names, wire keys, CVE ids, versions. Brevity means
  fewer words, not less information.
- Versioning is [SemVer](https://semver.org). Removing or renaming a tool
  input is a breaking change; so is un-registering a tool. If it ships in
  a minor, the changelog has to say why.
- Entries accumulate under `[Unreleased]` and move under a version
  heading only in a dedicated release PR. Tags are cut by the maintainer,
  never proposed automatically.
- **Don't state a version in prose.** The README uses a release badge,
  which cannot go stale. `docs/architecture.md`'s status line is the one
  exception, because no tag can derive which phase is current — so check
  it against the newest changelog heading when you touch it.

## Workflow

Feature branch off `main`, PR, green CI. Never commit to `main` directly,
and never push a tag without being asked. `main` is protected: a PR is
required, eight status checks must pass, and there are no bypass actors.
