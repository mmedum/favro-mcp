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
- [ ] Docs updated (`README.md`, tool inventory in `CHANGELOG.md` under `[Unreleased]`).
- [ ] If this is a breaking tool I/O change, the `[Unreleased]` section says so.
