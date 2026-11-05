# CLAUDE.md — favro-mcp

A Go MCP server for Favro over stdio: one binary per person, their own
Favro address, API token and organization, nothing hosted (D-01, D-02).
Why the code is the way it is lives in `docs/architecture.md`, one
decision per `D-NN`. Read the decision before changing the tool surface,
the error vocabulary, the write path or the package layout.

## Commands

- `make check` runs every gate CI runs; its `check` target is the list.
  `make hooks`, once per clone, adds the leak scan at commit time.
- `make live` drives the built binary against a real organization with
  `--dry-run`. `go run ./scripts/livefavro -writes` checks real writes
  on probes it creates and deletes. `CONTRIBUTING.md` covers both and
  the evals; `make api-diff` is a step in `docs/release.md`.
- `go run ./scripts/gates` lists the gates; add a name to run one.

## Hard rules

1. **Never commit anything internal**: organization names or ids, real
   card, board or collection names and ids, addresses, tokens, or content
   from a real organization, anywhere: code, fixtures, docs, commits,
   pull requests or logs. Naming a sibling server, such as
   google-docs-mcp, is fine. Fixtures are synthetic and name resources by
   role (D-49). `make leaks` and `make secrets` hold it.
2. **Never write to stdout outside `main`.** Stdout carries only MCP
   JSON-RPC frames; logs go to stderr through `slog` (D-45). forbidigo,
   `make smoke` and `TestForbidigoGuardsStdout` hold it.
3. **Never log a payload or anything that identifies the subject, and
   never print the token** (D-46, D-47).
   `TestDebugLogNeverCarriesTheSubject` and
   `TestSecretsScrubsTheTokenAndLeavesIdentifiers` hold it.
4. **Never treat a 200 from Favro as confirmation.** Favro answers 200
   for a body it ignored. A write Favro may drop is read back (D-24), and
   a wire contract is verified live (D-73). Reads accept the documented
   and every observed shape; writes send the documented one (D-15).
   `TestMCP_CardPlacement_ReadBack` and the live run hold it.
5. **Never send a write the client's dry-run gate cannot stop.** Every
   write takes `dry_run`, gated in `internal/favroapi`, and `--dry-run`
   forces it (D-28). `TestEveryWriteTakesDryRunAndSendsNothing` holds it.
6. **Never register a tool outside `addTool`** (D-60), or without a row in
   `smokeToolInputs` in `internal/tools/smoke_test.go` (D-62). `addTool`
   keeps `content` and `structuredContent` apart (D-61).
   `TestMCP_AllTools_SmokeCallable` and
   `TestContentAndStructuredContentDiffer` hold it.
7. **Never register a destructive tool without
   `FAVRO_ENABLE_DESTRUCTIVE=true`, or run one without `confirm: true`**
   unless it is a dry run (D-29, D-30). A tool is destructive when its
   input embeds `confirmInput`. `destructiveHint` is false only on a write
   that adds (D-33). `TestDestructiveToolsAreOptIn`,
   `TestEveryWriteIsClassified` and `TestEveryDestructiveCallNeedsConfirm`
   hold it.
8. **Never ask the person anything, or let a tool write the server's own
   settings** (D-31, D-39). `requiresUserInteraction` stays on the tools
   D-34 names. `TestNoToolWritesTheServersOwnSettings` and
   `TestOnlyTheWidestWritesPrompt` hold it.
9. **Never widen sharing through an ordinary write.** Collection, group
   and widget writes share only inside the organization; opening a
   collection is `favro_widen_collection_sharing` (D-35, D-36).
   `TestOrdinaryWritesShareOnlyInsideTheOrganization` holds it.
10. **Never take an organization id, aggregate pages, or create a tag
    from a name** (D-02, D-16, D-21). `make one-organization` and
    `TestMCP_AddTagToCard_HardFailUnknown` hold the first and the last.
11. **Never return an error outside `[class] message`.** The class comes
    from the error's type, never its text, and a new sentinel is
    `render.Sentinel` (D-40). A new class needs a row in
    `docs/TOOLS.md#error-classes`; `make classes` holds both.
12. **Never leave a cache stale after a write.** A write tool invalidates
    the caches it changed, which is why the resolver's `Invalidate*`
    methods are exported (D-09). `TestInvalidateCacheHooks` holds it.
13. **Never generate a Favro client or import uphill.** The wire types
    are this repository's own (D-04), and imports run `server` → `tools`
    → `service` → `favroapi` → `favro` (D-05); depguard holds it. Never
    call an endpoint or model a field without a verdict in
    `testdata/api-coverage.tsv` or `testdata/api-fields-waived.tsv`
    (D-72); `make api-coverage` and `make api-fields` hold it.
14. **Never rename or drop a released tool or input, or make an input
    required** (D-64). `make schema-diff` fails on it, and a pull request
    names each surface change in a commit footer (D-65).
15. **Never let the live driver or the evals print except through
    `internal/redact`, and never commit a transcript** (D-51). The
    redactor removes ids and addresses, not names. `make transcript`
    holds the printing.
16. **Never write a shell script.** Every check is Go in `scripts/gates`,
    derives its list from the code and asserts a floor on how much it
    read (D-06, D-68). `make parity` holds `make check` and CI to one set
    (D-69).
17. **Never commit to `main` or push.** The owner pushes, tags and merges
    (D-87).
18. **Never adopt a belief about Favro without a primary source**: its
    reference or a live run (D-73). Record a verified Favro behavior in
    the comment at the line that depends on it, with the reference link
    and the live step that checks it.

## Definition of done

- `make check` passes. On a pull request CI also runs `schema-ack`,
  which needs a commit footer naming each changed item of the tool
  surface (`CONTRIBUTING.md`).
- New behavior has tests, and the schema diff has been read.
- `/simplify` and `/code-review medium` have run, never at high. A fixed
  finding is one line in the commit body. A declined one goes in the PR
  body, or becomes a present-tense constraint comment at the line it
  protects. Never write findings into `docs/`.
- Anything touching the write path or an API response shape has had a
  `make live` run, and `go run ./scripts/livefavro -writes` when it
  touches confirm or sharing (D-73, D-75).
- `/security-review` runs before a release (`docs/release.md`).

## Traps

- **Green gates are not done.** Every fake is written from belief; only
  the live run tests it (D-73, D-74).
- **`make live` sends no write.** Only `-writes` sends one, to probes it
  made.
- **A write is sent once.** A 5xx or a connection lost after the request
  went out is `ambiguous_outcome`, never retried (D-41).
- **The HTTP timeout is per attempt.** A read retried after a 5xx can
  take longer in all.
- **A linter settings block that fails to load leaves its defaults on**,
  still firing. Check the message text of forbidigo and depguard.
- **Never delete a decision or reuse an ID.** Supersede or deprecate it,
  and take the next ID from the file's `Next ID:` line.

## Writing

Plain and short: lead with the outcome, one idea per sentence, no
narration, no count a gate does not check. History goes in commit
messages. `release.yml` lifts the version's CHANGELOG section into the
release notes, so the rules in `CONTRIBUTING.md` are not optional.
