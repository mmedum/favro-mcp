# Architecture — favro-mcp

**Status, 2026-09-13.** Released: v1.1.2. The server's own feature phases
(0–9) are complete and shipped. Of the alignment programme in §16, phases
**A0, A1 and A2 are done and unreleased**; A3–A8 are not started. Where a
sentence below describes something that does not exist, it says so and
names the phase that builds it.

This document is the design, the decided constraints, the evidence and
the phase plan. Read it before changing the tool surface, the error
vocabulary, the write path, or the package layout.

It is written against the shared standard for our Go MCP servers
(`~/.claude/mcp-server-standard.md`, adopted here 2026-09-13), which was
written out of google-docs-mcp and is run by four sibling servers:
google-chat-mcp, google-docs-mcp, google-drive-mcp and google-sheets-mcp.
Section numbering mirrors theirs so the five are cross-referenceable.
Where this server deviates, §17b names the deviation and why.

**Every rule here is written as though stating it were enough. None of
them is.** The standard's preamble is the load-bearing paragraph of this
whole document: for every rule adopted, make it a test, derive the list
from the code rather than typing it out, and have the checker assert a
floor on how much it read — zero findings and zero inputs otherwise print
the same sentence. This repository has already produced its own instance
of that failure: CLAUDE.md has said "every registered tool needs a row in
`smokeToolInputs` or the smoke test fails" since Phase 3, and that one IS
held by a test — while "never put tenant data in commits, PRs, docs or
tool descriptions", the loudest rule in the repository, is held by
nothing at all.

## 1. Mission and scope

A Go MCP server exposing Favro's REST API to a model over stdio, as 83
typed tools (measured 2026-09-13; the standard's §7b says re-measure or date it).

It is not a thin REST wrapper. Favro's API is id-shaped in a way a model
cannot navigate: a card is addressed by `cardId` in one widget and by
`cardCommonId` across widgets, columns repeat their names across boards,
tags and custom fields are org-global with no name index, and there is no
full-text search at all. A wrapper that exposed those endpoints one to
one would spend a conversation's entire rate-limit budget turning names
into ids. So the surface carries three layers the API does not have:
`favro_resolve_*` (name → ranked id candidates, cached), `favro_search_cards`
(client-side full-text over a scoped corpus), and `favro_get_card_full`
(one card with every id dereferenced, saving 4–7 round-trips).

### Non-goals

- **Multi-org.** The server binds one `FAVRO_ORGANIZATION_ID` at startup
  and no tool takes an `organization_id`. Favro's rate limit is per
  organization; a server that could switch orgs mid-session would make
  the budget unattributable, and the id is a deployment fact rather than
  a per-call one.
- **SCIM provisioning.** `/scim/v1` and `/scim/v2` are user and group
  lifecycle for an IdP, authenticated differently and used by directory
  software rather than by a person asking about a card. Out by category,
  recorded as one row rather than twenty-two (§16, phase A5).
- **Creating organizations.** `POST /organizations` and
  `PUT /organizations/{id}` exist; a card-work server that can create an
  organization is a capability nobody asked for and a blast radius
  nobody wants.
- **Auto-aggregating pagination.** List tools surface `next_page` and
  require an explicit follow-up, so a model can stop early rather than
  discovering the budget is gone.

## 2. Hard constraints from the platform

These are properties of Favro, not choices, and every one of them has
cost this repository something already.

1. **Favro returns HTTP 200 for a request body it ignores.** A wrong
   field name looks exactly like success. This is the single most
   important fact about the platform and it is why the standard's §7 — "verify against
   the discovery document" — does not transfer: there is no discovery
   document, and the reference page has been observed to disagree with
   the live API. A 200 is not confirmation; a read-back is.
2. **The REST documentation and the live API disagree in places.** Reads
   are therefore tolerant — accept the documented shape *and* any shape
   previously observed live, preferring the documented one
   (`Card.CustomFields()`, `Tasklist.Title()`, `Activity.CommonID()`
   exist for exactly this). Writes send the documented shape and say in
   the tool description when it is unverified.
3. **A missing endpoint answers with the SPA fallback page, not a 404.**
   `decodeJSONLenient` reports status, content-type and a body prefix so
   one round-trip is enough to diagnose it.
4. **Rate limits are per organization and tier-based** (Lite ~100/hr,
   Standard ~1000/hr, Enterprise ~10000/hr). Favro stalls responses
   before it rejects them: a non-zero `X-RateLimit-Delay` is the early
   warning, and 429 arrives once the needed stall would exceed 10s. One
   misbehaving agent exhausts the quota for every human in the org.
5. **Pagination is backend-affine.** A paginated read must carry
   `X-Favro-Backend-Identifier` from the first response, which is why
   every list tool returns a `request_id` the caller has to pass back.
6. **Two ids per card.** `cardId` is per-widget; `cardCommonId` is
   cross-widget. `GET /cards/{id}` 403s on a common id. Different
   endpoints require different ones — `/comments` and `/tasklists` want
   the common id, `/cards/{id}/activities` wants the per-widget id — and
   getting it wrong is a 403 rather than a helpful error.
7. **Custom fields are org-global but per-widget enabled.** A write to a
   field the widget has not enabled is accepted and ignored (see 1).
8. **Some deletes are not deletes.** `DELETE /cards/{id}` removes one
   widget's instance unless `everywhere` is set; `DELETE` on a
   collection does not cascade to widgets, which are left orphaned.

## 3. Requirements distilled from the siblings' failures

The four sibling servers paid for these; this one should not pay again.

- A gate that runs is worth more than a rule that is written. Five rules
  in the standard were true on paper and false in the code at the same
  time, and none was caught by reading.
- A completeness claim decays the moment the surface grows. Anything
  claiming to cover "every tool" — a smoke test, a live driver, a leak
  scan — must derive its list from the code and assert a floor on how
  much it read.
- A leak gate cannot be patterns alone when the payload is ordinary
  words. A card name has no shape a scanner can match. Fixtures are
  generated, never recorded, and a live driver reads only what it wrote.
- A manual gate nobody runs is a gate that never fires. Keep the fetch
  manual, commit what it fetched, and let `check` read the file.
- What `make check` runs and what CI runs are two lists in two files,
  and you only ever edit one of them. Assert they agree.
- A number in prose is a claim about the list beside it.
- Documentation that a person must copy into a system's configuration is
  an input, not a description.

## 4. Core design bets

These are the decisions the existing code already runs on. They are
recorded because they are not re-derivable from reading it, and because
§17b has to be able to point at them.

### 4.1 Names in, ids out, cached in between

The model speaks names; Favro speaks ids. `internal/server/resolver.go`
is the one place that bridges the two, with per-resource TTLs — 5 minutes
for slow-changing org metadata (tags, users, custom fields, groups), 60
seconds for things people add mid-session (collections, widgets,
columns). Every write tool invalidates the cache its write invalidated.
One resolver per process: per-tool resolvers would each cold-start their
own cache and burn the budget in parallel.

### 4.2 Every mutating tool takes `dry_run`, and the binary can force it

`dry_run: true` returns the would-be request — method, URL, redacted
headers, body — plus a predicted state change, without contacting Favro.
`--dry-run` on the binary forces it process-wide for sandboxed use. The
gate lives in the client (`shouldDryRun`), not in each tool, so a tool
cannot forget it; every mutating tool has a test proving dry-run never
reaches `RoundTrip`.

### 4.3 Tag tools hard-fail on unknown names

`favro_add_tag_to_card` refuses a name it cannot match rather than
creating the tag. Favro's own `addTags` takes names and creates unknown
ones, which turns a typo into a permanent org-global tag. Creating one is
`favro_create_tag`, explicitly.

### 4.4 Pagination is never auto-aggregated

One tool call is one page. §2.5's backend affinity makes a hidden
aggregation loop expensive and unattributable, and the model stopping
early is the common case.

### 4.5 Reads are tolerant, writes are documented

§2.2, as code. The accessors that accept two shapes are the tolerance;
the tool description that says "not verified against a live tenant" is
the honesty about the other direction.

### 4.6 Own wire types, raw REST

`internal/favro` hand-writes the request and response structs. There is
no generated Favro client to import and there will not be one.

### 4.7 A 200 is not confirmation (the rule that outranks the others)

Verification is a read-back, live, against a real organization. This is
why §15 exists and why phase A6 builds a live driver rather than
treating unit tests as the end of the argument.

## 5. Module layout

Current, and the target the alignment phases move it to. The target
mirrors the siblings; the names are Favro's.

```
cmd/favro-mcp/            main: server default, auth subcommands, --version,
                          --dry-run, --dump-schemas (A1), doctor (A7)
internal/config/          FAVRO_* env with flags bound to the same names,
                          validated at start                            (A3)
internal/auth/            credential resolution: env → OS keyring; Token.Apply
internal/favro/           wire types only: Card, Widget, Column, …. No deps
internal/favroapi/        the REST client: retry, rate-limit observation,
                          dry-run gate, pagination, redacted logging,
                          errors. No MCP imports                        (A3)
internal/cache/           the TTL cache the resolver runs on
internal/service/         orchestration: resolution, search, full-card
                          fan-out, description editing, write policy    (A3)
internal/render/          the readable half of every result, never the same
                          bytes as the structured half; and the closed
                          error vocabulary
internal/tools/           the MCP surface, one file per area            (A3)
internal/server/          SDK wiring; schema dump through an in-memory session
internal/redact/          the one redactor the live driver prints through (A6)
internal/livecover/       what "the driver covers the surface" means, so the
                          gate and the driver cannot disagree           (A6)
internal/version/         the build stamp
scripts/gates/            this repository's own checks, as Go
scripts/livefavro/        drives the built binary against a real org    (A6)
scripts/evals/            drives a model through the tools and scores it (A6)
testdata/                 synthetic fixtures, goldens, the API snapshot
docs/
```

Dependency direction runs one way: `tools` → `service` → `favroapi` →
`favro`. Nothing under `internal/favroapi` imports MCP; `internal/favro`
imports nothing. depguard holds it (A3).

**Today** everything from `service`, `render` and `tools` lives in
`internal/server` (85 files), and `internal/favro` is both the wire types
and the client. That is the one structural difference from the siblings,
and A3 is the phase that removes it.

**One language.** Everything the repository runs on itself is Go. The two
shell scripts that used to live in `scripts/` are gone (A1): a shell
script is held to no gofmt, vet, lint or test, `make check` runs on the
Windows runner where bash is a dependency rather than a given, and a
script that parses JSON with `sed` is how a quote ends up inside a
string. The plugin packer was the clearest case — it read two JSON
documents with `jq` and wrote a third — and porting it moved the
launcher's platform table into the same Go file the gate reads, which is
what closed the hole §12 describes. The one shell file left is
`.githooks/pre-commit`: three lines of git plumbing that exec a Go
program.

Dependencies, all pinned: `modelcontextprotocol/go-sdk` v1.7.0 (with
`google/jsonschema-go`), `zalando/go-keyring` v0.2.8, `golang.org/x/sync`,
`golang.org/x/term`. `stretchr/testify` and `pmezard/go-difflib` are test
dependencies the siblings do not have; A4 removes them.

Toolchain: Go 1.27 (`go.mod` is the single source of truth — every CI job
resolves via `go-version-file: go.mod`, and there is no N-1 matrix entry).

## 6. Addressing and error classes

### 6.1 How the model addresses things

By name through `favro_resolve_*`, or by an id Favro gave it. The server
hands out no handles of its own and does no index arithmetic, so the
standard's "the model never sees internal indices" is satisfied by the
shape of the API rather than by anything this server does.

The one place the distinction bites is §2.6's two card ids. Every tool
that takes one says in its description which it wants and why, because a
wrong choice is a 403 rather than a validation error.

### 6.2 Error classes

Every error a tool returns is rendered `[class] actionable message`,
from a closed vocabulary. The class is derived from the error's own
type — `Classify`, in `internal/render`, walks the chain with
`errors.As` — and never from matching on its text.

The `classes` gate holds this table and `internal/render/class.go` to
each other from both sides. A class the code declares and this table
does not name fails. A class this table names and no code returns fails
too, and that is the side that actually rots: to somebody deciding how
to handle an error, a documented class nothing emits is
indistinguishable from one that simply has not happened yet.

| Class | When | What the caller does about it |
|---|---|---|
| `invalid` | the request as given cannot be served: a missing argument, two mutually exclusive ones, a value out of range. Also Favro's 400 and 422 | change the arguments |
| `not_found` | a resource, or a name, that is not there — Favro's 404, a tag name no tag carries, a `find` that matched nothing | stop looking, or resolve a name first |
| `auth` | Favro's 401: the credentials themselves | nothing; a human has to fix it |
| `conflict` | a state collision — the resource moved under the caller | re-read, then retry |
| `unavailable` | Favro, or the network to it, failing: 5xx after the retry budget, a transport error, a cancelled context | retry later; the arguments are not the problem |
| `unsupported` | something this server will not do, as opposed to something that failed — a custom-field type `favro_set_card_custom_field` cannot write, Favro's 405 and 501 | do not try this again |
| `forbidden` | Favro's 403, which it uses both for "no permission" and for "exists but not visible to this token" (§2.6). Collapsing it into `not_found` would tell a model to stop looking for something that is there | try a different route to the resource, or ask for access |
| `rate_limited` | 429, carrying `retry_after_seconds` in the message. Distinct from `unavailable` because the correct response is to wait a named duration rather than to retry | wait the named number of seconds |
| `ambiguous` | a name matching several candidates | choose one; do not retry the name |

The fallback for an error that names no class is `invalid`, and that is
measured rather than neutral: every error this server's own layer
raises without a sentinel is an argument the caller got wrong — "pass
exactly one of", "is required", "must be between". A server bug
arriving there would be mislabelled, which is the trade.

Two tests hold the edges of that fallback, and it is worth being exact
about which edges, because neither covers the third.
`TestEverySentinelIsClassified` parses `internal/server` for
package-level sentinels and requires each to name its class.
`TestEveryFavroErrorTypeIsClassified` reads the types declared in
`internal/favro/errors.go` and requires `Classify` to have a case for
each, so an eighth typed error cannot land on the fallback in silence.
What neither covers is an inline `fmt.Errorf` in a handler: those are
the dozen or so argument errors above, they are all genuinely
`invalid`, and nothing would fail if a future one were not. Making a
new error a sentinel is what buys the check.

The asymmetry between those two tests is itself a note for A3. A
sentinel names its own class; the Favro error types have theirs read
off them from outside, because `Class` lives in `internal/render`,
which imports `internal/favro` and so cannot be imported by it. The
uniform version puts `Class` in a leaf package and gives each typed
error an `ErrorClass()`, at which point `Classify` collapses to one
`errors.As`. That is a layout change, and A3 is where layout is
decided.

**`unverified` was proposed and rejected.** §17's first open decision
asked whether the state §2.1 describes — a write whose 200 this server
does not trust — earns a class, and said to decide it by trying to
write the message. The message is
`[unverified] Favro returned 200 and the write was not read back`, and
writing it settles it two ways.

It is rendered on an error result, which sets `IsError`. That tells the
caller the call failed, when the write may well have landed; a caller
that retries on `IsError` posts the comment twice or creates the card
twice. The class would cause the damage it was meant to warn about.

And it carries nothing per call. This server does not read back, so the
flag would be constant for a given tool — and a constant per tool is a
tool description, which is where it already lives (§7.4 names the three
writes that have this property, and each tool says so). If read-back
ever lands, the honest signal is a field on a *successful* result, not
a class on a failed one.

## 7. Reading and writing

### 7.1 Reading a card

Three ways in, and a tool that takes one of them takes exactly one:
`card_id` (per-widget), `card_common_id` (cross-widget), or
`sequential_id` (the integer inside a human reference like the ones
people paste from the UI). `GET /cards/{id}` answers 403 for a common id
rather than redirecting, so `favro_list_cards` with the
`card_common_id` filter is the path for that form.

`favro_get_card_full` is the one read worth describing. A raw card is a
bag of ids — tags, assignees, widget, column, parent collections, custom
fields — and a model that gets one spends four to seven calls turning it
into something it can talk about. The tool fans those lookups out in
parallel through the resolver's caches and returns names beside the ids,
including per-type display values for every custom-field type Favro
publishes. An unrecognised type passes through with
`dereferenced: false` rather than being dropped or guessed at: a new
Favro field type should degrade to "here is the raw value", not to a
wrong rendering.

Descriptions are read with `descriptionFormat=markdown`, so what comes
back is what an edit has to be written against.

### 7.2 Search

Favro has no full-text search, so `favro_search_cards` builds one:
fetch a scoped corpus, strip markdown, lowercase, score name and body
separately (name phrase +1.0, name token overlap up to +0.6, body phrase
+0.5, body token overlap up to +0.5), cache the prepared corpus for 60
seconds per scope.

Exactly one of `widget_common_id` or `collection_id` is required, and
that is a platform constraint rather than a design choice: `/cards`
rejects an unfiltered listing, so there is no org-wide search to offer.
The tool says so in its description and points at the resolvers, because
a model that cannot see why it is being refused will retry the same call.

### 7.3 Editing a description without replacing it

`PUT /cards/{id}` takes a whole description, so the naive tool is
"replace the body" and the naive failure is a model rewriting a card it
only meant to add a line to. Three tools edit in place instead —
`favro_append_card_description`, `favro_prepend_card_description`,
`favro_replace_in_card_description` — each returning `{old, new,
unified_diff}` so the change is auditable in the transcript.

Two guards matter. `replace_in` defaults to `count: 1`, so a common
substring does not silently rewrite every occurrence; and it refuses to
PUT at all when `find` matches nothing, because a no-op write that
returns 200 is indistinguishable from a successful one (§2.1).

### 7.4 Writes that are not what they appear to be

Each of these is a place where Favro's answer and Favro's behaviour come
apart, and each is why a tool description carries a warning:

- **Custom fields** are org-global but enabled per widget. Writing to a
  field the widget has not enabled is accepted and ignored.
- **Attachment removal** has no endpoint. It rides on `removeAttachments`
  in `PUT /cards/{id}`, matched by file URL rather than display name, and
  returns 200 whether or not anything matched. Worse, Favro re-mints
  `fileURL` on every read with a fresh presigned signature, so the value a
  caller just read back never equals the one Favro stored; v1.1.1 strips
  the presigned query before sending. Two releases got this wrong before
  the read-back explained why.
- **Group membership**: the docs describe add/remove deltas with a
  per-entry `delete` flag; a live test observed whole-list replacement.
  The client sends the full intended list, which is correct under either
  reading.
- **Collection sharing** reads under `sharedToUsers` and writes under
  `shareToUsers`. Posting the read-shaped key is accepted and drops the
  invitations.
- **Deletes are scoped.** A card delete removes one widget's instance
  unless `everywhere` is set; a collection delete does not cascade, and
  can orphan widgets; a column delete is refused with 400 while cards
  remain on it.

### 7.5 Uploads

Attachments are a raw-bytes POST to `/cards/{id}/attachments` or
`/comments/{id}/attachments`, from a local absolute path, capped locally
at 8 MiB. Favro echoes the created attachment rather than the updated
card — verified live, and worth stating because the obvious assumption
is the other one.


## 8. Tool surface

83 tools (2026-09-13). The full reference is `docs/TOOLS.md`; the README
keeps only the five worth reaching for first. Conventions that hold
across all of them:

- Every mutating tool takes `dry_run` and has a test proving dry-run
  never reaches `RoundTrip`.
- Every registered tool has a row in `smokeToolInputs`
  (`internal/server/smoke_test.go`) or the smoke test fails. This is the
  repository's one existing instance of the standard's "derive the list
  from the code" rule, and it predates the standard.
- List tools surface `next_page` and never aggregate. Page numbers on
  this surface are 1-indexed; Favro's are 0-indexed, and `favroPage`
  converts at the boundary. The two used to be the same number while the
  schema said "1-indexed", so a caller that believed the schema and
  asked for page 1 was served the second page and never saw the first —
  a valid page of real results with rows silently missing, which is why
  no test caught it. The conversion lives in the MCP layer because
  `internal/favro` is a faithful client of an API whose page is
  0-indexed.
- Where two tools overlap, each description names the other and says
  when to choose it (`favro_update_card` vs `favro_move_card` /
  `favro_archive_card`; `favro_add_tag_to_card` vs `favro_update_card`
  with `add_tag_ids`).
- **Destructive tools are registered only when `FAVRO_ENABLE_DESTRUCTIVE=true`.**
  `dry_run` used to be the only guard, which the standard rejects for a
  reason it verified live: a host in an auto-approve permission mode runs
  an annotated tool without prompting, and the spec says clients treat
  tool annotations as untrusted. Client-side approval is not a safety
  layer; the tool that cannot run unattended is the one that is not
  registered. This removes tools from the default surface, so it is a
  breaking change and the changelog says so.

  Which tools those are is read from `DestructiveHint` at the moment of
  registration, in `addTool` — there is no second list of destructive
  tool names, because the tool that would be missing from it is the one
  added by somebody who did not know it existed.
  `TestDestructiveToolsAreOptIn` derives the same set from the
  annotations on the live surface and requires the default surface to
  differ from the full one by exactly it. The count is deliberately not
  written here: this section said "twelve" while the code annotated
  thirteen, which is the §7b failure in miniature.

## 9. Confidentiality, security, safety

**What may never enter the repository:** organization names or ids, card
names, ids or sequential ids, user names or email addresses, tokens, and
any content from a real Favro tenant — in code, fixtures, tests, tool
descriptions, commit messages, PR bodies, the changelog or the docs. Test
resources are referred to by role. Fixtures are generated, never
recorded.

This rule is older than this document and was stated in CLAUDE.md from
Phase 3 with **nothing holding it**. A1 added the `leaks` gate over the
working tree and `leaks history` over every blob and commit message; A6
adds the `transcript` gate so the live driver cannot print except through
the one redactor. Per the standard, the gate cannot be patterns alone —
an organization name is ordinary words — so it anchors on shapes the
server's own values cannot take: an `@` with a dot-suffixed domain, a
24-hex id of the length Favro mints, a literal keyword immediately before
an id, a link into the app. Its exemption lists are asserted rather than
trusted, and that assertion deleted five of them on the day it was
written (§18).

**What the history already holds.** `gates leaks history` was run for
the first time in A1, over every blob and commit message. It reports
three classes, none of them tenant data and none of them fixable without
rewriting a public history that has released tags on it:

- Session transcripts under `.entire/` (three blobs, ~725 KB), left by a
  session-recording tool that was removed in the commit before this
  programme began. Checked: zero Favro ids, zero app links, zero
  `cardCommonId` or `organizationId` mentions, zero credentials — but
  they carry the maintainer's address, which git authorship publishes on
  every commit anyway.
- The old test fixture's address at a registrable domain, in blobs
  predating A1's fix.
- `SHA-256` in older changelog entries, which is the card-reference <!-- leakcheck:allow -->
  rule's known false-positive class rather than a finding — and the
  clearest illustration of why that rule needs a list at all.

The working-tree scan is the one in `make check`; the history scan stays
manual, because its findings are facts about the past rather than things
a commit can fix. What it is for is knowing.

**Logging.** Method, endpoint shape, query parameter *names*, attempt
and outcome at debug; never a payload, and never a value that
identifies the subject. The Authorization header is redacted at the one
place headers are rendered, shared by the debug line and the dry-run
record, and so is `organizationId`.

§9 knew about two breaches of that rule when A2 started. It found two
more while fixing them, and the way each of the last two surfaced is
the useful part.

The two known ones. The debug line logged `req.URL.RawQuery`, and
Favro's query strings carry `cardCommonId`, `widgetCommonId` and
`sequentialId`, so a debug log reconstructed which cards a session
touched; it logs the parameter names now. The startup line logged
`organization_id` in full at INFO, naming the tenant in the first line
of every session — found by running the server against a real
organization during A1's live check, which is the kind of thing only a
live run shows.

**The third was found by the test, not by reading.** `organizationId` is
a header, `Token.Apply` sets it on every request, and `redactHeaders`
redacted only `Authorization`, so the header map reprinted the
organization id on every debug line. An existing test asserted it passed
through unredacted: the behaviour was not an oversight, it was pinned.
That is the argument for `TestDebugLogNeverCarriesTheSubject` asserting
over everything captured rather than over the line under suspicion, and
for capturing at `LevelDebug`, since a capture at the default level
passes against the broken code.

**The fourth was found by the security review, and the test had a hole
that let it through.** `req.URL.Path` was still logged whole, and every
get-one endpoint is `/cards/{cardId}` — the same leak as the query
string, in the other half of the same URL. The test missed it because it
drove a list endpoint, where the ids are all in the query. It now drives
a get-one call as well, and the path is logged as its shape:
`redactPathIDs` keeps a segment only if it reads as part of an endpoint
— lowercase alphanumeric, shorter than the 24 characters every Favro id
has — and anything else becomes `{id}` rather than being printed.

Its first version was stricter, rejecting any digit, and a live run
caught what the unit test could not: the production path is
`/api/v1/cards/{id}`, so `v1` was being redacted as an identifier. The
test talks to an `httptest` server whose base URL has no version
segment, so it was asserting against a path shape that does not occur.
There is now a row for the real one.

The lesson §9 records is not about any of the four. It is that "no
forbidden value reaches a log" is a claim about every call site and
every level, and a test that exercises one call site proves it for one
call site.

**Safety.** §8's destructive-tool gate; §4.2's dry-run; §4.3's hard-fail
on unknown tag names. Reads are budgeted only by pagination.

## 10. Auth, config, process model

Favro authenticates with **HTTP Basic: user email + API token**. There is
no OAuth for its REST API, so the standard's §3b — loopback IP literal,
PKCE, no OOB flow — describes a flow this platform does not offer. §17b
records it as a deviation with that reason rather than leaving a reader
to wonder.

What does carry over is the rest of the credential handling: the token
lives in the OS keyring (macOS Keychain, Windows Credential Manager,
Linux Secret Service) with an environment-variable override, resolved
first-complete-triple-wins, and `favro-mcp auth which` says which source
won. The token is never printed, never logged, and never reaches a
dry-run record.

API tokens are user-scoped, so a team install should generate one from a
dedicated service-style Favro user with minimum permissions rather than
from a person's account. That sentence belongs in the README, and does.

Config is read from `FAVRO_USER_EMAIL`, `FAVRO_API_TOKEN`,
`FAVRO_ORGANIZATION_ID`, `FAVRO_LOG_LEVEL` and
`FAVRO_MCP_SKIP_VALIDATE` and `FAVRO_ENABLE_DESTRUCTIVE`. A3 moves the
reading and validating into `internal/config` with flags bound to the
same names, as the siblings do.

Stdout carries JSON-RPC frames only; logs go to stderr through `slog`.

## 11. Reliability

- **Retry**: 429 once, honouring `Retry-After` capped at 30s; 5xx three
  attempts on a 250ms / 1s / 4s schedule. A single request is capped at
  30s including retries.
- **Rate-limit observation**: every response's `X-RateLimit-*` headers
  are recorded, and `favro_rate_limit_status` reports the most recent
  snapshot without spending a call.
- **The SDK's disconnect trap** (standard §11), which this server had:
  `errors.Is(err, io.EOF)` does not catch the end of a stdio session. The
  SDK reports a closed connection as JSON-RPC −32004 with the EOF only as
  message text, so until A1 this binary exited 1 every time a host closed
  the pipe — which every host logs as a crash. `cleanDisconnect` now
  matches the code rather than the text, and the smoke gate closes stdin
  the moment the last message is written, because a run that sleeps first
  will never see the bug.

## 12. Distribution

Today: six platform archives plus a `favro-mcp.plugin` bundle (a Cowork /
Claude Code plugin: multi-arch binaries and a launcher, with a
`.mcp.json` pointing at `${CLAUDE_PLUGIN_ROOT}`), built by goreleaser and
a shell script, published from a tag.

What the standard asks for and A7 adds: a `.mcpb` Claude Desktop bundle
packed **in Go** in the universal binary's post hook, named in
`checksum.extra_files` and `release.extra_files` so it is hashed and
signed rather than merely uploaded; an SBOM per archive; a keyless cosign
signature over the checksums; `actions/attest-build-provenance`; and
`mod_timestamp: {{ .CommitTimestamp }}` so a rebuild is byte-identical.
The `.plugin` bundle stays — it is how this server is actually installed
— and the manifest gate validates both against the staged tree rather
than against a schema, because the failures that matter are referential:
an `entry_point` nothing stages, a `platform_overrides` entry no declared
platform can reach, a launcher choosing between binary names no manifest
mentions.

`go install` applies no ldflags, so the version falls back to
`debug.ReadBuildInfo()` rather than reporting `dev` (A7 verifies this;
the standard lists it as a thing that bites).

## 13. Testing

- Unit tests with the race detector, against `httptest` fakes. A
  per-package coverage floor of 80% (A1 — today coverage is measured and
  uploaded but nothing fails below a floor).
- The MCP surface is exercised in-memory over the SDK's transport
  (`TestMCP*`), and over real stdio by the `smoke` gate (A1).
- `stretchr/testify` today; stdlib `testing` after A4, matching the
  siblings and removing two dependencies.
- **Live verification before every phase commit.** Build, reconnect the
  MCP server, exercise the new tools against a real organization, fix
  what the wire contract actually is, then commit. Unit tests cannot
  catch what §2.1 describes.
- Agent evals (A6) score whether a model can complete a task through the
  tools at all — the failure a unit test cannot see is a tool the model
  never finds.

## 14. Confirmed decisions and their consequences

| Decision | Consequence |
|---|---|
| Single organization, bound at startup | No tool takes `organization_id`; rate-limit budget is attributable; multi-org needs a second process |
| Resolution caches are process-wide | A write must invalidate the right cache or the next read is stale; `force_refresh` exists on every list and resolve tool as the escape hatch |
| Pagination never auto-aggregates | Every list tool returns `next_page` + `request_id`, and callers must pass both back |
| Dry-run gate lives in the client | A tool cannot forget it; the test proving it is per-tool |
| Tag tools hard-fail unknown names | A typo cannot create an org-global tag; creating one is explicit |
| Destructive tools behind an env flag | The delete-style tools leave the default surface; breaking change; changelog says why. Which ones is read from the annotation, never from a list |
| Stdlib tests (A4) | Two dependencies gone; thousands of assertion lines rewritten once |
| API surface snapshot committed (A5) | CI holds the completeness claim offline; the fetch stays manual and is named in the release checklist |

## 15. What must be verified live

Not yet verified against a real organization, and each one is a place
§2.1 can be hiding. A5 and A6 close them.

1. Every per-type custom-field write shape in `favro_set_card_custom_field`
   — the README already says these are documented-but-unconfirmed.
2. `favro_remove_attachment` — see §7.4. The presigned-query strip
   shipped in v1.1.1 is the third attempt at this write and the first
   that can be right in principle; nothing has yet confirmed a removal
   live.
3. The webhook endpoints A5 adds, all three.
4. Whether `PUT /groups/{id}` replaces the member list or applies a
   delta. The docs say delta with a per-entry `delete` flag; a live test
   observed whole-list replacement. The client sends the full list, which
   is correct under either reading, but the disagreement is unresolved.
5. Whether any documented field this client does not model is accepted
   live — the `api-fields` gate's list of unmodelled fields is a
   worklist, not a verdict.

## 16. Delivery phases

The server's own phases 0–9 are done and shipped (v1.1.2). What follows
is the alignment programme. Each is a branch, a PR and green CI; each
ends with the working tree modified and waits for an explicit "start
phase An" before the next begins.

- **A0 — this document**, plus CLAUDE.md rewritten to the sibling shape.
  Docs only.
- **A1 — gates and supply chain. Done.** `scripts/gates` as one Go binary
  with a registry; `--dump-schemas` and a committed `schemas.json`; the
  coverage floor (80%, per package), `leaks` (+ `history`), `pins`,
  `parity`, `plugin`, `schema-diff`, `smoke` and `staleness`; both shell
  scripts ported to Go; every action pinned to a SHA with the version in
  a trailing comment; gitleaks, go-licenses and CodeQL in CI; tool
  versions pinned; `make check` and `ci.yml` asserted to run the same
  set. **Merging it needs one repository setting changed by hand:** the
  ruleset on `main` requires eight status checks by name — `lint`,
  `build`, `coverage`, `test-mcp`, `vulncheck` and three `test-unit (os)`
  — and this phase renames the jobs that produce them to `test (os)`,
  `gates`, `cover`, `static`, `secrets` and `coverage`. Seven of the
  eight required names will never appear again, so a pull request would
  wait for them forever. Update the required list to the new names as
  part of merging, not after. It found defects in the repository and in
  itself while it was being built — an ordinary disconnect exiting non-zero, a fixture
  address at a registrable domain, two environment variables the README
  never documented, and three claims that nothing held — all fixed here
  and recorded in §18.

- **A2 — error classes and the result shape. Done.** The closed
  vocabulary of §6.2, with the `classes` gate holding it and the document
  to each other from both sides; `internal/render` so `content` and
  `structuredContent` stop being the same bytes, wired in at `addTool`
  rather than across 83 handlers; `FAVRO_ENABLE_DESTRUCTIVE`, gated from
  the annotation so there is no second list; and the logging leaks in §9
  removed with the test that would have caught them — which found a
  third one nobody had recorded, the `organizationId` header, pinned by
  an existing assertion. §17's first open decision is settled in §6.2:
  `unverified` was written out as a message and rejected.
- **A3 — layout.** `internal/server` split into `internal/tools` /
  `internal/service` / `internal/server`; `internal/favro` split into wire
  types and `internal/favroapi`;
  `internal/config` added; depguard holds the dependency direction.
- **A4 — stdlib tests.** testify and go-difflib removed, package by
  package, floor unchanged.
- **A5 — API compliance.** `gates api-diff` fetches favro.com/developer
  and writes `testdata/api-surface.json`; `testdata/api-coverage.tsv`
  carries one verdict per endpoint by hand; `api-coverage` and
  `api-fields` run offline in `check`. Then the gaps: webhooks
  (three endpoints), any per-id endpoint not modelled, SCIM written off
  as one categorical row, organizations write endpoints written off.
  Every new endpoint verified live before the commit.
  **Webhooks were parked by an explicit decision in phase 9** ("deferred
  indefinitely", alongside an HTTP transport). The alignment ask reopens
  that: a coverage gate forces a verdict, and "out" needs a reason better
  than "not yet". §17.4 is where it gets decided, not here.
- **A6 — live driver and evals.** `scripts/livefavro` driving the built
  binary against a real organization through `internal/redact`; the
  `transcript` gate; `internal/livecover` and the `live-cover` gate;
  `scripts/evals`.
- **A7 — distribution.** The `.mcpb` bundle packed in Go, SBOM, cosign,
  attestation, reproducible timestamps, `doctor`, issue forms.
- **A8 — documentation.** `docs/configuration.md`, `docs/development.md`,
  `docs/security.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`; the package
  map derived from `go list`; every path a document names checked to
  exist; the staleness gate widened to cover them.

### Closing a phase

`make check` green, tests for the new behaviour, `/simplify` and
`/security-review` over the pending diff with findings resolved or
explained, a look at the schema diff for breaking changes, live
verification where the phase touched the wire, and the changelog entry
under `[Unreleased]`. Tags are cut by the maintainer, never proposed.

## 17. Open decisions

1. ~~**Does `unverified` (§6.2) earn its place as an error class?**~~
   **Decided in A2: no.** Writing the message settled it. An error class
   renders on a result with `IsError` set, which says the call failed —
   and a caller that retries on that repeats a write that may already
   have landed, so the class would cause the damage it warned about.
   It also carries nothing per call, because this server does not read
   back. §6.2 has the reasoning and what to do instead.
2. **Does the `.plugin` bundle stay once `.mcpb` exists?** Two bundles is
   two manifests to keep honest. Answer in A7; the current lean is yes,
   because the plugin is how this server is actually installed.
3. **How much of `favro_get_card_full`'s fan-out belongs in `service`
   versus `tools`?** A3 decides; the fan-out is the one piece of
   genuinely concurrent orchestration in the repository.
4. **Do webhooks come back?** They were deferred indefinitely in phase 9,
   with an HTTP transport, because nothing consumes a callback: this
   server is a stdio process a host starts and stops, and a webhook needs
   an endpoint that outlives it. `GET /webhooks` and `DELETE /webhooks/{id}`
   do not need one — listing and removing what somebody else registered is
   ordinary read-and-write work — and `POST` is the half that implies a
   receiver. The likely answer is therefore a split verdict rather than a
   single row, which is exactly the kind of distinction a per-endpoint
   record can hold and a prose paragraph cannot. Decide in A5.

## 17b. Deviations from the shared Go MCP server standard

Adopted 2026-09-13. Where this server differs, the difference is a
decision rather than drift.

| The standard says | Here | Why |
|---|---|---|
| §3b Login: loopback IP literal, random port, PKCE S256, no OOB flow | HTTP Basic with a user email and an API token, stored in the OS keyring with an env override | Favro's REST API offers no OAuth flow at all. Everything in §3b that can carry over does: keyring with file fallback, an env override, a subcommand that says which source won, and a token that is never printed |
| §7 Verify against the discovery document or specification, never the reference page's prose | There is no discovery document; verification is a live read-back | Favro publishes a reference page that has been observed to disagree with the live API, and returns 200 for a body it ignores (§2.1). The stronger rule replaces the weaker one rather than excusing it: §4.7 |
| §1 An API-coverage gate where the server speaks to a documented API surface | The surface snapshot is scraped from the reference page rather than fetched as a machine-readable document | Same cause. The snapshot is still machine-owned and rewritten only by `gates api-diff`; the hand-written file carries verdicts only (A5) |
| §3 A read-only mode registers only read tools and requests read-only scopes | Read-only mode is the absence of `FAVRO_ENABLE_DESTRUCTIVE` plus `--dry-run`; there are no scopes to request | Favro's API tokens carry the user's own permissions and cannot be scoped down at issue time. The honest mitigation is the README's advice to issue the token from a least-privileged service user |
| §10b Every server ships a `.mcpb` | Ships a `.plugin` today; both after A7 | The `.plugin` is the install path this server actually has users on. Dropping it to satisfy the letter of the rule would break them |

Everything else is adopted as written, including the preamble's three
obligations for any rule adopted — make it a test, derive the list from
the code, assert a floor on how much the checker read.

## 18. Evidence log: conventions checked, changed, or rejected

Sources: the shared standard (read in full, 2026-09-13), the four sibling
repositories' Makefiles, gate registries, CI workflows and architecture
documents, the MCP Go SDK v1.7.0 source in the module cache, Favro's
published REST reference at favro.com/developer, and this repository's
own code and history. Rows are dated where they were checked.

Three tiers, and the row says which: **verified here** against a primary
source or by running something; **adopted** from a sibling that verified
it; **asserted**, meaning believed and not yet held by anything.

| Date | Claim | How checked | Verdict |
|---|---|---|---|
| 2026-09-13 | The SDK writes the same bytes into `content` and `structuredContent` when a tool declares an output schema | Read `mcp/server.go:398–435` in the module cache: the marshalled output becomes `StructuredContent`, and when `res.Content` is nil the same serialized JSON is added as a `TextContent` block | **Verified here.** Every tool in this repository returns a typed output and a nil result, so every one of them is in that state. Standard §2 forbids it: the two halves must both be present and must not be the same bytes. Fixed in A2 at `addTool`, so the fix is one function rather than 83 handlers that each have to remember |
| 2026-09-13 | The debug request log cannot reconstruct its subject | Read `internal/favro/client.go:587–601`: it logs `req.URL.RawQuery`, and Favro's query strings carry `cardCommonId`, `widgetCommonId` and `sequentialId` | **Verified here — the claim is false.** Standard §4's rule is that a log must not identify or reconstruct the subject; an id in a query string does both. A2 logs the parameter names instead, which is the part a debug line is for |
| 2026-09-13 | "Never put tenant data in commits, PRs, docs or tool descriptions" is enforced | Searched the repository for a gate, a test or a CI step holding it. There is none; gitleaks is not configured either | **Verified here — unheld.** The loudest rule in CLAUDE.md is the one nothing can fail. A1 |
| 2026-09-13 | Favro's documented endpoint surface | Fetched favro.com/developer. 22 endpoints this client does not implement: `/webhooks` ×3, `/organizations` write ×2, SCIM v1.1 ×10 and v2.0 ×12 (counted from that fetch) | **Asserted, pending A5.** The fetch went through a summarising reader, which is exactly the "reference page's prose" the standard warns about. A5 re-derives the snapshot per section and the count becomes a gate's output rather than a sentence here |
| 2026-09-13 | The sibling gate set | Read all four `Makefile`s and both gate registries (`scripts/gates`) | **Adopted.** 14 gates plus `transcript` and `live-cover` where a live driver exists. Note the standard's own warning: reading a `check:` target list is not an audit of what runs, since several siblings run gates as ordinary Go tests |
| 2026-09-13 | Actions are pinned | Read `.github/workflows/*.yml`: every action is a floating major tag (`actions/checkout@v7`, …) and `govulncheck` installs `@latest` | **Verified here — unpinned.** GitHub's own guidance is that a full-length commit SHA is the only immutable reference. A1 |
| 2026-09-13 | 83 tools | Counted registered tool-name constants; the README and `docs/TOOLS.md` both say 83 | **Verified here, today.** The standard's §7b: re-measure at each release or date it. A1's staleness gate takes the count over from this sentence |
| 2026-09-13 | govulncheck must run in binary mode | Existing repository decision, recorded in the Makefile: x/vuln v1.7.0's source analysis tops out at go1.26 and errors on the go1.27 stdlib | **Adopted, temporary.** Revert to `./...` when x/vuln ships an x/tools that understands 1.27. The siblings run source mode because they are not yet on a toolchain that breaks it |
| 2026-09-13 | This binary exits 0 when a host closes the pipe | Drove the built binary over stdio and closed stdin: it exited **1**, logging `server is closing: EOF`. The SDK's `jsonrpc2.ErrServerClosing` is `NewError(-32004, ...)`, and `errors.Is(err, io.EOF)` cannot match it | **Verified here — the claim was false.** Every host logs a non-zero exit on an ordinary disconnect as a crash, and this happened on every disconnect this server has ever had. Fixed in A1 by matching the code through the public `jsonrpc.Error` alias; the smoke gate closes stdin with nothing in flight, which is the only way to see it |
| 2026-09-13 | govulncheck must run in binary mode on Go 1.27 | Ran `govulncheck@v1.8.0 ./...` against this module: "No vulnerabilities found". v1.7.0's source analysis errored on the 1.27 stdlib, which is why the workaround existed | **Verified here — no longer true.** x/vuln v1.8.0 (2026-09-08) understands the 1.27 stdlib. Source mode is back in A1, and it analyses call paths rather than a symbol table, which is the stronger check |
| 2026-09-13 | Every dependency carries a licence the allowlist names | `go-licenses check` failed on `github.com/segmentio/asm`, reporting an empty licence name. Read the module's LICENSE: it relicensed from MIT to **MIT No Attribution (MIT-0)** in v1.2.1 | **Verified here.** MIT-0 is OSI-approved and strictly more permissive than the MIT it replaced, and go-licenses v1.6.0's classifier (2023) predates it, so no `--allowed_licenses` value can satisfy the check. Ignored by path with the reason written where the flag is, in both the Makefile and CI |
| 2026-09-13 | An exemption list can be trusted to describe reality | Wrote the leak gate's allowlist of citation prefixes from what such a list usually holds — RFC, CVE, SEP, HTTP, G — then asserted that each one occurs in this repository. **Five of eight did not.** | **Verified here.** Every one of those five read as a considered exemption and exempted nothing; each was a hole somebody would have had to find by accident. The assertion deleted them, and the three that remained (plus two the first real run found: an SPDX identifier, and ISO-8601) are the whole list. This is the standard's rule — derive the list from the code — applied to a list that looked too small to need it |
| 2026-09-13 | A gate that writes what it checks is checking anything | `schema-diff` regenerated `schemas.json` on every run, and the changelog claimed the committed file is where a reviewer sees a wire change | **Verified here — the claim was false.** It was true only for people who ran `make check` before pushing; everyone else got a green build and a diff showing nothing, and the gate left the working tree dirty mid-`check`. Split into `make schemas` (writes) and the gate (verifies, and names that target when it fails). The comparison is by tool surface, not bytes: the dump carries the version stamp, which differs between a local build and CI's |
| 2026-09-13 | `make check` is "everything CI runs" | The parity gate compared the gate registry with the workflow, which covered 8 of `check`'s 15 prerequisites; `vet`, `lint`, `vuln`, `licenses`, `secrets`, `tidy` and `fmt-check` were unheld | **Verified here — the claim was false.** Deleting the vulnerability scan from CI left the gate green while the Makefile went on calling `check` everything CI runs. It now reads the `check:` line itself and derives each prerequisite's signature from its recipe, so the third hand-maintained list a target-name map would have needed does not exist. Its own test caught the next layer of the same bug: `gitleaks/v8@v8.30.1` reduced to `v8`, which matched CI inside the version string it was meant to be checking |
| 2026-09-13 | Every gate here has a test | Walked the command registry against the test sources: `schema-diff` had none — the command that decides what a breaking change is, and so the whole semver promise — and neither did `precommit` or `plugin-pack` | **Verified here — the claim was false**, in the doc comment that makes it and in CLAUDE.md's eleventh rule. Now held by `TestEveryCommandHasATest`, which reads the registry, parses the package's call graph, and requires each command or something it delegates to be exercised in code rather than named in a comment |
| 2026-09-13 | `BSC-123`, the example card reference in tool descriptions and fixtures, is invented | The leak gate flags anything shaped like a card reference, and this prefix is exempted by name — which is only safe if the prefix is not a real board's. Nothing in the repository could answer that, so the maintainer was asked | **Verified with the maintainer.** It belongs to no board of theirs. Recorded beside the exemption, because an exemption whose premise lives only in a conversation is the kind that gets "cleaned up" later by somebody who cannot check it |
| 2026-09-13 | The public history carries nothing about a tenant | Ran `gates leaks history` over every blob and commit message — the first time this repository's history has been scanned. Three session transcripts survive from a removed recording tool; counted the Favro shapes in them directly: zero ids, zero app links, zero credentials | **Verified here.** The rule holds, with the caveat in §9: the transcripts carry the maintainer's address, which git authorship publishes anyway, and the history cannot be rewritten without breaking released tags |
| 2026-09-13 | A leak gate's first run is mostly false positives | Ran it: 4 findings, then 17 from the staleness gate. Three leak findings were real (a fixture address at a registrable domain, a build artifact left in the tree, and this gate's own binary), one was a Go identifier read as a value; of the staleness findings, three were real and fourteen were the extractor's | **Verified here, and consistent with the standard's warning.** The tuning is written into both gates as comments naming what each narrowing is for, because a path check that has not been tuned tells you about your regexp rather than your documentation |
| 2026-09-13 | The debug line's remaining leak was the query string | Wrote the test §9 asked for — capture every record at `LevelDebug`, drive a request whose token and filters are all markers, assert no marker appears anywhere — and ran it against the fixed code | **Verified here — the claim was false.** It failed on the first run, on the `organizationId` *header*: `Token.Apply` sets it on every request and `redactHeaders` redacted only `Authorization`. A test in the repository asserted it passed through unredacted, so the behaviour was not an oversight, it was pinned. Redacted now, and the pinned assertion reads the other way |
| 2026-09-13 | The query string was the whole of the URL leak | Ran `/security-review` over A2's diff. It reported no exploitable finding, and noted below its own bar that `req.URL.Path` was still logged whole | **Verified here — the claim was false.** Every get-one endpoint is `/cards/{cardId}`, so the path carried the same ids the query did. `TestDebugLogNeverCarriesTheSubject` had passed throughout, because it drove a list endpoint where the ids are all in the query — the test proved the rule for one call site and the sentence claimed it for all of them. Fixed, and the test drives a get-one call now |
| 2026-09-13 | A unit test against `httptest` exercises the path the server really sends | The first `redactPathIDs` rejected any digit in a segment. Unit tests passed; the live check printed `path=/api/{id}/cards/{id}` | **Verified here — the claim was false.** `httptest`'s base URL has no version segment, so the test asserted against a shape production never produces and `v1` was being redacted as an identifier. The rule takes lowercase alphanumerics now, and the test has a row for the real path. Nothing leaked — this one cost only the usefulness of the log — but it is the same blind spot as the row above, found the same day, in the fix for it |
| 2026-09-13 | `unverified` earns a place in the error vocabulary | §17's instruction: decide by writing the message. Wrote it — `[unverified] Favro returned 200 and the write was not read back` — and followed what a caller does with it | **Verified here — rejected.** An error class renders with `IsError` set, which says the call failed; a caller that retries on that posts the comment twice. It also carries nothing per call, because this server never reads back, so the flag is constant per tool — and a constant per tool is a tool description, which is where it already is. §6.2 records the reasoning |
| 2026-09-13 | Twelve tools are destructive | Counted the tools annotated `DestructiveHint: true` while building the registration gate | **Verified here — the claim was false; there are thirteen.** §8 had carried the hand-typed count since it was written. The gate now reads the annotation at registration and the test derives the same set from the live surface, so neither a count nor a list of names is written down anywhere |
| 2026-09-13 | A gate that skips the file it guards is checking the right thing | Ran the new `classes` gate: it reported six of the nine classes as emitted by nothing | **Verified here — the claim was false, and it was this gate's own first finding about itself.** It skipped `class.go` wholesale to avoid counting the declarations, and `Classify` — where six of the nine are returned from — is in that file. It now skips the const block and the `Classes` slice and walks everything else |
| 2026-09-13 | Favro has no OAuth for its REST API | Reference page documents HTTP Basic with email + API token only; no authorization endpoint is published | **Verified here.** §17b row 1 |
