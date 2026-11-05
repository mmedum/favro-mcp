# Releasing

A maintainer releases by pushing a tag, and the tag is the whole release
(D-83). `release.yml` runs the linter and the unit tests, takes the
tag's CHANGELOG section as the release notes, and publishes with
GoReleaser. The archives, their
SBOMs and the `.mcpb` bundle are in the cosign-signed `checksums.txt`
and carry build provenance (D-84). A tag without a pre-release suffix
also gets `favro-mcp.plugin` and the MCP registry entry (D-88, D-92).

The tag waits for the owner's explicit go.

## Before the release commit

- `make check` passes, and CI is green on Linux, macOS and Windows
  (D-69, D-82).
- `make live` has run for anything touching the write path or an API
  response shape, and `go run ./scripts/livefavro -writes` for anything
  touching confirm or sharing (D-73, D-75). `make evals` has run if the
  release changes what a model reads (D-76).
- `/simplify`, `/code-review medium` and `/security-review` have run
  since the previous tag.
- `make api-diff` refetches Favro's reference into
  `testdata/api-surface.json`; it reaches the network, so `make check`
  does not run it. Anything Favro added then fails `make api-coverage`
  or `make api-fields` until its TSV gives it a verdict (D-72).
- `make leaks-history` scans every blob and commit message ever
  committed, which `make check` does not; the history is not rewritten
  (D-50).
- Rehearse the build into `dist/`, publishing nothing, with the
  GoReleaser version `release.yml` pins. It skips signing, which needs
  the workflow's identity, and SBOMs, since syft is installed only in
  CI. `plugin-pack` then builds `favro-mcp.plugin` from `dist/`:

  ```bash
  go run github.com/goreleaser/goreleaser/v2@v2.18.1 release --snapshot --clean --skip=sign,sbom,publish
  go run ./scripts/gates plugin-pack
  ```

  The version must agree in the archives' and the bundle's file names,
  `--version` and `checksums.txt`. A bundle missing from `checksums.txt`
  ships unsigned and looks no different.

## Steps

1. On a topic branch, rename `[Unreleased]` in `CHANGELOG.md` to
   `## [X.Y.Z] - YYYY-MM-DD`, leave an empty `[Unreleased]` above it, and
   add the version's link reference at the bottom.
2. Read `make schema-diff`. A removed tool, a removed input or a newly
   required input fails it, with no way past (D-64), so a break needs the
   owner's decision before it is written. A new major version also
   raises go.mod's `/vN`.
3. Commit as `Release X.Y.Z`, open a pull request, and merge once CI is
   green on that commit (D-87).
4. Tag the merge, annotated, and push the tag on its own:

   ```bash
   git switch main && git pull --ff-only
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

The tag trigger admits only `vX.Y.Z`, with an optional alphanumeric
suffix (D-85), and the release fails when the version has no CHANGELOG
section.

## What no rehearsal reaches

Signing, attestation and the registry publish need the workflow's OIDC
token, so read the first run after any change to the pipeline.

| Fails | State afterward | Recovery |
|---|---|---|
| tests, release notes, cosign or SBOMs | no release | fix it, then release the next patch |
| attestation or `.plugin` upload | release published, incomplete | fix it, then release the next patch |
| registry publish | release fine, no entry | `gh workflow run publish-mcp.yml --ref main -f tag=vX.Y.Z`; do not tag again |

`publish-mcp.yml` fills the entry's hash in from the signed
`checksums.txt`.

## Verify from outside

```bash
cd "$(mktemp -d)" && gh release download vX.Y.Z --repo mmedum/favro-mcp
sha256sum -c checksums.txt --ignore-missing
cosign verify-blob checksums.txt --bundle checksums.txt.bundle \
  --certificate-identity 'https://github.com/mmedum/favro-mcp/.github/workflows/release.yml@refs/tags/vX.Y.Z' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify favro-mcp_X.Y.Z_linux_amd64.tar.gz --repo mmedum/favro-mcp
```

A pass prints little, so read the exit code. Last, outside the
repository, run `go install github.com/mmedum/favro-mcp/v4/cmd/favro-mcp@latest`
as a user would, and check that `favro-mcp --version` names the new
release (D-86).

## A bad release

A bad release is fixed forward.

- Never move or delete a pushed tag. The Go module proxy caches it
  within minutes, and the checksum database records its hash.
- Ship a patch release with the fix.
- In that patch, add `retract vX.Y.Z // why` to `go.mod`, so the go
  command skips the bad version for `@latest`.
- Mark the bad version's CHANGELOG heading `[YANKED]`:
  `## [X.Y.Z] - YYYY-MM-DD [YANKED]`.
