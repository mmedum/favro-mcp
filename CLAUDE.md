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

1. **Keep everything internal out of the repository**, because it is
   public and a pushed commit cannot be taken back: organization names or
   ids, real card, board or collection names and ids, addresses, tokens,
   and content from a real organization. That holds in code, fixtures,
   docs, commits, pull requests and logs. Naming a sibling server, such
   as google-docs-mcp, is fine. Fixtures are synthetic and name resources
   by role (D-49). `make leaks` and `make secrets` hold it.
2. **Write to stdout only from `main`.** Stdout carries only MCP JSON-RPC
   frames, because a client reads each line as a frame and one stray
   print breaks the session; logs go to stderr through `slog` (D-45).
   forbidigo, `make smoke` and `TestForbidigoGuardsStdout` hold it.
3. **Log the call, not a payload or anything that identifies the
   subject, and keep the token out of every output** (D-46, D-47). A
   printed line ends up in a log file, an issue or a transcript.
   `TestDebugLogNeverCarriesTheSubject` and
   `TestSecretsScrubsTheTokenAndLeavesIdentifiers` hold it.
4. **Take confirmation from a read-back or a live run, not from Favro's
   200.** Favro answers 200 for a body it ignored. A write Favro may drop
   is read back (D-24), and a wire contract is verified live (D-73).
   Reads accept the documented and every observed shape; writes send the
   documented one (D-15). `TestMCP_CardPlacement_ReadBack` and the live
   run hold it.
5. **Send every write where the client's dry-run gate can stop it.**
   Every write takes `dry_run`, gated in `internal/favroapi`, and
   `--dry-run` forces it (D-28). The client holds the gate, so no tool
   can forget it. `TestEveryWriteTakesDryRunAndSendsNothing` holds it.
6. **Register every tool through `addTool`** (D-60), with a row in
   `smokeToolInputs` in `internal/tools/smoke_test.go` (D-62). A rule
   applied in each handler is a rule each handler can forget. `addTool`
   keeps `content` and `structuredContent` apart (D-61).
   `TestMCP_AllTools_SmokeCallable` and
   `TestContentAndStructuredContentDiffer` hold it.
7. **Register a destructive tool only with
   `FAVRO_ENABLE_DESTRUCTIVE=true`, and run one only with `confirm: true`**
   unless it is a dry run (D-29, D-30). The tool that cannot run
   unattended is the one not registered. A tool is destructive when its
   input embeds `confirmInput`. `destructiveHint` is false only on a write
   that adds (D-33). `TestDestructiveToolsAreOptIn`,
   `TestEveryWriteIsClassified` and `TestEveryDestructiveCallNeedsConfirm`
   hold it.
8. **Leave per-call approval to the client rather than asking the person,
   and keep the server's own settings out of every tool's reach** (D-31,
   D-39). A tool that could change a setting would let card text widen
   the model's reach. `requiresUserInteraction` stays on the tools D-34
   names. `TestNoToolWritesTheServersOwnSettings` and
   `TestOnlyTheWidestWritesPrompt` hold it.
9. **Widen sharing only through the tool made for it, not an ordinary
   write.** Collection, group and widget writes share only inside the
   organization; opening a collection is `favro_widen_collection_sharing`
   (D-35, D-36). `TestOrdinaryWritesShareOnlyInsideTheOrganization` holds
   it.
10. **Bind the organization at startup rather than take an organization
    id, return one page per call rather than aggregate pages, and refuse
    an unknown tag name rather than create a tag from it** (D-02, D-16,
    D-21). A typo would otherwise become a tag the whole organization
    sees. `make one-organization` and
    `TestMCP_AddTagToCard_HardFailUnknown` hold the first and the last.
11. **Return every error as `[class] message`**, because the class tells
    the caller what to do next. The class comes from the error's type,
    not its text, and a new sentinel is `render.Sentinel` (D-40). A new
    class needs a row in `docs/TOOLS.md#error-classes`; `make classes`
    holds both.
12. **Invalidate the caches a write changed, from the write tool**, which
    is why the resolver's `Invalidate*` methods are exported (D-09). A
    cache that outlives a write answers the next resolution with the
    world before it. `TestInvalidateCacheHooks` holds it.
13. **Hand-write the wire types rather than generate a Favro client, and
    import only downhill.** The wire types are this repository's own
    (D-04), and imports run `server` → `tools` → `service` → `favroapi`
    → `favro` (D-05); depguard holds it. Record a verdict in
    `testdata/api-coverage.tsv` or `testdata/api-fields-waived.tsv` before
    you call an endpoint or model a field (D-72); `make api-coverage` and
    `make api-fields` hold it.
14. **Keep every released tool and input, under its name, and keep
    optional inputs optional** (D-64), because the released surface is a
    contract. `make schema-diff` fails on a break, and a pull request
    names each surface change in a commit footer (D-65).
15. **Print from the live driver and the evals only through
    `internal/redact`, and keep every transcript out of the repository**
    (D-51). The redactor removes ids and addresses, not names.
    `make transcript` holds the printing.
16. **Write Go, not shell**, because a shell script is held to no gofmt,
    vet, lint or test. Every check is Go in `scripts/gates`, derives its
    list from the code and asserts a floor on how much it read (D-06,
    D-68). `make parity` holds `make check` and CI to one set (D-69).
17. **Commit to a branch, not `main`, and leave the push to the owner.**
    The owner pushes, tags and merges (D-87).
18. **Adopt a belief about Favro only from a primary source**: its
    reference or a live run (D-73). Record a verified Favro behavior in
    the comment at the line that depends on it, with the reference link
    and the live step that checks it.

## Definition of done

- `make check` passes. On a pull request CI also runs `schema-ack`,
  which needs a commit footer naming each changed item of the tool
  surface, and `changelog`, which needs an entry under `[Unreleased]`
  or a `Changelog: none` footer (`CONTRIBUTING.md`).
- New behavior has tests, and the schema diff has been read.
- `/simplify` and `/code-review medium` have run, the review at medium
  rather than high. A fixed finding is one line in the commit body. A
  declined one goes in the PR body, or becomes a present-tense
  constraint comment at the line it protects. Findings go there, not
  into `docs/`.
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
  went out is `ambiguous_outcome`, not a retry (D-41).
- **The HTTP timeout is per attempt.** A read retried after a 5xx can
  take longer in all.
- **A linter settings block that fails to load leaves its defaults on**,
  still firing. Check the message text of forbidigo and depguard.
- **Supersede or deprecate a decision rather than delete it, and take a
  fresh ID rather than reuse one.** The next ID is on the file's
  `Next ID:` line.

## Writing

Plain and short: lead with the outcome, one idea per sentence, no
narration, no count a gate does not check. History goes in commit
messages. `release.yml` lifts the version's CHANGELOG section into the
release notes, so follow the rules in `CONTRIBUTING.md` to the letter.
