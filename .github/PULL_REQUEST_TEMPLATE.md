<!-- Thanks for contributing! Fill in the sections that apply. -->

## Summary

<!-- 1–3 bullets on what changed and why. -->

## Linked issues

<!-- Closes #N, refs #M, etc. -->

## Output / screenshots

<!-- If a tool's behavior changed, paste a representative input + output. -->

## Checklist

- [ ] `make ci` passes locally (lint + vet + test + vulncheck).
- [ ] New/changed mutating tools accept `dry_run: bool` and have a test that fails if HTTP `RoundTrip` happens during dry-run.
- [ ] No secrets, tokens, or `Authorization` headers in code, tests, fixtures, or logs.
- [ ] New or changed tools have a row in `docs/TOOLS.md`, and one in `smokeToolInputs` (`internal/server/smoke_test.go`).
- [ ] `CHANGELOG.md` has an entry under `[Unreleased]`; `README.md` updated if the overview changed.
- [ ] If this is a breaking tool I/O change, the `[Unreleased]` section says so.
