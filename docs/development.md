# Development

Go 1.27. `go.mod` is the single source of truth — every CI job resolves
the toolchain with `go-version-file: go.mod`, and there is deliberately
no N-1 matrix entry.

```
make check     # everything CI runs — before every commit
make test      # unit tests with the race detector
make build     # ./bin/favro-mcp
make fmt       # gofumpt + goimports
make hooks     # point git at .githooks, so the leak scan runs at commit time
make help      # every target, with what it does
```

## Gates

`scripts/gates` is one Go binary with a registry. One language and no
shell: a shell script is held to no gofmt, vet, lint or test, `make
check` runs on the Windows runner where bash is a dependency rather than
a given, and a script that parses JSON with `sed` is how a quote ends up
inside a string. Two shell scripts were replaced by it.

`go run ./scripts/gates` lists them all. The starred ones run in both
`make check` and `ci.yml`, and `gates parity` fails the build if those
two lists stop agreeing — so adding a gate means adding it in both
places, and the registry's `gate: true` flag is what says it belongs.

| Gate | Holds |
|---|---|
| `coverage` | The per-package statement floor, 80%, per package rather than on the average — an average hides a package at 20% behind four at 95%. |
| `leaks` | Nothing identifying a tenant is in the working tree. `leaks history` scans every blob and commit message, since a tree scan cannot see what a later commit cleaned up. |
| `pins` | Every action is a full commit SHA, every tool version an exact one, every workflow pins its shell. An action that fetches `latest` is a pinned wrapper around an unpinned dependency. |
| `classes` | The closed error vocabulary and `docs/architecture.md` §6.2 name each other, from both sides. A documented class no code emits fails too — that is the side that rots. |
| `api-coverage` | Every documented Favro endpoint has a verdict and every verdict an endpoint. |
| `api-fields` | Every documented field is modelled by a wire type or waived with a reason. An endpoint can be implemented while the type behind it drops half of what Favro sends. |
| `transcript` | The live driver reaches the terminal only through the redactor. |
| `live-cover` | The live driver exercises every tool and every option, or waives it with a reason. |
| `parity` | `make check` and `ci.yml` run the same set — every prerequisite, matched by what each recipe runs rather than by target name. |
| `plugin` | The committed `.plugin` manifest against the files the packer will stage. |
| `mcpb` | The committed `.mcpb` manifest against the files the packer will stage, plus the release configuration that gets the bundle hashed, signed and published. |
| `schema-diff` | The tool schemas against the last tag, so a breaking wire change is visible in the pull request. |
| `smoke` | The binary driven over stdio, twice: a conversation, and an abrupt disconnect. |
| `staleness` | The documentation against the code. |

Not gates, and why: `api-diff` needs the network and rewrites a snapshot,
so it is a maintainer command; `mcpb-pack` and `plugin-pack` run during a
release; `release-notes` is called by the release workflow; `fmt-check`
and `precommit` are the fast subset the git hook runs.

### Writing one

Three things, from the standard's preamble, and the third is the one
everybody skips:

1. **Make it a test.** A rule stated in a document is not held by
   anything.
2. **Derive its list from the code**, not from a list typed into the
   checker. A typed list goes stale silently and reads as coverage.
3. **Assert a floor on how much it read.** "Found nothing" and "looked at
   nothing" print the same sentence otherwise. Every gate here reports a
   count, and several of them caught their own bugs on the first run
   because the count came back at zero or one.

Then watch it fail. A gate nobody has watched fail is not yet a gate —
break the thing it checks, confirm the message is the one you would want
at 2am, and put it back.

## Tests

The standard library, not testify: `if got != want { t.Errorf(…) }`.
`depguard` denies `stretchr/testify` and `pmezard/go-difflib` outright.

- Fixtures are **generated, never recorded** from a live response, and
  nothing tenant-specific may appear in one. Refer to test resources by
  role — "a card on a board the token can write to".
- Every mutating tool needs a test proving `dry_run` never reaches
  `RoundTrip`.
- Every registered tool needs a row in `smokeToolInputs`
  (`internal/tools/smoke_test.go`) or the smoke test fails.
- A unit test cannot catch what `docs/architecture.md` §2.1 describes: a
  200 from Favro is not confirmation. A fixture written to match an
  assumption agrees with the assumption — which is exactly how
  `GET /webhooks` shipped through a paginated helper it does not use.

## Verifying against a real organization

```
make live      # drives the built binary against a real organization
```

Every mutating step carries `dry_run`, so it builds and validates
requests and sends none. It prints only through `internal/redact`, and
**a transcript is never committed** — the redactor removes ids and
addresses, not names. See `docs/security.md`.

`favro-mcp doctor` is the quick version: it answers whether credentials
resolve, whether Favro accepts them, and whether the organization binding
is right.

## Commits

- Feature branch off `main`, pull request, green CI. Never commit to
  `main` directly. `main` is protected and there are no bypass actors.
- A phase ends with a commit rather than a modified working tree, so the
  work survives a cleared session and the diff can be reviewed as a unit.
- Say what changed and why. The investigation belongs in the commit body;
  deep context belongs in a comment next to the code.
- Before committing: `make check` green, tests for the new behaviour,
  `/simplify` and `/security-review` over the pending diff with findings
  resolved or explained, a look at the schema diff, and live verification
  where the change touched the wire.

## Releasing

Tags are cut by the maintainer and never proposed automatically.

`CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/):
entries accumulate under `[Unreleased]` and move under a version heading
only in a dedicated release PR. One line per change, keeping the
specifics — tool names, wire keys, versions. Brevity means fewer words,
not less information.

Versioning is [SemVer](https://semver.org). Removing or renaming a tool
input is a breaking change; so is un-registering a tool. If it ships in a
minor, the changelog has to say why.

Pushing a tag runs `.github/workflows/release.yml`: GoReleaser builds five
platform archives — Linux and macOS on both architectures, Windows on
amd64 — plus a macOS universal binary, the `.mcpb` bundle is
packed in the universal binary's post hook, `checksums.txt` covers the
lot, cosign signs it keylessly and `attest-build-provenance` attests it.
`gates release-notes` supplies the release header from `CHANGELOG.md` and
**fails the release if that version has no section**, so a release whose
entry was never written fails at tag time rather than publishing empty
notes.

The signing step cannot be rehearsed locally — it needs an OIDC token
only CI has. Watch the first run after any change to it.
