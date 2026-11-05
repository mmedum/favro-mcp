<!-- Thanks for contributing! Fill in the sections that apply. -->

## Summary

<!-- 1–3 bullets on what changed and why. -->

## Linked issues

<!-- Closes #N, refs #M, etc. -->

## Test plan

<!-- How you checked it. If a tool's behavior changed, paste a representative input and output, with nothing from a real organization in it. -->

## Checklist

- [ ] `make check` passes locally.
- [ ] A new write embeds `dryRunInput`; `TestEveryWriteTakesDryRunAndSendsNothing` fails until it does, and checks its dry run sends nothing.
- [ ] A new or changed tool has a row in `docs/TOOLS.md` and one in `smokeToolInputs` (`internal/tools/smoke_test.go`).
- [ ] Nothing from a real Favro organization, and no token or `Authorization` header, in code, tests, fixtures or logs.
- [ ] `CHANGELOG.md` has an entry under `[Unreleased]` for a user-visible change.
- [ ] A change to the tool surface is named in a `SCHEMA-CHANGE:` or `BREAKING CHANGE:` commit footer.
- [ ] Anything touching the write path or an API response shape has had a `make live` run.
