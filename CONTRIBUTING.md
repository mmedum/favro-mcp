# Contributing to favro-mcp

## Development setup

```
git clone https://github.com/mmedum/favro-mcp.git
cd favro-mcp
make build       # build ./bin/favro-mcp
make ci          # lint + test + vet + vulncheck
make test        # unit tests with race detector
make lint        # golangci-lint + gofumpt diff + goimports
make build       # build ./bin/favro-mcp
```

Go 1.27 required — `go.mod` is the single source of truth. `golangci-lint` must be
v2.13.1 or newer; older builds refuse a config targeting Go 1.27.

## Dogfooding the local build

To point Claude Code at your own build instead of the released plugin, create
an untracked `.mcp.json` at the repo root:

```json
{
  "mcpServers": {
    "favro": {
      "command": "${CLAUDE_PROJECT_DIR:-.}/bin/favro-mcp"
    }
  }
}
```

It is gitignored on purpose: `bin/` is a build output, so a committed config
would reference a command that doesn't exist in a fresh clone. The config
shipped to users is `plugin-template/.mcp.json`, which resolves via
`${CLAUDE_PLUGIN_ROOT}`. Run `make build` first, then reconnect the server and
check with `favro_ping` that the version matches your working tree.

## Workflow

- Work on feature branches. `main` rejects direct pushes.
- Open a PR. All eight checks must be green before merge.
- Merge commits, so each PR stays identifiable in `git log --merges`.
- Before pushing: `make fmt && make lint && make test`.

These are enforced, not conventions — see below.

## Branch protection

`main` is protected by a repository ruleset, active with no bypass actors: the
rules apply to everyone, maintainers included.

- Require a pull request before merging. Approvals: 0 while solo; raise it once a co-maintainer exists.
- Require conversation resolution before merging.
- Required status checks, which must be green *and* up to date with `main`:
  `lint`, `build`, `coverage`, `test-mcp`, `vulncheck`,
  `test-unit (ubuntu-latest)`, `test-unit (macos-latest)`, `test-unit (windows-latest)`.
- Block force-pushes and branch deletion.

Check names are matched literally, so renaming a CI job silently drops its
requirement — and adding one leaves it unrequired until the ruleset is updated
too.

## Tests

- Unit tests run on every PR (`*_test.go`). The favro-package tests use `httptest.NewServer` to exercise URL building, retry, pagination, and JSON decode through the real HTTP transport.
- MCP-protocol tests use the SDK in-memory transport — no subprocess.
- Race detector (`-race`) is on by default.
- Wire-format coverage against the live Favro API is currently done by the maintainer's pre-commit live MCP test, not in CI. A recorded-fixture replay path (`*_integration_test.go`) may land later.

## Dry-run for mutating tools

Every mutating tool accepts `dry_run: bool`. When adding a new write tool, also add a test that uses an HTTP transport which fails the test if `RoundTrip` is ever called during dry-run. The PR template prompts for confirmation that this exists.

## Changelog

`CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

- Categories, in this order: `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security`.
- Newest version first, ISO dates (`YYYY-MM-DD`), link references at the bottom.
- **One line per change.** Say what changed, and why only when it isn't obvious from the change itself. Keep the specifics — tool names, wire keys, CVE ids, versions — but leave the investigation story in the commit body or the PR.
- New entries go under `[Unreleased]`. They only move under a version heading in a release PR.

## Versioning

[SemVer](https://semver.org). The MCP tool schema is the public interface:
renaming or removing a tool input is a breaking change. If one ships in a minor
release, the changelog entry has to say why.

## Releasing

Releases are tag-driven.

1. Open a release PR moving `[Unreleased]` entries under a new version heading with the date, and add its link reference.
2. Merge it.
3. Tag: `git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z`.
4. `release.yml` cross-compiles and publishes the GitHub Release with binaries + `.plugin` attached.

The tag must point at a commit whose `CHANGELOG.md` already names the version —
goreleaser bundles that file into every archive. Pre-release tags
(`v1.0.0-rc.1`) are auto-marked as pre-release.

## License

By contributing, you agree that your contributions will be licensed under Apache-2.0.
