# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.0.1] — 2026-05-03

### Added
- Repo bootstrap: `go.mod` (Go 1.26 + toolchain), `.gitignore`, `.editorconfig`, `NOTICE`.
- `README.md`, `CONTRIBUTING.md`, this changelog.
- `.golangci.yml` with the production linter set.
- `Makefile` with `lint`, `test`, `build`, `fmt`, `ci`, `tidy` targets.
- GitHub Actions: `ci.yml` (lint + multi-OS unit tests + vulncheck + build), `release.yml` (tag-driven goreleaser pipeline placeholder).
- Dependabot config for Go modules and GitHub Actions.
- PR template with dry-run and docs checkboxes.
