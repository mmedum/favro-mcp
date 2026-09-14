# Security policy

## Reporting a vulnerability

Report privately, never in a public issue: use
[GitHub's private vulnerability reporting](https://github.com/mmedum/favro-mcp/security/advisories/new)
for this repository.

Please include what an attacker can do and the smallest sequence that
shows it. **Do not include anything from a real Favro organization** —
no tokens, organization ids, card ids or names, or addresses. A
description by role is enough to act on.

Expect an acknowledgement within a week. If a fix ships, the advisory
and the changelog will say what was wrong and which versions carried it.

## Supported versions

The latest release. This is a single-maintainer project and there are no
backport branches.

## What this server is, in security terms

It runs on your own machine, holds one Favro API token, and talks to one
Favro organization over HTTPS. There is no service to host and no
multi-tenant boundary to cross.

- **The token** resolves from the environment or your OS keyring, and is
  never logged, never returned by a tool, and never printed by
  `favro-mcp doctor` in either of its modes.
- **Stdout carries MCP JSON-RPC frames and nothing else.** Diagnostics go
  to stderr through `slog`, and no payload is logged.
- **Delete-style tools are not registered** unless
  `FAVRO_ENABLE_DESTRUCTIVE=true`. A host running in an auto-approve mode
  will call an annotated tool without prompting, which is why this is a
  registration decision rather than a per-call one.
- **Every mutating tool takes `dry_run`**, gated in the HTTP client
  rather than in each tool, so a dry run cannot reach the network.
