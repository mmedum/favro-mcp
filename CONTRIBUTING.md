# Contributing to favro-mcp

## Development setup

```
git clone https://github.com/mmedum/favro-mcp.git
cd favro-mcp
make ci          # lint + test + vet + vulncheck
make test        # unit tests with race detector
make lint        # golangci-lint + gofumpt diff + goimports
make build       # build ./bin/favro-mcp
```

Go 1.26+ required. The `toolchain` directive auto-bumps you to the matching toolchain.

## Workflow

- Work on feature branches; `main` is protected.
- Open a PR — direct pushes to `main` are blocked.
- CI must be green before merge: `lint`, `test-unit` on ubuntu/macos/windows, `test-mcp`, `vulncheck`, `build`.
- Squash or rebase merges only (linear history).
- Before pushing: `make fmt && make lint && make test`.

## Branch protection (one-time repo setup)

On `main`:

- Require a pull request before merging.
- Required approvals: 0 if solo maintainer; 1 once a co-maintainer exists.
- Required status checks (must be green and up-to-date with `main`):
  `lint`, `test-unit (ubuntu-latest)`, `test-unit (macos-latest)`, `test-unit (windows-latest)`,
  `test-mcp`, `vulncheck`, `build`.
- Require conversation resolution before merging.
- Require linear history.
- Lock force-push.
- Allow auto-merge for PRs whose checks pass (Dependabot patch + minor only).

## Tests

- Unit tests run on every PR (`*_test.go`).
- Integration tests are gated by both a `*_integration_test.go` filename suffix and a `FAVRO_INTEGRATION=1` env var. They run on push to `main` and on same-repo PRs labeled `integration-ok`. Never on fork PRs.
- MCP-protocol tests use the SDK in-memory transport — no subprocess.
- Race detector (`-race`) is on by default.

## Dry-run for mutating tools

Every mutating tool accepts `dry_run: bool`. When adding a new write tool, also add a test that uses an HTTP transport which fails the test if `RoundTrip` is ever called during dry-run. The PR template prompts for confirmation that this exists.

## Releasing

Releases are tag-driven. After a phase exits its gate:

1. Open a release PR moving "Unreleased" entries in `CHANGELOG.md` under a new heading with the date.
2. Merge the release PR.
3. Tag: `git tag -a vX.Y.Z -m "vX.Y.Z" && git push origin vX.Y.Z`.
4. The `release.yml` workflow rebuilds, cross-compiles, and publishes the GitHub Release with binaries + `.plugin` attached.

Pre-release tags (`v1.0.0-rc.1`) are auto-marked as pre-release by goreleaser.

## License

By contributing, you agree that your contributions will be licensed under Apache-2.0.
