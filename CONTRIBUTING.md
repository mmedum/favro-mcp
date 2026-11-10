# Contributing to favro-mcp

Thanks for considering a contribution. This is a Go MCP server for
Favro, run by people against their own organization. Read this once
before your first pull request; CI enforces the rest.

## Before anything else

**Nothing from a real Favro organization may enter this repository**
(D-49). Not organization ids or names; not the names, ids or sequential
ids of real cards, boards or collections; not user names, addresses or
API tokens; not text from a real card. This holds for code, docs,
fixtures, tool descriptions, issues, pull requests, commit and tag
messages and logs. Refer to test resources by role, such as "a card on a
board the token can write to". Naming the sibling servers, such as
google-docs-mcp, is fine.

Fixtures are synthetic and written inline in the tests. `make leaks`
scans the tree for addresses at a real domain, Favro's 24-hex ids, keyed
organization ids and tokens, app links and card references.
`make secrets` runs gitleaks for credentials, over every commit HEAD
reaches. Both run in `make check` and CI. `make hooks` points git at
`.githooks`, whose pre-commit hook runs gofmt, `go vet`, the leak scan
and gitleaks over the staged changes on every commit.

**Commit under your GitHub no-reply address**, the one at
`users.noreply.github.com`, and credit co-authors with theirs. The
history is public and is not rewritten (D-50). `make leaks-history`
reads every commit message, trailers included, but not the identity git
records, so that one is on you.

## Reporting bugs and requesting features

Open a [GitHub issue](https://github.com/mmedum/favro-mcp/issues) with
what [`docs/troubleshooting.md`](docs/troubleshooting.md#reporting-a-problem)
lists. Never paste a tool result: it is your organization's data.

For anything security-sensitive, do not open a public issue. Follow
[`SECURITY.md`](SECURITY.md).

## Development setup

```bash
git clone https://github.com/mmedum/favro-mcp
cd favro-mcp
make build
make hooks
```

Go is the only build prerequisite, at the version in `go.mod` (D-79);
`GOTOOLCHAIN=auto`, the default, fetches it. `make check` runs
golangci-lint, govulncheck, go-licenses and gitleaks with `go run` at the
versions the Makefile names, and `make pins` holds those to the ones CI
uses (D-80). Everything the repository runs on itself is Go, in
`scripts/`; the one shell file is the git hook that runs it (D-06).
`make help` lists every target.

### Running your own build

To point Claude Code at your build rather than a release, create an
untracked `.mcp.json` at the repository root:

```json
{
  "mcpServers": {
    "favro": {
      "command": "${CLAUDE_PROJECT_DIR:-.}/bin/favro-mcp"
    }
  }
}
```

It is gitignored, since `bin/` is a build output and a committed config
would name a command a fresh clone does not have. The config shipped to
users is `plugin-template/.mcp.json`. Run `make build`, reconnect the
server, and check with `favro_ping` that the version is your tree's.

## Gates that must pass

```bash
make check
```

`make check` runs the gates CI runs, and the `check` target in the
`Makefile` is the list. `make parity` fails when that list and `ci.yml`
differ, command by command, or when a gate runs outside the pipeline its
dispatch entry names, so a check you add goes in both (D-69). CI runs the tests on
Linux, macOS and Windows (D-82).

`go run ./scripts/gates` lists the checks, and `go run ./scripts/gates
<name>` runs one; their tests are in `go test ./scripts/gates`. A check
derives its lists from the code and fails when it reads too little to be
looking at anything (D-68). Before you trust a gate you changed, break
what it holds and watch it fail.

These are worth knowing before they fail on you:

- **The coverage floor** is 80% of statements in every package on its
  own, not on the average (D-71). An exemption names its reason.
- **`make schema-diff`** holds the committed `schemas.json` to the
  binary's dump, and the tool surface to the newest release tag HEAD
  reaches (D-64).
  `make schemas` rewrites the file.
- **`make api-coverage` and `make api-fields`** hold every endpoint and
  field of Favro's reference to a verdict in `testdata/api-coverage.tsv`
  or `testdata/api-fields-waived.tsv` (D-72).
- **`make outcomes`** fails a tool that states an outcome its response
  did not carry (D-25). An honest branch takes a row in
  `testdata/outcome-claims.tsv` with its reason.
- **`make classes`** holds the error vocabulary in
  `internal/render/class.go` to the table in
  [`docs/TOOLS.md`](docs/TOOLS.md#error-classes) (D-40).
- **`make staleness`** holds `docs/TOOLS.md`'s tool table to the binary
  both ways, the README's permission rules and plugin section, and
  `docs/configuration.md` to every setting the code reads (D-78). A
  commit since the newest release tag HEAD reaches that changes what
  users get, as D-78 lists it, needs a CHANGELOG entry.
- **`make live-cover`** fails when a tool or option has no live step, or
  no waiver in `testdata/live-cover-waived.tsv` (D-74).

## Tests

Unit tests use the standard library, `if got != want { t.Errorf(…) }`,
and run with the race detector (D-70). depguard refuses
`stretchr/testify` and `pmezard/go-difflib`. The client's tests run
against `httptest` servers, and the tool tests drive the server through
the SDK's in-memory transport.

- `TestEveryWriteTakesDryRunAndSendsNothing` holds every tool that is not
  read-only to taking `dry_run`, and a dry run to sending no write
  (D-28). A new write embeds `dryRunInput`.
- `TestMCP_AllTools_SmokeCallable` fails until every registered tool has
  a row in `smokeToolInputs` in `internal/tools/smoke_test.go` (D-62).
- A fake answers as its author believed Favro would, so it agrees with
  that belief. Only the live run tests it (D-73).

## Versioning

[SemVer](https://semver.org/spec/v2.0.0.html), read through the tool
surface: names, input and output schemas, annotations and documented
behavior. A removed tool, a removed or renamed input, or a newly required
one is a breaking change. `make schema-diff` fails on it with no way
past, so it needs the maintainer's decision first. If one ships in a
minor release, its CHANGELOG entry says why.

On a pull request, CI also runs `schema-ack` (D-65). A commit footer has
to name each item of the tool surface that changed since the
merge-base, a hint or a description included:

```bash
git commit --allow-empty -m "Acknowledge the tool surface change" \
  -m "SCHEMA-CHANGE: favro_update_card, favro_move_card: destructiveHint true"
```

Name a tool by its name and the instructions as `instructions`. Only the
names before the first colon followed by white space count, separated
by commas; a list wraps only after a comma, and the rest describes the
change. A `Token #value` footer such as `Closes #12` acknowledges
nothing. A break of something the last tag shipped is named in a
`BREAKING CHANGE:` footer instead. A refusal prints the footer that
fixes it, to add in an empty commit, never an amend. `make schema-ack`
runs the check locally against `origin/main`.

## Adding a tool

1. Put the wire call in `internal/favroapi` and the orchestration in
   `internal/service`, with tests against `httptest` fakes. Tools stay
   thin.
2. Register it in `internal/tools` through `addTool` (D-60). A write
   embeds `dryRunInput`, a destructive one `confirmInput` (D-29), and
   `TestEveryWriteIsClassified` names it as one that adds, changes or
   destroys (D-33).
3. Errors are `[class] message`, from `render.Sentinel` or a typed error
   in `internal/favroapi` (D-40).
4. Add a row to `smokeToolInputs` (D-62).
5. Add a row to `docs/TOOLS.md`. A destructive tool also gets a
   `permissions.ask` rule in the README, and a new setting a row in
   `docs/configuration.md`.
6. Add a step in `internal/livecover`, or `make live-cover` fails (D-74).
7. Give each endpoint and field it uses a verdict in `testdata/` (D-72).
8. Run `make schemas`, read `make schema-diff`, and add the
   `SCHEMA-CHANGE:` footer above.

## The live run

Every fake in the unit suite is written from what we believe Favro does,
and only a live run tests that belief (D-73). Anything touching the write
path or an API response shape needs one.

```bash
make live
go run ./scripts/livefavro -only favro_list_cards -v
```

- **What it needs.** A real organization and the credentials the server
  reads: `FAVRO_USER_EMAIL`, `FAVRO_API_TOKEN` and
  `FAVRO_ORGANIZATION_ID`, or the keyring entries `favro-mcp auth login`
  writes. `make live` builds the binary first; the driver alone runs
  `./bin/favro-mcp`, and `-bin` names another.
- **A dry run against real data.** The driver starts the server with
  `--dry-run`, `FAVRO_ENABLE_DESTRUCTIVE=true` and an upload directory it
  makes and removes (D-74). The steps run against the first objects the
  organization lists, and a step whose object the organization lacks is
  skipped. Every write step carries `dry_run`, and the server's client
  refuses to send any write, so a step checks what Favro answers to a
  read and to a dry run's validation.
- **`-only` and `-v`.** `-only <tool>` runs that tool's steps; `-v`
  prints what each step is for after its name, and each result,
  redacted.
- **Named steps.** Every step and every `-writes` check has a unique
  name in `internal/livecover`, printed on its `ok`, `FAIL` or `SKIP`
  line. A step that must be refused asserts the error class. A comment
  at the code cites one as `Checked by the live step "<name>".`

`go run ./scripts/livefavro -writes` checks the writes the steps can only
preview, and is the one mode that writes (D-75). It creates a probe tag
and deletes it, first without `confirm: true`, which must be `[blocked]`
and leave the tag, then with it. It creates a probe collection, checks
that a public create is `[blocked]` and created nothing, and opens the
collection to the organization with `favro_widen_collection_sharing`,
after that tool refuses a name that is not the collection's. It reads
the collection back, narrows it with `favro_update_collection`, and
deletes it the same two ways. The run fails if the server asks the
person anything (D-31). It sends a write only to a probe it created.

**What it leaves behind.** Nothing, when it ends well. `make live`
writes nothing to Favro. Each `-writes` probe is named `livefavro-…`, and
however the run ends, SIGINT, SIGTERM and SIGHUP included, a sweep looks
for each probe by name, deletes it, reads that it is gone, and names any
that is still there. The driver prints a redacted summary and writes no
transcript file.

**Reading it.** Everything it prints goes through `internal/redact`,
which masks ids, addresses and the credentials, but not card or board
names (D-51). Never commit or paste the output.

### Evals

```bash
make evals
go run -tags=evals ./scripts/evals -only <task>
```

Evals score whether a model can do the job through the tools, by the end
state read back and by the tool-call trace (D-76). They need the
credentials above and the `claude` command, and spend API usage; `-model`
picks the model and `-budget` caps one task in USD. The model gets this
server's tools and nothing else, from a server started without
`FAVRO_ENABLE_DESTRUCTIVE` (D-77). Each run builds a sandbox collection
named `favro-mcp evals …` and removes it at the end, even after a failed
build, unless `-keep` is set.

## Branch and commit conventions

- `main` takes pull requests only, the maintainer's and release commits
  included (D-87). Work on a short topic branch.
- Write the subject in the imperative, under 72 characters, saying what
  changes. Look at a recent `git log` for the tone.
- The body says the problem, why this change solves it, and the
  alternatives rejected. A fixed review finding is one line at the end
  of the body. A declined one goes in the pull request, or becomes a
  present-tense comment at the line it protects.
- Pull requests merge with a merge commit, so each stays one entry in
  `git log --merges`.
- The maintainer pushes, merges, tags and releases.

## Pull requests

1. Open against `main`, with a summary and a test plan.
2. CI must be green and up to date with `main`, and every conversation
   resolved.
3. A maintainer reviews and merges.

`main` has a ruleset with no bypass actors. It requires a pull request
and these status checks: `test (ubuntu-latest)`, `test (macos-latest)`,
`test (windows-latest)`, `gates`, `cover`, `static`, `secrets` and
`coverage`. It blocks force-pushes and deletion. A check is matched by
its job name, so renaming a CI job drops its requirement. Do not add a
required check before the workflow producing it is on `main`: a check
that has never run blocks every pull request, the one adding it
included.

Every action is pinned to a full commit SHA, and every tool an action
installs to one exact version; `make pins` holds both (D-80).

## Changelog

Every user-visible change gets an entry under `## [Unreleased]` in
`CHANGELOG.md`, in the same pull request that makes the change. Leave
out changes only contributors see, unless they must act on them; such a
line starts `For contributors:`.

- **Sections** are the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
  set and nothing else: Added, Changed, Deprecated, Removed, Fixed and
  Security, in that order. Newest version first, ISO dates, link
  references at the bottom.
- **One line per change, 25 words or fewer, impact first.** Keep the
  specifics, such as tool names, wire keys and versions. The
  investigation belongs in the commit message.
- **Breaking changes** start with `**Breaking:**`, name the item in
  backticks, and may add one sentence saying what to do.
- **Security fixes** name the CVE or GHSA.

The release workflow takes the version's section as the release notes,
so the entry is the release note (D-83).

## Releasing

Maintainers release as [`docs/release.md`](docs/release.md) says.

## Code style

- Match the surrounding code. Do not reformat what you did not touch.
- Comments explain why: a constraint, an invariant, something Favro
  does that the code has to work around. Do not narrate what the code
  does.
- A decision in `docs/architecture.md` is `D-NN`. Cite it by ID, or a
  doc by its path and heading anchor, never by section number. What you
  verified about Favro goes in the comment at the line that depends on
  it, with the reference link and the live step that checks it.
- Write plainly: lead with the outcome, one idea per sentence. This
  holds for comments, commit messages, the CHANGELOG and tool
  descriptions.
- Logs record the call, never the payload (D-46).

`CLAUDE.md` has the short version of the rules, and
[`docs/architecture.md`](docs/architecture.md) the reasoning, one
decision per D-ID, and the [codemap](docs/architecture.md#codemap).

## Code of conduct

Taking part in this project means agreeing to
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md), the Contributor Covenant
3.0. It says how to report a problem, and what happens then.

## License

By contributing, you agree that your contributions will be licensed
under the project's [Apache 2.0 License](./LICENSE).
