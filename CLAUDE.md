# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Commands

```
make ci        # lint + vet + test -race + vulncheck — run before every commit
make test      # unit tests with the race detector
make lint      # golangci-lint (needs >= v2.13.1; older builds refuse a Go 1.27 config)
make fmt       # gofumpt + goimports
make build     # ./bin/favro-mcp
make package-plugin   # goreleaser snapshot + favro-mcp.plugin bundle
```

Go 1.27. `go.mod` is the single source of truth — every CI job resolves via
`go-version-file: go.mod`. Don't add a job pinned to an older minor.

## Layout

- `internal/auth` — credential sources (env, OS keyring), `Token`, startup validation.
- `internal/favro` — the REST client. One file per resource, each with its types, request structs and `Client` methods.
- `internal/server` — the MCP layer. `tools_*.go` register tools; `resolver.go` holds the name→ID caches; `full_card.go` does the dereferencing fan-out.
- `internal/cache` — generic TTL cache.
- `cmd/favro-mcp` — stdio entry point and the `auth` subcommands.

## Working with the Favro API

Favro's [REST docs](https://favro.com/developer/) and the live API disagree in
places, and **Favro returns HTTP 200 for a request body it ignores** — a wrong
field name looks exactly like success. This has bitten this repo repeatedly.

- Reads: be tolerant. Accept the documented shape *and* any shape previously observed, preferring the documented one. `Card.CustomFields()`, `Tasklist.Title()` and `Activity.CommonID()` exist for exactly this.
- Writes: send the documented shape, and say in the tool description if it's unverified.
- A 200 is not confirmation. Verify a write by reading the resource back.
- Never put tenant data — org names, card names, ids, emails, sequential ids — in commits, PRs, docs or tool descriptions. Refer to test resources by role.

## Conventions

- Every mutating tool takes `dry_run` and must have a test proving dry-run never reaches `RoundTrip`.
- Every registered tool needs a row in `smokeToolInputs` (`internal/server/smoke_test.go`) or the smoke test fails.
- Pagination is never auto-aggregated. List tools surface `next_page` and require an explicit follow-up.
- Tag tools hard-fail on unknown tag names rather than creating them — typo prevention.
- Single-org: the server binds `FAVRO_ORGANIZATION_ID` at startup and tools never take an `organization_id`.

## Docs and releases

- **CHANGELOG.md** follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/): `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`, in that order, newest version first, ISO dates, link references at the bottom.
- **One line per change.** Say what changed, and why only when it isn't obvious. Investigation detail belongs in the commit body or the PR; deep context belongs in a code comment next to the code. Keep the specifics — tool names, wire keys, CVE ids, versions. Brevity means fewer words, not less information.
- Versioning is [SemVer](https://semver.org). Removing or renaming a tool input is a breaking change; if it ships in a minor, the changelog has to say why.
- Entries accumulate under `[Unreleased]` and are only moved under a version heading in a dedicated release PR. Tags are cut by the maintainer, never proposed automatically.
- Keep `README.md` scannable — the full tool reference lives in `docs/TOOLS.md`.

## Workflow

Feature branch off `main`, PR, green CI. Never commit to `main` directly, and
never push a tag without being asked.
