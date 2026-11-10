# Architecture

How `favro-mcp` is put together, and the decisions a contributor should
not undo without a better reason than the one that put them here. Read
the decision before changing the tool surface, the error vocabulary, the
write path or the package layout. Part 1 is the overview. Part 2, from
[Decisions](#decisions), is one entry per decision, cited elsewhere by
its D-ID. What was verified about Favro lives in the comment at the code
that depends on it, with Favro's reference linked and, for a
load-bearing quirk, the live step that checks it named.

## Context

`favro-mcp` is a Model Context Protocol server for Favro. One Go binary
runs as a subprocess of an MCP client and speaks MCP over stdio. It
holds one person's Favro address, API token and organization id, and
calls Favro's REST API over HTTP Basic with wire types of its own (D-01,
D-04, D-54). Nothing is hosted.

It is more than a wrapper around the REST API, because that API is
shaped by ids a model cannot navigate. A card has a per-widget `cardId`,
a cross-widget `cardCommonId` and a sequential number. Columns repeat
their names across boards. Tags and custom fields belong to the whole
organization and have no name index, and there is no full-text search.
So the surface adds what the API lacks: names resolved to ids, a
client-side search, and a card read with every id dereferenced (D-07,
D-08, D-13, D-11).

A few facts about Favro shape everything else. It answers 200 to a
request body it ignores, and its reference disagrees with the live API
in places, so a 200 is not confirmation (D-73, D-15, D-24). A later page
of a listing is served from its page number alone (D-16). Calls
are counted per user per hour, at a rate the organization's plan sets
(D-42). A card delete reaches one widget's copy unless told otherwise
(D-37).

The parties around the server:

- **The person** issues the API token, ideally from a least-privileged
  Favro user, and every call acts in their one organization (D-02, D-55,
  D-56).
- **The deployer** sets the switches the client passes at start: the
  destructive tools, the upload directory, the log level and
  process-wide dry run (D-29, D-38, D-28, D-57). The deployer is usually
  the person.
- **The client** approves each call by its own rules and draws its
  prompt from the tool annotations. The server asks the person nothing
  (D-31, D-33, D-34).
- **The model** chooses the calls and their arguments, sets `confirm`,
  and reads card text other people wrote. It can be wrong, so no tool
  lets it widen its own reach (D-30, D-35, D-39). The threats this
  leaves open are in [security.md](security.md).
- **Favro** is the only network peer. There is no telemetry and no
  update check (D-53).

It is not a multi-organization server (D-02), a SCIM or
organization-administration client (D-03), a webhook receiver (D-67), or
a guard on what the token may do in Favro itself (D-56).

## Quality goals

In priority order:

1. **Nothing is lost or exposed by a call nobody meant.** The
   destructive tools exist only when the deployer enables them, each
   needs `confirm`, and the widest carry a prompt on every call. The
   ordinary writes share only inside the organization, uploads read one
   directory, and a mistyped tag never becomes a tag (D-29, D-30, D-34,
   D-35, D-38, D-21).
2. **A result says what Favro did, not what was asked.** A write Favro
   may accept and drop is read back. A write that may have landed is
   `[ambiguous_outcome]` and is never sent again. No tool states an
   outcome its response did not carry (D-24, D-41, D-25, D-22, D-23).
   The unit tests run against fakes written from what Favro is believed
   to do, and only a live read-back tests that belief (D-73, D-74).
3. **The hourly budget is spent on purpose.** One call is one page,
   names resolve through one cache, and a full card is one call rather
   than several (D-16, D-08, D-11, D-42).
4. **Nothing about a tenant leaves where it belongs.** Stdout carries
   frames only, logs carry no subject, the token is never printed,
   transcripts are redacted and never committed, and the repository
   holds nothing real (D-45, D-46, D-47, D-51, D-49).
5. **Every rule is a check, and the surface is a contract.** A rule
   adopted is a test that derives its list from the code, and a released
   tool or input keeps working (D-68, D-64, D-65, D-69).

## Codemap

Dependencies run one way: `server` to `tools` to `service` to `favroapi`
to `favro`, with `config`, `cache`, `auth` and `render` as leaves
(D-05).

- `cmd/favro-mcp` is the program. With no subcommand it serves.
  `--dry-run`, `--dump-schemas`, `--version`, `auth login`,
  `auth status`, `auth logout`, `auth which`, `doctor` and
  `doctor --show-ids` are the rest. It reads the settings once, refuses
  a removed setting still turned on, resolves and checks the
  credentials, and exits non-zero when they fail (D-32, D-55, D-58). It
  is the one place that names stdout (D-45).
- `internal/config` holds every `FAVRO_*` setting, the removed ones
  included, as `config.Vars`. Each is read once through `config.Lookup`,
  with the spaces around a value dropped. `CredentialsFrom` reads the
  three credentials for the callers to hand to `auth` (D-57).
- `internal/auth` says where credentials come from, the environment and
  then the OS keyring, and what they are: `Token`, which sets HTTP Basic
  and the organization header. It holds the startup validator with its
  own timeout, and `KeyringSource`, the only writer of the keyring
  (D-54, D-55, D-39, D-43).
- `internal/favro` is Favro's wire vocabulary, one file per resource. It
  imports nothing from this module. The accessors that accept two
  observed shapes live here (D-04, D-05, D-15).
- `internal/favroapi` is the REST client, one method per endpoint. It
  holds the retry policy, the rate-limit snapshot, pagination, the
  dry-run gate, the redaction of paths and headers for the debug line,
  and typed errors that name their own class. It imports no MCP (D-28,
  D-41, D-42, D-46, D-40).
- `internal/favroapi/favroapitest` is the one client fixture the test
  packages above the client share, with an invented credential triple
  (D-49).
- `internal/cache` is the goroutine-safe TTL cache that the resolver and
  search run on (D-08).
- `internal/service` is the orchestration, in plain Go values with no
  MCP: the resolver and its caches, client-side search, the full-card
  fan-out, and the description editors with their Myers diff (D-08,
  D-09, D-13, D-11, D-18, D-19, D-20). The editors' golden corpus sits
  in its testdata directory.
- `internal/render` writes the readable half of every result and
  declares the closed error vocabulary. It imports nothing from this
  module, which is what lets the client's errors name their class (D-61,
  D-40, D-05).
- `internal/tools` is the MCP surface, one file per area of Favro.
  `register.go` is the one list of what registers. `registry.go` holds
  `addTool`, which applies the destructive gate, `confirm`, the mark,
  the dry-run context, both result halves and the error class (D-60,
  D-29, D-30, D-34, D-61). `sharing.go` holds the
  inside-the-organization checks and `verify.go` the read-backs (D-35,
  D-24).
- `internal/server` builds the MCP server and dumps the schemas through
  an in-memory client session (D-66).
- `internal/redact` is the one redactor that the live driver, the evals
  and `doctor` print through. It matches shapes, and takes known values
  registered by kind (D-51, D-48).
- `internal/livecover` holds the live driver's named steps and
  placeholders, so the driver and the `live-cover` gate read one list
  (D-74).
- `internal/version` holds the build stamp, and falls back to what Go
  embeds for a `go install` build (D-86).

Beside the packages, `scripts/gates` is every check the repository runs
on itself, one Go program behind a registry; `go run ./scripts/gates`
lists them (D-06, D-68). `scripts/livefavro` is the live driver that
`make live` runs (D-74, D-75). `scripts/evals` holds the agent evals,
behind the `evals` build tag and run by `make evals` (D-76, D-77).
`testdata` holds the API surface snapshot, the per-endpoint and
per-field verdicts, the live-cover waivers and the outcome-claim
allowances (D-72, D-74, D-25). `packaging/mcpb` holds the Claude Desktop
bundle's manifest, `packaging/registry` the MCP registry entry, and
`plugin-template` the plugin's manifest (D-88, D-89, D-92).
`schemas.json` is the committed schema dump that `schema-diff` compares
against (D-64).

## Request flow

### Startup

`cmd/favro-mcp` reads every setting through `config.Lookup`. With
`--dump-schemas` it prints the surface and exits before it reads a
credential (D-66). A removed setting still turned on refuses the start
(D-32). Credentials come from the environment, else the keyring, and a
partial set in the environment is an error (D-55). One request validates
them against Favro under a short timeout, unless
`FAVRO_MCP_SKIP_VALIDATE` is on. Any failure logs its reason to stderr
and exits non-zero (D-58, D-43).

`tools.Register` adds every tool through `addTool`. The destructive
tools register only with `FAVRO_ENABLE_DESTRUCTIVE`, and the upload
tools only with a valid `FAVRO_UPLOAD_DIR` (D-29, D-38). The server then
serves stdio, and a host closing the pipe is a clean exit (D-44).

### A read

A client calls `favro_get_card_full` with a sequential id. The handler
passes the identity to `internal/service`, which lists the cards
carrying that number and takes the first board copy. Every copy is
listed in `placements` (D-10, D-12). Tags, assignees, the widget and its
column, the collections and the custom fields are resolved in parallel
through the resolver's caches. A custom-field type the server does not
know passes through undereferenced (D-11, D-08).

`internal/favroapi` sends each request with the credentials, records the
rate-limit headers, and retries a read's 5xx with backoff (D-41, D-42).
`addTool` returns the structured result and a readable summary that
differs from it (D-61).

### A write

A client calls `favro_move_card` to move a card to another column.
`addTool` puts a requested dry run on the context (D-28). The handler
reads the card's current parent and `sheetPosition` and carries them in
the body, so the move does not detach the card (D-23). The client
refuses a column move without `widgetCommonId`, and checks the column in
Favro's answer (D-22). Under dry run it returns the redacted request
instead of sending it.

The PUT goes once. A 5xx, or a connection lost after the request went
out, is `[ambiguous_outcome]` (D-41). After a real write the tool reads
the card back and reports the verdict in `notes`, and the caches the
write changed are invalidated (D-24, D-09).

A destructive call adds three steps. `favro_delete_collection` exists
only because the deployer enabled the destructive tools. `addTool`
refuses it `[blocked]` without `confirm: true`, unless it is a dry run.
Claude Code prompts on every call of it, because the tool carries the
mark (D-29, D-30, D-34).

## Invariants

- Stdout carries only MCP JSON-RPC frames (D-45).
- A log line carries no payload and no value that identifies the subject
  (D-46).
- The token is never printed (D-47).
- No tool takes an organization id (D-02).
- Every write takes `dry_run`, and the gate is in the HTTP client
  (D-28).
- A tool is destructive when its input takes `confirm`, and it registers
  only with the destructive flag (D-29).
- A destructive call without `confirm: true` is refused unless it is a
  dry run (D-30).
- The server asks the person nothing (D-31).
- No tool writes the server's own settings (D-39).
- The ordinary writes share only inside the organization (D-35).
- A write is sent once, and one that may have landed is
  `[ambiguous_outcome]` (D-41).
- Every tool error is `[class] message` from one closed vocabulary
  (D-40).
- `content` and `structuredContent` are never the same bytes (D-61).
- Every tool registers through `addTool` (D-60) and has a smoke row
  (D-62).
- Nothing from a real tenant enters the repository (D-49), and a
  transcript is never committed (D-51).
- A released tool or input keeps working (D-64), and a pull request
  names each change to the surface (D-65).
- `make check` and CI run the same set (D-69).

## Cross-cutting concerns

**Errors.** Every tool error is a tool result of the form
`[class] message`, and the class tells the caller what to do next. The
vocabulary is closed and declared in `internal/render/class.go`, and
[TOOLS.md](TOOLS.md#error-classes) documents each class for a reader.
The `classes` gate holds the two to each other from both sides. A class
is read from the error's type, never its text, and an error that names
none is `invalid` (D-40).

**Caching and the budget.** One resolver per process caches the
organization's lists, longer for what rarely changes than for what
people add mid-session. A write invalidates what it changed, and every
list and resolve tool takes `force_refresh` (D-08, D-09). Search caches
its corpus per scope (D-13). Nothing pages on the caller's behalf, and
`favro_rate_limit_status` reports the last rate-limit headers without a
call (D-16, D-42). The TTLs are the code's, in
`internal/service/resolver.go`.

**Dry run.** Every write takes `dry_run`, and `--dry-run` forces it for
the process. The gate is in the HTTP client, so no tool can forget it,
and a dry run returns the request it would have sent with the
credentials redacted (D-28).

**Confidentiality.** A debug line carries the method, the path's shape,
the query's parameter names, the attempt and the outcome (D-46). The
live driver, the evals and `doctor` print through one redactor, which
cannot catch a name, so a transcript is never committed (D-48, D-51).
The `leaks` gate scans the tree, and its history scan runs by hand
(D-49, D-50). The threat model is in [security.md](security.md).

**Approval.** The server asks the person nothing, and what holds on
every client is in the server. The destructive tools register only with
the flag, and each needs `confirm` (D-29, D-30). The ordinary writes
share only inside the organization, and widening a collection is a tool
of its own (D-35, D-36). The wider delete is named in the arguments
(D-37). The annotations say which tools change something, and the widest
carry Claude Code's mark (D-33, D-34).

**Verification.** Unit tests use Go's own `testing` package against
`httptest` fakes (D-70). The MCP surface is exercised in memory, and the
`smoke` gate drives the built binary over stdio (D-62, D-44). The live
driver drives every tool and option through the binary against a real
organization with `dry_run`, and its write mode checks real writes on
probes it creates (D-74, D-75). The agent evals score whether a model
can do a task through the tools at all (D-76). A fake agrees with
whatever it was written from, so only the live run tests the belief
behind it (D-73). How to run the live driver is in
[CONTRIBUTING.md](../CONTRIBUTING.md#the-live-run).

**Gates.** `go run ./scripts/gates` lists every gate, and `make check`
runs the set CI runs (D-68, D-69). The docs gate holds every D-ID
citation to an entry, every Held by cell to something that exists, the
codemap to the tree and every live step a comment names to the driver.
The staleness gate holds the README, [TOOLS.md](TOOLS.md),
[configuration.md](configuration.md) and the CHANGELOG to the code
(D-78).

**Distribution.** A release is built from a tag and signed and attested
(D-83, D-84). It carries the platform archives, the `.plugin` and a
Claude Desktop `.mcpb` whose settings derive from `config.Vars`, every
switch off by default (D-88, D-89, D-90, D-91). Releasing is in
[release.md](release.md), and fixing a symptom in
[troubleshooting.md](troubleshooting.md).

## Decisions

| ID | Decision | Status | Held by |
| --- | --- | --- | --- |
| D-01 | One Go binary serves one person's Favro account over stdio; nothing is hosted | accepted | smoke, review |
| D-02 | The server binds one organization at startup, and no tool takes an organization id | accepted | one-organization, TestOneOrganizationAgainstThisRepository, TestMCP_GetOrganization_TakesNoOrganizationID |
| D-03 | SCIM provisioning and creating or changing an organization are out of scope | accepted | api-coverage, TestAPICoverageAgainstThisRepository |
| D-04 | Own wire types over raw REST; there is no generated Favro client | accepted | review |
| D-05 | Dependencies run server → tools → service → favroapi → favro; favro, render and auth import nothing from the module, and nothing below tools imports MCP | accepted | depguard, review |
| D-06 | Everything the repository runs on itself is Go; the one shell file is the git hook that runs it | accepted | review |
| D-07 | The surface adds what Favro's API lacks: name resolution, client-side search and a dereferenced card read | accepted | review |
| D-08 | One resolver per process turns names into ranked id candidates, cached longer for org metadata than for what people add mid-session | accepted | TestResolveTag_RankingScoreScale, TestTTL_Invalidate |
| D-09 | A write invalidates the caches it changed, and every list and resolve tool takes force_refresh | accepted | TestInvalidateCacheHooks, TestMCP_CreateTag_InvalidatesCacheOnSuccess, TestMCP_CreateCard_InvalidatesSearchCacheOnSuccess |
| D-10 | The model addresses by name through a resolver or by Favro's own ids; a card by exactly one of three ids, and each tool says which it takes | accepted | TestGetFullCard_IdentityValidation, review |
| D-11 | favro_get_card_full dereferences every id in parallel in internal/service, and passes an unrecognized custom-field type through undereferenced | accepted | TestGetFullCard_HappyPath_ByCardID, TestGetFullCard_SkipsResolversWhenCardHasNoIDs, TestFormatCustomFieldValue |
| D-12 | A card looked up by common or sequential id is its board copy, every copy is listed in placements, and unique keeps the board copy | accepted | TestGetFullCard_PrefersTheBoardCopyAndListsEveryPlacement, TestGetFullCard_ByCardIDListsThePlacementsToo, TestMCP_ListCards_UniqueKeepsTheBoardCopy |
| D-13 | Search is client-side full text over one widget or one collection, scored name before body, and cached per scope | accepted | TestScoreCard_ScoreScale, TestScoreCard_NameBeatsBodyOnly, TestMCP_SearchCards_ScopeConflict, TestMCP_SearchCards_MissingScope |
| D-14 | Descriptions are read and returned in Favro's stored markdown, and matching is never loosened to its normalization | accepted | TestMCP_GetCard_DescriptionFormatForwarded, TestSearchCards_ForcesMarkdownDescriptionFormat |
| D-15 | A read accepts the documented and every observed shape; a write sends the documented one and says when it is unverified | accepted | TestCard_CustomFields_ReconcilesBothWireKeys, TestWebhookColumnIDsAcceptsBothSpellings, review |
| D-16 | One call is one page: list tools return next_page and request_id, and nothing aggregates | accepted | TestPaginate_MultiplePages_EchoesRequestID, TestMCP_ListOrganizations_ForwardsRequestIDOnPage2, review |
| D-17 | Page numbers are 1-indexed on the surface and converted to Favro's 0-indexed pages in the tool layer | accepted | TestListToolPageNumberingIsOneIndexed |
| D-18 | A description is edited in place by append, prepend or replace-in, each returning the old text, the new and a unified diff | accepted | TestDescriptionEditors_GoldenCorpus, TestMCP_AppendCardDescription_HappyPath, TestMCP_PrependCardDescription_HappyPath |
| D-19 | replace-in changes one occurrence by default, and sends nothing when find matches nothing | accepted | TestReplaceInDescription_LiteralDefaultCount, TestMCP_ReplaceInCardDescription_NoMatch_RefusesPUT |
| D-20 | The unified diff is the repository's own Myers diff, held byte for byte to diff -u | accepted | TestUnifiedDiffMatchesGNUDiff, TestUnifiedDiffBoundsTheSearch, TestUnifiedDiffScalesWithChangeNotSize |
| D-21 | Tag tools refuse an unknown tag name; creating a tag is favro_create_tag | accepted | TestMCP_AddTagToCard_HardFailUnknown, TestMCP_AddTagToCard_AmbiguousNames |
| D-22 | A card move names its widget: the client refuses a column or lane move without it, and checks the column Favro returns | accepted | TestUpdateCard_ColumnMoveWithoutWidget_NoNetworkCall, TestMoveCard_ColumnMoveWithoutWidget, TestUpdateCard_MoveIgnored, TestColumnMoveContractIsStatedOnce |
| D-23 | A structural card write carries the card's current parent and sheetPosition, and clear_parent detaches on purpose | accepted | TestMCP_StructuralWrite_ParentGuard, TestMCP_StructuralWrite_KeepsTheCardsPlaceUnderItsParent, TestStructuralCardWrites_RunTheParentGuard, live |
| D-24 | A write Favro may accept and drop is read back, and its verdict is a note on a successful result | accepted | TestMCP_CardPlacement_ReadBack, TestMCP_CardPlacement_ReadBackFails_ReportsUnconfirmed, TestMCP_SetCardCustomField_NotOnCard_Reports, TestMCP_SetCardCustomField_OnCard_ReportsValue, live |
| D-25 | No tool states an outcome the response did not carry | accepted | outcomes, TestOutcomesCatchesAnAssertedOutcome, TestOutcomesAgainstThisRepository |
| D-26 | Moving a card to another widget adds an instance there, and the tool says so; no move is assembled from an add and a delete | accepted | review |
| D-27 | A group update sends the whole intended member list | accepted | TestMCP_UpdateGroup_HappyPath, review |
| D-28 | Every write takes dry_run, enforced in the HTTP client, and --dry-run forces it for the process | accepted | TestEveryWriteTakesDryRunAndSendsNothing, TestDryRun_WriteHelpers_NoRoundTrip, TestADryRunWritesNothingWhateverTheHandlerDoes, TestRunServer_DryRunFlag_AnnouncedAtStartup, live-cover |
| D-29 | The destructive tools register only with FAVRO_ENABLE_DESTRUCTIVE=true, and a tool is destructive when its input takes confirm | accepted | TestDestructiveToolsAreOptIn, TestEveryWriteIsClassified, TestLoadDestructive, staleness |
| D-30 | A destructive call needs confirm: true unless it is a dry run | accepted | TestEveryDestructiveCallNeedsConfirm, TestNoWriteWithoutConfirmSendsADelete, live |
| D-31 | The server asks the person nothing; per-call approval is the client's | accepted | TestMCP_AllTools_SmokeCallable, live |
| D-32 | FAVRO_REQUIRE_PROMPT refuses start unless false, and FAVRO_INTERACTION_HINT is ignored with a warning | accepted | TestARemovedSettingIsRefusedUnlessOff, TestARemovedHintIsWarnedWhenOff, TestRunServer_ARemovedSettingRefusesToStart, TestDoctorFailsOnARemovedSetting |
| D-33 | readOnlyHint is on the reads alone, and destructiveHint is false only on a write that adds | accepted | TestOnlyToolsThatAddSayTheyAreNotDestructive, TestEveryToolIsAnnotated, TestEveryWriteIsClassified |
| D-34 | Five tools carry Claude Code's requiresUserInteraction, and only a destructive tool may | accepted | TestOnlyTheWidestWritesPrompt |
| D-35 | The ordinary writes share only inside the organization, and refuse every widening as [blocked] | accepted | TestOrdinaryWritesShareOnlyInsideTheOrganization, TestCollectionWritesRefuseWideSharing, TestEverySharingArgumentRefusesItsWidestValue, TestAGroupIsSharedOnlyWhenEveryoneInItIsAMember, TestOrganizationRoles |
| D-36 | Opening a collection to the organization or to anyone with its link is favro_widen_collection_sharing, witnessed by the collection's name and read back | accepted | TestWidenCollectionSharing, live |
| D-37 | The wider delete is an argument of the same delete tool, and favro_delete_widget refuses a call that names no scope | accepted | TestDeleteWidgetNamesItsScope, TestMCP_DeleteCard_Everywhere |
| D-38 | The upload tools read only inside FAVRO_UPLOAD_DIR, through os.Root, and are not registered without it | accepted | TestMCP_UploadAttachment_ReadsOnlyTheUploadDir, TestUploadToolsNeedAnUploadDir, TestLoadUploadDir, TestUploadAttachment_ExactlyAtCapIsAllowed |
| D-39 | No tool writes the server's own settings: the environment, a file or the keyring | accepted | TestNoToolWritesTheServersOwnSettings |
| D-40 | Every tool error is `[class] message` from one closed vocabulary, classified by the error's type and never its text, with invalid as the fallback | accepted | classes, TestClassesAgainstThisRepository, TestClassify, TestEverySentinelIsClassified, TestEveryErrorTypeNamesItsClass |
| D-41 | A 429 is retried once when it says to wait 30 seconds or less, and a read's 5xx up to three attempts; a write is sent once, and a 5xx or a failure after the dial is ambiguous_outcome | accepted | TestDo_429WithinCap_RetriesOnce, TestDo_429AboveCap_ReturnsRateLimitError, TestDo_5xx_RetriesUpToBudgetThenTransientError, TestDo_WriteIsNotRepeatedAfter5xx, TestDo_WriteCutOffAfterSendIsAmbiguous |
| D-42 | Every response's rate-limit headers are recorded, and favro_rate_limit_status reports the last snapshot without a call | accepted | TestDo_RecordsRateLimitSnapshot, TestMCP_RateLimitStatus_AfterObservation |
| D-43 | Every HTTP call has a timeout, the startup check and doctor's live half included | accepted | TestDefaultValidator_HasReasonableDefaults, review |
| D-44 | A host closing the pipe is a clean exit, matched by the SDK's error code | accepted | TestCleanDisconnect, smoke |
| D-45 | Stdout carries only MCP JSON-RPC frames, and logs go to stderr through slog | accepted | forbidigo, TestForbidigoGuardsStdout, smoke |
| D-46 | A log line carries the method, the path's shape, the query's parameter names, the attempt and the outcome, never a payload or a value that identifies the subject | accepted | TestDebugLogNeverCarriesTheSubject, TestRedactPathIDs, TestRedactHeaders_Authorization, TestStartupLineNamesNoTenant |
| D-47 | The token is never printed: not in a result, a log, a dry-run record, auth status or doctor | accepted | TestAuthStatus_ReportsSourceAndOrgButNeverTheToken, TestAuthLogin_StoresInKeyringWithoutEchoingTheToken, TestDoctorShowIDsPrintsIDsButNeverTheToken, TestMCP_FavroPing_OutputContainsNoSecrets, TestSecretsScrubsTheTokenAndLeavesIdentifiers |
| D-48 | doctor's report is written to be pasted in public: the values it hides are registered with the redactor by kind, and --show-ids prints them for the person's own screen | accepted | TestDoctorRedactsCredentials, TestLiteralRedactsWhatNoPatternMatches, TestCountReportsWhatItRedacted, TestDoctorShowIDsPrintsIDsButNeverTheToken |
| D-49 | Nothing from a real tenant enters the repository: fixtures are synthetic and name resources by role, and leaks scans the tree | accepted | leaks, TestFindLeaksCatchesEachShape, TestFindLeaksLeavesTheRepositoryAlone, TestLeaksReadsTheRepository |
| D-50 | The history scan runs by hand and the history is not rewritten; each known finding is accepted in its own commit by hash, rule and count | accepted | TestKnownFindingIsPinnedToItsCommit, TestEveryKnownFindingIsInTheHistory, TestHistoryReadsATagFromItsMessage |
| D-51 | The live driver and the evals print only through internal/redact, and a transcript is never committed | accepted | transcript, TestTranscriptAgainstThisRepository, TestTranscriptSkipsOnlyTheDisabledStub, TestTranscriptReadsEveryDriverFile |
| D-52 | The webhook signing secret has no field, so no result can carry it | accepted | TestWebhookSecretIsNeverDecoded |
| D-53 | Favro is the only network peer; there is no telemetry, crash reporting or update check | accepted | review |
| D-54 | Favro is authenticated by HTTP Basic, an address and an API token; there is no OAuth flow | accepted | TestValidator_AuthorizationHeaderIsBasic |
| D-55 | Credentials resolve from the environment, then the OS keyring; the first complete triple wins, and a partial one is an error | accepted | TestDefaultSources_OrderIsEnvThenKeyring, TestAuthWhich_EnvWinsOverKeyring, TestAuthWhich_FallsBackToKeyring, TestRunServer_PartialEnvCredentials_Errors, TestKeyringSource_Load_CorruptPayload_Errors |
| D-56 | Read-only use is the destructive flag left off plus --dry-run, and the token comes from a least-privileged user | accepted | review |
| D-57 | Configuration is the environment: every variable is in config.Vars and read through config.Lookup, with the spaces around it dropped | accepted | TestEveryVariableTheCodeNamesIsInEnvVars, TestServerVariablesAreSpelledOnce, TestASettingOfSpacesIsUnset, TestLookup, TestVarsDescribeWhatLoadDoes, staleness |
| D-58 | The server refuses to start without a credential Favro accepts, and FAVRO_MCP_SKIP_VALIDATE skips the live check | accepted | TestMain_ExitsNonZeroOnStartupFailure, TestRunServer_NoCredentials_ErrorsBeforeContactingFavro, TestLoadSkipValidate, TestRejectedCredentialsNameTheVariables |
| D-59 | doctor reports the build and its version source, the credential source, whether Favro accepts the credentials and whether the organization binding is right | accepted | TestDoctorFailsOnWrongOrganizationBinding, TestDoctorReportsUnreachableAPI, TestDoctorWithoutCredentialsSkipsTheNetwork |
| D-60 | Every tool registers through addTool, the one place for the destructive gate, confirm, the mark, both result halves and the error class | accepted | review |
| D-61 | content and structuredContent are never the same bytes: addTool fills the readable half from internal/render | accepted | TestContentAndStructuredContentDiffer, TestSummaryPrefersSummarizer, TestListToolContentSummarizesThePage |
| D-62 | Every registered tool has a smokeToolInputs row | accepted | TestMCP_AllTools_SmokeCallable |
| D-63 | Where two tools overlap, each description names the other and says when to choose it | accepted | review |
| D-64 | The released tool surface is a contract: a removed tool, a removed input or a newly required one is a break that schema-diff names | accepted | schema-diff, TestCompareSurfacesNamesEveryBreakingChange, TestSameSurfaceIgnoresTheBuildStamp, TestABreakIsABreakOfARelease |
| D-65 | A pull request names each changed item of the tool surface in a commit footer | accepted | schema-ack, TestSchemaAckNeedsEveryItemNamed, TestSchemaAckTellsARefusalFromAFailure, TestParityHoldsThePullRequestGates |
| D-66 | --dump-schemas lists the whole surface, whatever the flags, as an in-memory client session sees it | accepted | TestDumpSchemasListsEverythingAClientIsTold, TestDumpSchemasNeedsNoCredentials |
| D-67 | Webhooks are listed and deleted, never registered | accepted | api-coverage, TestDeleteWebhookIsGatedAndDryRunnable, TestListWebhooksDecodesABareArray |
| D-68 | Every rule adopted is a test that derives its list from the code and asserts a floor on how much it read | accepted | TestEveryCommandHasATest, review |
| D-69 | make check and CI run the same set, matched by what each recipe runs | accepted | parity, TestParityCatchesEachDivergence, TestParityAgainstThisRepository, TestParityRefusesToCompareNothing, TestParityHoldsTheTreesTagsToAVetPass, TestParityRefusesACommandWhoseFailureMakeIgnores, TestParityHoldsMakeCheckToWhatAPushRuns |
| D-70 | Unit tests use only Go's standard library, against httptest fakes, with the race detector | accepted | depguard, review |
| D-71 | Every package holds an 80% statement floor of its own | accepted | cover, TestEveryExemptionNamesARealPackage, TestReadProfileRefusesAnEmptyRun |
| D-72 | Every endpoint Favro documents and every field of a modeled type has a verdict, held offline to a snapshot refetched by hand | accepted | api-coverage, api-fields, TestAPICoverageAgainstThisRepository, TestCheckCoverageReportsAnUndecidedEndpoint, TestCheckFieldsReportsAStaleWaiver |
| D-73 | A wire contract is verified live, by reading the resource back; the reference page is not proof | accepted | live |
| D-74 | The live driver drives every tool and option through the built binary with dry_run, from the steps internal/livecover holds | accepted | live-cover, TestStepsWithoutDryRunNamesEveryWriteThatLacksIt, TestMeasureCountsWhatItSays, TestTheStepsRunThroughAServerStartedWithDryRun, live |
| D-75 | The live driver's write mode writes only to probes it created, and deletes them however the run ends | accepted | TestScratchWritesOnlyToObjectsTheRunCreated, TestSweepFailsOnAProbeThatIsStillThere, TestEachInterruptRunsTheCleanups, live |
| D-76 | Agent evals score each task by the end state read back and by the trace, in a sandbox the run makes and removes | accepted | TestEveryTaskIsScorableAndExplained, live |
| D-77 | The eval session reaches this server's tools and nothing else, and none that deletes | accepted | review |
| D-78 | The documentation is held to the code: tool names, counts, settings, gates, the package map and paths | accepted | staleness, TestStalenessAgainstThisRepository, TestToolsDocumentedReadsBothDirections, TestDestructiveCountMatchesTheBinary, TestEveryEnvNameReachesBothDocuments, TestTheChangelogRule, TestTheChangelogRuleCountsWhatShips, TestStalenessRunsEveryOtherCheckWithoutARelease |
| D-79 | The toolchain is the one go.mod names, everywhere; there is no N-1 matrix entry | accepted | review |
| D-80 | Every action is pinned to a full SHA, every tool to one exact version, and every workflow pins its shell | accepted | pins, TestPinsReadsTheWorkflows, TestExactSemverRejectsWhatCanMoveUnderYou, TestPinsShellReadsDepth, TestUnpinnedTools, TestPinsNamesEachProblem, TestReadersRefuseALineBreakTheyDoNotRead |
| D-81 | make check scans for vulnerabilities in source mode, licenses against an allowlist and secrets with gitleaks, as CI does | accepted | vuln, licenses, secrets, parity, TestTheHookScansWithTheMakefilesGitleaks |
| D-82 | CI runs on Linux, macOS and Windows | accepted | review |
| D-83 | A tag is the whole release, and its notes are that version's CHANGELOG section | accepted | release-notes, TestChangelogSectionTakesOneVersion, TestASectionEndsOnlyAtTheLinkFooter, TestReleaseNotesReadTheFence, TestReleaseNotesRefusesATagThatIsNotAVersion |
| D-84 | A release is signed and attested: the checksums cover the archives and both bundles, under keyless cosign and build provenance, with an SBOM per archive and byte-identical rebuilds | accepted | mcpb, TestReleaseWiringIsHeld, TestTheBundlesCarryProvenance |
| D-85 | A tag name reaches no shell: it travels through the environment, and the trigger admits only alphanumeric suffixes | accepted | TestReleaseNotesRefusesATagThatIsNotAVersion, TestReleaseNotesAcceptsRealVersionShapes, review |
| D-86 | A go install build reports the module version Go embeds | accepted | TestSourceAndModuleAlwaysAnswer, TestStampFrom |
| D-87 | Changes reach main through a pull request with eight required checks and no bypass actors | accepted | review |
| D-88 | Both bundles ship: the .plugin, which is how people install this server, and the .mcpb beside it | accepted | plugin, mcpb, TestPluginGateAgainstThisRepository, TestCommittedManifestPasses |
| D-89 | The .mcpb asks for every setting the server reads, derived from config.Vars, each defaulting to the server's own default, and is the whole setup | accepted | mcpb, TestBundleVarsAreDerivedFromTheSettings, TestTheConfigDiffNamesTheFieldsThatDiffer, TestAManifestThatConfiguresNothingIsCaught |
| D-90 | A bundle's commands start with ${__dirname}/ and name a staged binary or the launcher, and no platform override sets env or args | accepted | mcpb, TestManifestProblemsRunsEveryCheck, TestLauncherNamesWhatThePackerStages, TestTheCommittedManifestMeetsTheFloor |
| D-91 | One release matrix names the six archives, and both bundles run the Windows amd64 binary on arm64 | accepted | TestTheReleaseMatrixIsTheFamilysSix, TestGoreleaserMatrixIsTheOneTheBundleClaims, TestReleaseArchivesAreTheFamilys |
| D-92 | The MCP registry entry obeys the rules the registry enforces in code | accepted | registry, TestMCPRegistryCatchesWhatTheRegistryRefuses, TestMCPRegistryAgainstThisRepository |
| D-93 | A write whose 200 is not trusted carries an `unverified` error class | rejected | — |
| D-94 | The server confirms its riskiest writes with the person, through MCP form elicitation | superseded by D-31 | — |

### D-01 One Go binary serves one person's Favro account over stdio; nothing is hosted

The server is one Go binary that an MCP client starts as a subprocess
and talks to over stdio. It holds one person's Favro address, API token
and organization id, and every call acts as that person. Nothing is
hosted, and there is no shared deployment.

The reason is reach. A Favro API token carries everything its user can
do in the organization (D-56). A server per person keeps every call
inside that person's own access, with the token in their own keyring
(D-55). A stdio process also has no port to expose and no session of its
own to authenticate. The binary ships as release archives and in two
bundles a client installs (D-88).

Rejected: a hosted, multi-tenant server, which would hold many people's
tokens in one place; an HTTP transport, which needs authentication of
its own and a process that outlives the client.
Cost: each person installs the binary and supplies their own three
values. Anything that needs a URL Favro can call, such as receiving a
webhook, cannot exist here (D-67).

### D-02 The server binds one organization at startup, and no tool takes an organization id

The server binds one organization at startup, from
`FAVRO_ORGANIZATION_ID`, and no tool takes an organization id.
`favro_get_organization` takes no input and returns the bound
organization.

Favro routes every request by the `organizationId` header
([routing](https://favro.com/developer/#routing)). Its reference
documents the id in the path of
[get an organization](https://favro.com/developer/#get-an-organization)
as required, and the live API ignores it and answers with the
organization the header names. A required input that cannot change the
result reads as a choice and is not one: a model asking for one
organization would be handed another with a 200. The organization is a
deployment fact rather than a per-call one, and the plan that sets the
rate limit is the organization's (D-42). The `one-organization` gate
fails on any tool input that takes an organization id.

Rejected: a server that switches organization mid-session, which would
spend two plans' budgets in one session with nothing to say whose; a
required organization id input, which Favro ignores.
Cost: a person who works in two organizations runs two server processes,
one bound to each.

### D-03 SCIM provisioning and creating or changing an organization are out of scope

SCIM provisioning, in both versions, and the organization writes, `POST`
and `PUT` on `/organizations`, are out of scope.
`testdata/api-coverage.tsv` records each as `out` with its reason, and
the `api-coverage` gate holds every documented endpoint to a verdict
(D-72).

SCIM is an identity provider syncing users and groups on a schedule,
with an authentication model other than the API token this server holds
([SCIM](https://favro.com/developer/#scim-v1-1-provisioning)). Nothing a
model doing work inside Favro reaches for it. Creating an organization
mints a tenant the bound one has no part in (D-02). Updating one renames
it and rewrites its membership and roles, a write whose blast radius is
every user of the organization rather than one widget or card
([update an organization](https://favro.com/developer/#update-an-organization)).

Rejected: SCIM tools, which directory software uses rather than a person
asking about a card; creating or changing an organization, a blast
radius nobody wants.
Cost: membership, roles and the organization itself are changed in Favro
or through an identity provider, never through this server.

### D-04 Own wire types over raw REST; there is no generated Favro client

`internal/favro` hand-writes every request and response type, and
`internal/favroapi` calls Favro's REST API with `net/http`. Favro
publishes a reference page and no machine-readable description of its
API ([introduction](https://favro.com/developer/#introduction)), so
there is no client to generate.

Hand-written types do what a generated client would not. A read accepts
the shapes the live API is seen to send beside the documented one
(D-15), and a write sends exactly the documented shape. Every field
Favro documents for a modeled type has a verdict, so a type that drops a
field is caught offline rather than by a silent gap in a result (D-72).
`internal/favro` imports nothing from this module, so the client, the
service and the tools share it without depending on each other (D-05).

Rejected: a generated Favro client, which does not exist and has no
specification to be generated from.
Cost: every endpoint and field is typed by hand. A field Favro adds
stays invisible until `make api-diff` refetches the reference and
`api-fields` names it.

### D-05 Dependencies run server → tools → service → favroapi → favro; favro, render and auth import nothing from the module, and nothing below tools imports MCP

Imports run one way: `server` to `tools` to `service` to `favroapi` to
`favro`. `favro`, `render` and `auth` import nothing from the module,
and nothing below `tools` imports MCP.

An import that runs uphill between two of these packages is an import
cycle, which `go build` already refuses with a better message.
`depguard` holds what a cycle cannot catch, in four rules:
`favro-imports-nothing`, `render-is-a-leaf`, `auth-is-a-leaf` and
`no-mcp-below-the-tool-layer`. Keeping MCP out of `internal/favroapi`
and `internal/service` is what lets both be tested without a protocol
session. `render` being a leaf is what lets the client's typed errors
name their own class: the vocabulary sits below the package that raises
them (D-40). `auth` stays a leaf because its callers read the
credentials through `config.CredentialsFrom` and hand them over.

Rejected: rules for the directions an import cycle already refuses,
which can never fire; classifying the client's errors from outside by a
switch, which lets an added error type fall through unclassified.
Cost: `config` and `cache` are leaves by convention only, since no rule
names them.

### D-06 Everything the repository runs on itself is Go; the one shell file is the git hook that runs it

Everything the repository runs on itself is Go: the gates, the live
driver, the evals and the plugin and bundle packers. The one shell file
is `.githooks/pre-commit`, which does nothing but exec
`go run ./scripts/gates precommit`.

A shell script is held to no gofmt, vet, lint or test. `make check` runs
on the Windows runner, where bash is a dependency rather than a given. A
script that parses JSON with `sed` is how a quote ends up inside a
string. In Go, a packer and the gate that checks its output read the
same table: the plugin's launcher is generated from the platform table
the `plugin` gate reads, so the two cannot disagree (D-90).

Rejected: shell scripts, held to no gofmt, vet, lint or test, with bash
a dependency on the Windows runner.
Cost: the git hook stays a shell file, because git runs hooks as
programs, and it only hands over to Go.

### D-07 The surface adds what Favro's API lacks: name resolution, client-side search and a dereferenced card read

Beside Favro's endpoints, the surface carries three layers its API does
not have. The `favro_resolve_*` tools turn a name into ranked id
candidates (D-08). `favro_search_cards` is full-text search over one
scope (D-13). `favro_get_card_full` returns a card with a name beside
every id (D-11).

Favro addresses everything by id. A card has a per-widget id, a
cross-widget id and a sequential number, and tags and custom fields are
organization-wide ids with no name index
([card](https://favro.com/developer/#card)). Columns repeat their names
across boards. A wrapper that exposed the endpoints one to one would
make the model list a resource to find each id it needs. That spends the
hourly budget on lookups rather than on work (D-42).

Rejected: a one-to-one REST wrapper, which would spend a conversation's
rate-limit budget turning names into ids.
Cost: the three layers are this server's own code, with ranking, caching
and scoring to keep right. A name can match the wrong thing, so a
resolver returns ranked candidates rather than one id.

### D-08 One resolver per process turns names into ranked id candidates, cached longer for org metadata than for what people add mid-session

One resolver per process turns a name into id candidates, ranked by how
closely each matches, and every tool shares it. It caches each list it
reads. The lists people seldom change, tags, users, custom fields and
groups, are kept longer than collections, widgets and columns, which
people add during a session. The TTLs are in
`internal/service/resolver.go`.

One resolver means one cold start. The first resolution of a tag fills
the tag list, and every later one in the session reads it, whichever
tool asks. `TestResolveTag_RankingScoreScale` holds the ranking to one
scale, so an exact name outranks a partial one. Checked by the live
steps "resolve a tag by name", "resolve a user by name" and "resolve a
column by name".

Rejected: a resolver per tool, each cold-starting its own cache and
spending the budget in parallel.
Cost: a write must invalidate the right cache or the next read is stale
(D-09). A change someone else makes in Favro stays invisible until the
TTL runs out.

### D-09 A write invalidates the caches it changed, and every list and resolve tool takes force_refresh

A write invalidates the caches it changed, after it succeeds. Creating a
tag drops the cached tag list, and creating a card drops the search
corpus of its scope. Every list and resolve tool takes `force_refresh`,
which reads Favro and refills the cache.

A cache that outlives a write answers the next resolution with the world
before it. A model that has just created a tag would be told it does not
exist, and would create it again. Invalidation is wired at each write
rather than left to expiry. `TestInvalidateCacheHooks` holds that after
each hook the next resolution reads Favro again.
`TestMCP_CreateTag_InvalidatesCacheOnSuccess` and
`TestMCP_CreateCard_InvalidatesSearchCacheOnSuccess` hold two writes to
it through the tool.

Rejected: expiry alone, which leaves a write's own result invisible
until the TTL runs out.
Cost: a change someone else makes in Favro is not seen until the TTL
runs out. `force_refresh` is the escape hatch, on every list and resolve
tool.

### D-10 The model addresses by name through a resolver or by Favro's own ids; a card by exactly one of three ids, and each tool says which it takes

The model addresses a thing by name through a `favro_resolve_*` tool, or
by an id Favro gave it. The server hands out no handles of its own and
does no index arithmetic. A card is addressed by exactly one of
`card_id`, the per-widget id, `card_common_id`, the cross-widget id, or
`sequential_id`, the number in the reference people paste from Favro. A
call naming two is refused `[invalid]`.

Favro's endpoints disagree on which card id they take. `GET /cards/{id}`
answers 403 for a common id, which its reference does not say
([get a card](https://favro.com/developer/#get-a-card)). `/comments` and
`/tasklists` filter by the common id, and a card's activities take the
per-widget id. So `favro_list_cards` with `card_common_id` is the path
for that form. Checked by the live steps "read a card in full by common
id" and "list cards by common id".

Rejected: handles or index arithmetic of the server's own, which an API
addressed by ids does not need.
Cost: a wrong id is a 403 rather than a validation error, so each tool's
description names the id it takes and why.

### D-11 favro_get_card_full dereferences every id in parallel in internal/service, and passes an unrecognized custom-field type through undereferenced

`favro_get_card_full` fetches one card and dereferences its ids in
parallel: tags, assignees, the widget and its column, the collections,
the custom fields and the placement of every copy. It runs in
`internal/service`, under `errgroup`, against the resolver's caches, and
returns names beside the ids.

A raw card is a bag of ids, and a model given one spends several calls
turning it into something it can talk about. The fan-out lives in
`internal/service` because it is the repository's one concurrent
orchestration, and concurrency owned by a protocol handler can be tested
only through a protocol session. Every custom-field type Favro publishes
gets a display value
([custom field types](https://favro.com/developer/#custom-field-types)).
A type the server does not know passes through with
`dereferenced: false` and its raw value, so a type Favro adds later
reads as "here is the raw value" rather than as a wrong rendering.

Rejected: the fan-out in `internal/tools`, which only a protocol session
could test; dropping or guessing an unrecognized field type.
Cost: on a cold cache, a full read lists every resource it dereferences
at once, and the first full read of a session is the expensive one.

### D-12 A card looked up by common or sequential id is its board copy, every copy is listed in placements, and unique keeps the board copy

A card looked up by common id or sequential id resolves to its board
copy, the first copy that sits on a widget. Every copy is listed in
`placements`, and `favro_list_cards` with `unique` keeps the board copy
too.

Assigning a card to someone gives it a second copy on their to-do list,
with `todoListUserId` set and no widget or column. A listing by
`cardCommonId` or by `cardSequentialId` puts that copy first, an order
Favro's reference does not give
([get all cards](https://favro.com/developer/#get-all-cards)). With
`unique=true`, Favro keeps the to-do copy by `cardCommonId` and the
board copy by `cardSequentialId`, so its own `unique` is not one rule. A
first-row lookup answers with a card that has no board and no column,
which is not the card a person means. This was probed by hand on a
scratch collection. No live step checks it, since only an assigned card
has a to-do list copy.

Rejected: the first row a listing returns, which is the to-do list copy;
Favro's own `unique`, which keeps the to-do copy by `cardCommonId`.
Cost: a card that sits on no board resolves to its first copy, which has
no column to report.

### D-13 Search is client-side full text over one widget or one collection, scored name before body, and cached per scope

`favro_search_cards` fetches one scope's cards, strips the markdown,
lowercases, and scores the name and the body separately, a name match
above a body match. Exactly one of `widget_common_id` or `collection_id`
is required, and the prepared corpus is cached per scope.

Favro has no full-text search, and `/cards` refuses a listing that names
no widget, collection or card
([get all cards](https://favro.com/developer/#get-all-cards)). So there
is no organization-wide corpus to offer, and the scope is a platform
constraint rather than a design choice. The tool says so in its
description and points at the resolvers, because a model that cannot see
why it was refused retries the same call.
`TestScoreCard_NameBeatsBodyOnly` holds the ordering. Checked by the
live steps "search a widget's cards" and "search a collection's cards".

Rejected: an organization-wide search, which `/cards` cannot serve.
Cost: a search on a cold scope lists every card in it before it scores
one. A card outside the scope given is never found, however well it
matches.

### D-14 Descriptions are read and returned in Favro's stored markdown, and matching is never loosened to its normalization

A description is read in the markdown Favro stores, and returned as
stored. The description editors and search read with
`descriptionFormat=markdown`
([get a card](https://favro.com/developer/#get-a-card)).
`favro_get_card` and `favro_get_card_full` take `description_format` to
show it, and every card write asks for it on the card it returns.

Favro normalizes markdown when it stores it: a `- ` bullet becomes `* `,
and list items gain a blank line between them. Its reference does not
say so. The stored form is what an edit has to be written against, so
`find` in `favro_replace_in_card_description` matches the stored text
exactly. Matching is never loosened to paper over the difference. A
guess at Favro's serializer that went wrong would edit text the caller
did not quote. Checked by the live step "read a card as markdown".

Rejected: loosening matching to Favro's normalization, since guessing
its serializer is worse than showing its output.
Cost: the stored form is not what the caller sent. A caller quoting its
own earlier text can miss, and has to read the card first.

### D-15 A read accepts the documented and every observed shape; a write sends the documented one and says when it is unverified

A read accepts the documented shape and every shape the live API has
been seen to send, preferring the documented one. A write sends the
documented shape, and a tool whose write shape has not been seen to land
says so in its description.

Favro's reference disagrees with the live API in places, and with
itself. Its field table gives a webhook's column scope as `columnIds`,
an array, and every example shows `columnId`, a string
([webhook options](https://favro.com/developer/#webhook-options)).
`Card.CustomFields`, `Tasklist.Title` and `Activity.CommonID` accept
both shapes, and `TestCard_CustomFields_ReconcilesBothWireKeys` holds
one of them. A read that refused an observed shape would fail on real
data. A write has nothing better to send than the documented shape, and
Favro answers 200 to a body it ignores, so the honest move is to say
what is unconfirmed (D-73).

Rejected: reads held to the documented shape alone, which fail on what
the live API sends.
Cost: a write's unverified shape is admitted in its description rather
than fixed. Issue #118 asks to confirm each custom field type's write
shape against Favro, since only a Text field has been seen to take a
write.

### D-16 One call is one page: list tools return next_page and request_id, and nothing aggregates

Every list tool returns one page, with `next_page` and `request_id`, and
nothing pages on the caller's behalf. The caller passes `next_page`
back, and `request_id` with it.

A loop that read every page would spend the hourly budget on rows the
model may never read, and hide where it went (D-42). A model that stops
early is the common case. Inside the server, the resolver and search do
read whole lists, and each is cached so that it is read once (D-08,
D-13).

`requestId` is a field of the response body. Favro's reference asks for
it as a query parameter beside `page`, and for the
`X-Favro-Backend-Identifier` header to carry the value a response
returned in that header
([pagination](https://favro.com/developer/#pagination),
[routing](https://favro.com/developer/#routing)). The client sends the
request id in that header and no `requestId` parameter. Favro serves a
later page from `page` alone, and the request id in the header is not
the backend identifier and changes nothing. This is checked by hand
against a live organization; no live step reads a second page, and
issue #127 asks for one.

Rejected: a hidden aggregation loop in the list tools, expensive and
unattributable.
Cost: a caller passes `next_page` back by hand for every page it wants.

### D-17 Page numbers are 1-indexed on the surface and converted to Favro's 0-indexed pages in the tool layer

Page numbers on this surface start at 1, and the tool layer converts
them to Favro's pages, which start at 0. `favroPage` does it at the
boundary, and `internal/favroapi` stays a faithful client of an API
whose first page is 0
([pagination](https://favro.com/developer/#pagination)).

The schema says 1-indexed because that is what a model and a person
assume. Passing the number through under that schema serves page 1 as
Favro's second page. The caller then never sees the first page, and
nothing in the result says so: it is a valid page of real results with
rows silently missing. The conversion lives in the MCP layer because the
off-by-one is a promise the surface makes, not a property of Favro.
`TestListToolPageNumberingIsOneIndexed` holds every list tool to it.

Rejected: Favro's page number passed through, under which page 1 served
the second page.
Cost: there are two numbering schemes in the code, and the conversion
has to stay in the one place, the tool layer.

### D-18 A description is edited in place by append, prepend or replace-in, each returning the old text, the new and a unified diff

A description is edited in place by `favro_append_card_description`,
`favro_prepend_card_description` or `favro_replace_in_card_description`.
Each returns the old text, the new and a unified diff, so the change is
auditable in the transcript.

`PUT /cards/{id}` takes a whole description
([update a card](https://favro.com/developer/#update-a-card)). So the
obvious tool replaces the body, and its obvious failure is a model
rewriting a card it meant to add a line to, losing what other people
wrote. Each editor reads the stored markdown first (D-14), builds the
new text from it, and sends the whole description back. The golden
corpus in `internal/service/testdata/markdown` holds the editors to
their output, through `TestDescriptionEditors_GoldenCorpus`. Checked by
the live steps "append to a card's description, dry run" and "prepend to
a card's description, dry run".

Rejected: a replace-the-body tool as the way to edit, with which a model
rewrites a card it meant to add a line to.
Cost: `favro_update_card` still replaces a description whole and is
registered by default. An edit someone makes between an editor's read
and its write is lost.

### D-19 replace-in changes one occurrence by default, and sends nothing when find matches nothing

`favro_replace_in_card_description` changes one occurrence of `find`
unless `count` asks for more. When `find` matches nothing, it sends
nothing and answers `[not_found]`.

A common substring otherwise rewrites every place it occurs, most of
them not the one meant, and the diff only shows it afterwards. A write
that changes nothing still answers 200, indistinguishable from one that
worked (D-73). So the no-match case is refused before any request, and
the model learns that its quote did not match the stored text (D-14).
`TestMCP_ReplaceInCardDescription_NoMatch_RefusesPUT` holds it. Checked
by the live step "a description find that matches nothing is
[not_found]".

Rejected: rewriting every occurrence of a common substring by default; a
no-op PUT that answers 200.
Cost: a caller who means every occurrence has to say so with `count`. A
quote that misses costs a read and a second call.

### D-20 The unified diff is the repository's own Myers diff, held byte for byte to diff -u

The `unified_diff` every description editor returns comes from
`internal/service/diff.go`, a Myers diff of the repository's own.
`TestUnifiedDiffMatchesGNUDiff` holds it byte for byte to `diff -u` run
on the same inputs.

The diff is output a model and a person read, so it has to read the way
`diff -u` does. go-difflib printed a stray context line at the end of a
hunk and dropped `\ No newline at end of file`, the one thing a diff
cannot recover from its lines. The common prefix and suffix are trimmed
first, which makes append and prepend almost free. The search is capped
on edit distance, which also bounds its memory.
`TestUnifiedDiffScalesWithChangeNotSize` holds that two long bodies
differing at each end get a small diff.

Rejected: go-difflib, which printed a stray context line and dropped the
no-newline marker; a cap on the size of the trimmed middle, which handed
a model a whole-body diff for a four-line edit to two long bodies.
Cost: an edit past the edit-distance cap falls back to a diff that
deletes and re-inserts the whole body.

### D-21 Tag tools refuse an unknown tag name; creating a tag is favro_create_tag

The tag tools refuse a tag name they cannot match, `[not_found]`, and
never create a tag. Creating one is `favro_create_tag`, explicitly. A
name that matches several tags is `[ambiguous]`, and the caller picks
one by id.

Favro's own `addTags` takes names and creates any it does not know
([update a card](https://favro.com/developer/#update-a-card)). A typo
would then become a permanent tag, visible to the whole organization,
from a call meant to label one card. A tag is organization-wide, so the
cost of the typo falls on everyone who filters by tags afterwards.
`TestMCP_AddTagToCard_HardFailUnknown` holds the refusal. Checked by the
live steps "adding an unknown tag name to a card is [not_found]" and
"removing an unknown tag name from a card is [not_found]".

Rejected: Favro's `addTags`, which creates unknown names and turns a
typo into a permanent organization-wide tag.
Cost: creating a tag is an explicit second call. A model that meant to
create one is refused first and has to say so.

### D-22 A card move names its widget: the client refuses a column or lane move without it, and checks the column Favro returns

A card move names its widget. The client refuses a column or lane move
that lacks `widgetCommonId` before anything is sent, and checks the
column in Favro's answer.

Favro answers a column move without `widgetCommonId` with a 200 and a
stub card that has no column, and the card stays where it was
([update a card](https://favro.com/developer/#update-a-card)). Its
reference does not say the field is required. `UpdateCard` compares the
returned `columnId` with the one asked for and raises
`WriteIgnoredError` when they differ. The read-back then checks what
Favro stored (D-24). The lane is not compared, because a card on a board
without lanes comes back with no `laneId`, and checking it would invent
a failure. `TestColumnMoveContractIsStatedOnce` holds the tools'
descriptions to one statement of the rule. Checked by the live step
"move a card to another column, dry run".

Rejected: `listPosition` as the required field, which has nothing to do
with it; checking `laneId`, which a board without lanes leaves empty.
Cost: Favro publishes no lane endpoint, so a lane move cannot be set up
to probe, and the requirement rests on the column's evidence. A move
echoed in the answer but not stored has not been seen.

### D-23 A structural card write carries the card's current parent and sheetPosition, and clear_parent detaches on purpose

A structural card write carries the card's current parent and
`sheetPosition`, read first. `clear_parent` detaches a card on purpose.

Favro reads `parentCardId` off the body of any write that carries
`widgetCommonId`, so a body naming no parent leaves a nested card at top
level. Its answer shows a null parent either way
([update a card](https://favro.com/developer/#update-a-card)). A rename
is enough, and every column move carries `widgetCommonId` (D-22). Only
the parent is dropped: `columnId` and `listPosition` survive the same
write. Naming the parent has a cost of its own. Favro re-seats a card
whose body names its parent after the parent's last child, unless the
body names `sheetPosition` too. So `favro_update_card` and
`favro_move_card` carry both, report them in `notes`, and read the card
back. This was probed by hand. Checked by the live steps "update a card
structurally without naming its parent, dry run" and "move a card naming
its own parent, dry run".

Rejected: sending the caller's body as given, which detaches a nested
card on a rename; re-sending the card's whole placement, which answers a
broader loss than Favro shows.
Cost: every structural write reads the card first, one more request, and
a dry run reads it too.

### D-24 A write Favro may accept and drop is read back, and its verdict is a note on a successful result

`favro_move_card` and `favro_update_card` read the card back after a
column or lane write, and `favro_set_card_custom_field` after a field
write. The verdict is a note on a successful result.

A 200 from Favro is not confirmation (D-73). A write to a custom field
the card's widget has not enabled answers 200 with the full card and the
field absent
([card custom fields](https://favro.com/developer/#card-custom-fields)).
A move's own answer comes from the request handling that may have
ignored the body, so only a later read is evidence. The comparison stays
per path, since "is my column the returned column" and "is my field
there" are different questions. It is a note rather than an error
because the write was accepted, and because clearing a field leaves it
absent too. `skip_verify` skips the read for a caller about to read the
card anyway.

Rejected: one shared confirm helper, a shape nothing fits;
`WriteIgnoredError` on a custom field, since only a Text field has been
seen to take a write; a pre-check on `CustomField.widgetCommonId`, which
would refuse writes that land.
Cost: a read-back is one more request. No live step removes an
attachment, and Favro answers 200 whether or not the URL matched.
Issue #119 asks for a `-writes` check that removes one.

### D-25 No tool states an outcome the response did not carry

No tool states an outcome its response did not carry. A result reports
what Favro answered, and a sentence about what is true after the call
needs the response, or a read-back, behind it.

Favro answers 200 to a body it ignores (D-73), so success text written
from the request asserts what nothing confirmed, and a model passes it
on to the person as fact. The `outcomes` gate holds the one shape of
this a machine can check: a branch that tests a boolean field of the
request and then states what is true afterwards. A branch that consults
Favro, refuses, or words its sentence as "would" is honest, and the gate
recognizes all three by structure.
`TestOutcomesCatchesAnAssertedOutcome` holds the gate to a planted case.

Rejected: success text written from the request, which states what the
caller asked for as what happened.
Cost: the gate cannot judge words, so a flagged branch that is right
anyway takes a row in `testdata/outcome-claims.tsv` with its reason.

### D-26 Moving a card to another widget adds an instance there, and the tool says so; no move is assembled from an add and a delete

`favro_move_card` with another widget adds an instance of the card
there, and its description says so: the source instance stays where it
is.

Favro's model is one card common to several widgets, with an instance on
each under one `cardCommonId`. A PUT that names another widget adds an
instance, and there is no call that relocates a card
([update a card](https://favro.com/developer/#update-a-card)). Removing
the source is a separate `favro_delete_card` on that widget's instance
(D-37). This server does not assemble a move from the two. A half-failed
pair leaves a duplicate or a lost card, and the caller could not tell
which from one result. Checked by the live step "move a card across
widgets with every positioning option, dry run".

Rejected: an add then a delete, this server inventing a transaction
Favro does not offer, whose half-failure leaves a duplicate.
Cost: a caller who means "move" deletes the source instance as a second
call, which is destructive and needs `FAVRO_ENABLE_DESTRUCTIVE`.

### D-27 A group update sends the whole intended member list

`favro_update_group` sends the whole intended member list.

Favro's reference describes a group update as add and remove deltas,
each entry with a `delete` flag
([update a group](https://favro.com/developer/#update-a-group)). A live
test observed the whole list being replaced. Sending the full list is
correct under either reading, since a change that names every member is
the list. Before it sends, the tool reads the group's members, so a
member re-sent as they are widens nothing, and only someone who has
access can be removed (D-35). Checked by the live steps "replace a
group's members, dry run" and "adding a user outside the organization to
a group is [blocked]".

Rejected: deltas with a per-entry `delete` flag, as the reference
describes, which would leave a group holding only the change if Favro
replaces.
Cost: leaving someone out of the list removes them. Issue #121 asks
whether `PUT /groups` replaces a group's members or applies a change; if
it replaces, the tool takes members as add and remove lists.

### D-28 Every write takes dry_run, enforced in the HTTP client, and --dry-run forces it for the process

Every write takes `dry_run`, and `--dry-run` forces it for the whole
process. A dry run returns the request it would have sent, method, URL,
headers with the credentials redacted, and body, and sends no write.

The gate is in the HTTP client: `shouldDryRun` reads the per-call flag
from the context `addTool` puts it on, and the process flag from the
client. A tool cannot forget a gate it never holds.
`TestEveryWriteTakesDryRunAndSendsNothing` holds that every tool without
`readOnlyHint` takes `dry_run` and that its dry run sends no write. A
dry run still reads: a structural write reads the card, so the previewed
body is the one that would be sent (D-23). The live driver sends every
write step with `dry_run` (D-74).

Rejected: the gate in each tool, which a tool could forget.
Cost: `dry_run` is not a second guard against an unattended call. The
model sets it, and a client that runs with nobody watching runs the real
write (D-29).

### D-29 The destructive tools register only with FAVRO_ENABLE_DESTRUCTIVE=true, and a tool is destructive when its input takes confirm

A tool is destructive when its input takes `confirm`, and the
destructive tools register only with `FAVRO_ENABLE_DESTRUCTIVE=true`.
`addTool` reads the input at registration, so there is no second list of
names.

A host in an auto-approve mode runs an annotated tool without prompting,
and the MCP specification tells clients to treat annotations as
untrusted
([tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)).
The tool that cannot run unattended is the one not registered. A list of
destructive names would miss exactly the tool added by somebody who did
not know the list existed. `TestDestructiveToolsAreOptIn` derives the
set from the `confirm` input on the live surface.
`TestEveryWriteIsClassified` names every write as one that adds, changes
or destroys, and only the last takes `confirm`, so a delete written
without it fails the build.

Rejected: `dry_run` as the only guard; a list of destructive tool names;
`destructiveHint` as the gate, which is true on every write that changes
something, most of which lose little.
Cost: the destructive tools are off the default surface, and a deployer
who wants one of them enables all of them.

### D-30 A destructive call needs confirm: true unless it is a dry run

A destructive call needs `confirm: true`. `addTool` refuses one without
it `[blocked]`, unless it is a dry run, which writes nothing.

`confirm` is a witness that the caller meant the call, never approval:
the model sets it. What it buys is that a destructive call is one the
model has to name, and the refusal tells it why. Registration panics on
a tool that takes `confirm` without `destructiveHint`, or carries the
mark without `confirm` (D-34). `TestNoWriteWithoutConfirmSendsADelete`
backs it with what each write previews: no write without `confirm` may
preview an HTTP DELETE. Checked by the live steps "favro_delete_tag
without confirm is [blocked]" and "favro_delete_collection without
confirm is [blocked]".

Rejected: registration as the only guard, under which every registered
destructive tool runs on any call with nothing said about meaning it.
Cost: the model sets `confirm`, so it is a witness and never approval. A
client running with nobody watching passes it as readily as anything
else (D-31).

### D-31 The server asks the person nothing; per-call approval is the client's

The server puts no question to the person, and per-call approval is the
client's.

The MCP specification gives consent to each tool call to the host, and
lets a client answer an elicitation "from the user or other sources"
([elicitation](https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation)).
Clients answer such a question with nobody there, never draw it, or
leave it hanging, and no client's allow and ask rules match it. A
question on every call is answered without reading. So what holds on
every client is in the server: registration, `confirm`, the sharing
refusals, the witnesses and `dry_run` (D-29, D-30, D-35, D-36, D-28).
The in-memory harness's client declares form elicitation and fails any
test whose call puts a question to it. Checked by the live step "the
server asked the person nothing".

Rejected: the server's own question to the person through form
elicitation (D-94).
Cost: a client that runs with nobody watching gets whatever is
registered, so `FAVRO_ENABLE_DESTRUCTIVE` stays off for one.

### D-32 FAVRO_REQUIRE_PROMPT refuses start unless false, and FAVRO_INTERACTION_HINT is ignored with a warning

`FAVRO_REQUIRE_PROMPT` set to anything but `false` refuses the start,
and `doctor` reports it as a failed check. `FAVRO_INTERACTION_HINT` set
to `false` is ignored with a startup warning that `doctor` repeats.
`config.Vars` lists both as removed settings.

Both configured the server's question to the person, which it does not
put (D-31, D-94). `FAVRO_REQUIRE_PROMPT` refused the asking writes on a
client that could not ask. Ignoring it would quietly allow what its
owner set the server to stop, so the server refuses to start instead.
`FAVRO_INTERACTION_HINT=false` withheld Claude Code's mark. Ignoring it
only makes the server stricter, so a warning is enough, and the warning
says why Claude Code prompts for the marked tools (D-34).
`TestRunServer_ARemovedSettingRefusesToStart` holds the refusal.

Rejected: quietly allowing what the owner set the server to stop.
Cost: a headless deployment that relied on
`FAVRO_INTERACTION_HINT=false` finds Claude Code refusing the marked
tools.

### D-33 readOnlyHint is on the reads alone, and destructiveHint is false only on a write that adds

`readOnlyHint` is true on the reads and on nothing else.
`destructiveHint` is true on every write that changes or removes
something. It is false only on a write that creates something and
changes nothing already there: a card, collection, column, comment,
group, tag, task, task list, widget or attachment.

The MCP schema reads false as "the tool performs only additive updates"
([ToolAnnotations](https://modelcontextprotocol.io/specification/2025-11-25/schema#toolannotations)).
Clients prompt by these hints: claude.ai and Codex by `destructiveHint`,
and VS Code on every tool without `readOnlyHint`. A false hint on an
update would let a client skip the prompt on a write that overwrites a
description. The hint is not the destructive gate, which reads `confirm`
(D-29), so the updates stay registered by default.
`TestOnlyToolsThatAddSayTheyAreNotDestructive` names each write that
adds, and `TestEveryToolIsAnnotated` keeps `readOnlyHint` off every
write.

Rejected: false on a write that loses little; true on the destructive
tools alone.
Cost: a client that prompts by `destructiveHint` prompts on every
update, move and edit. How Claude Desktop reads the hints has not been
tried, which #124 covers.

### D-34 Five tools carry Claude Code's requiresUserInteraction, and only a destructive tool may

Claude Code's `anthropic/requiresUserInteraction` mark sits on
`favro_delete_tag`, `favro_delete_group`, `favro_delete_collection`,
`favro_delete_widget` and `favro_widen_collection_sharing`. Only a
destructive tool may carry it, and `addTool` refuses the mark on any
other.

Each of these reaches past one item. A tag comes off every card that
carries it, and a group off every share, assignment and custom field
that names it. A widget takes its cards with it, and a collection leaves
widgets that lived only in it where nobody finds them. A widening opens
a collection to the organization or to anyone with its link. Claude Code
from v2.1.199 prompts on every call of a marked tool in every
interactive mode, and denies it where no prompt can be drawn
([Claude Code MCP](https://code.claude.com/docs/en/mcp)).
`TestOnlyTheWidestWritesPrompt` holds the set.

Rejected: the mark only for a client that cannot ask; the mark on every
delete, since a delete made many times in a row brings back a prompt
nobody reads.
Cost: Claude Code denies a marked tool where no prompt can be drawn, as
in `dontAsk` mode or `-p` without a permission host. For every other
write, the operator decides with Claude Code's `permissions.ask`.

### D-35 The ordinary writes share only inside the organization, and refuse every widening as [blocked]

The ordinary writes share only inside the organization, and refuse every
widening `[blocked]`. They refuse `public_sharing` of `organization` or
`public` on a collection, and a share or member entry that names someone
by email or by `type: 'organization'`. They refuse a userId the
organization does not list, or whose `organizationRole` is not
`administrator` or `fullMember`
([organization roles](https://favro.com/developer/#organization-roles)).
They refuse a new collection admin or group administrator, guests as a
widget's owners or editors, and a group holding anyone outside those two
roles.

A client remembers an approval per tool, never per argument. A wide
share carried as an argument therefore rides along unseen once the tool
is always allowed. An entry that gives someone no more than they hold
widens nothing and goes through, since an update reads the existing
shares first. Checked by the live steps "sharing a new collection by
email is [blocked]" and "opening a collection to the organization on an
update is [blocked]".

Rejected: widening as an argument of an ordinary write, which a client's
per-tool approval carries unseen.
Cost: an invitation by email, a new admin and guests as a widget's
owners or editors have no tool, and an unknown role refuses a member
rather than admitting a guest. Issues #122 and #123 ask what Favro does
with an email it does not know, and whether a share with a group that
holds a guest is refused, which the live run's organization cannot try.

### D-36 Opening a collection to the organization or to anyone with its link is favro_widen_collection_sharing, witnessed by the collection's name and read back

Opening a collection to the organization, or to anyone with its link, is
`favro_widen_collection_sharing`. It is destructive, so it registers
only with the flag, needs `confirm: true` and carries the mark (D-29,
D-30, D-34).

It also takes `collection_name`, which must match the name Favro holds,
and a call naming another is refused `[conflict]`. A client's prompt
shows the arguments, so the name in the prompt is the collection being
opened. After the write the tool reads the collection back and says in
`notes` whether the mode took
([update a collection](https://favro.com/developer/#update-a-collection)).
`organization` counts as a widening, as a domain-wide share does in
google-drive-mcp. Checked by the live steps
"favro_widen_collection_sharing with another name is [conflict]" and
"favro_widen_collection_sharing opens it to the organization and reads
it back".

Rejected: `public_sharing` on the ordinary collection writes, which a
client's standing approval of those tools would carry unseen (D-35).
Cost: an organization-visible collection needs
`FAVRO_ENABLE_DESTRUCTIVE`. A public share is never tried live, since
making a real collection public is the act it would test.

### D-37 The wider delete is an argument of the same delete tool, and favro_delete_widget refuses a call that names no scope

`favro_delete_card` and `favro_delete_widget` take `everywhere: true`
for the delete from every widget or every collection.
`favro_delete_widget` refuses a call that names neither `collection_id`
nor `everywhere`.

Favro's deletes are scoped. A card delete removes one widget's instance
unless `everywhere` is set
([delete a card](https://favro.com/developer/#delete-a-card)), and a
widget belongs to several collections
([delete a widget](https://favro.com/developer/#delete-a-widget)). Both
variants are deletes, behind the same flag and `confirm: true`, so a
client that always allows the tool has already allowed a delete. The
wider one is named in the arguments a client's prompt shows. A widget
delete that named nothing would get the wider delete by omission, so it
is refused. Checked by the live steps "delete a widget from every
collection, dry run" and "delete a widget from a collection, dry run".

Rejected: the wider delete as a tool of its own, as google-chat-mcp
makes every risky variant.
Cost: a client that always allows the tool has already allowed the wider
delete. A collection delete does not cascade and can leave widgets that
lived only in it unreachable.

### D-38 The upload tools read only inside FAVRO_UPLOAD_DIR, through os.Root, and are not registered without it

The upload tools read only inside `FAVRO_UPLOAD_DIR`, and are not
registered without it. The value must be an absolute path to a directory
that exists; anything else is logged at start and leaves the upload
tools off.

A card's text can suggest a path to a model, and Favro is a network
peer, so a tool that reads any file the account can read is an
exfiltration channel. `file_path` is relative to the directory or
absolute within it, and the read goes through `os.Root`, which refuses a
`..` or a link that leaves it ([os.Root](https://pkg.go.dev/os#Root)).
The size cap and the regular-file check come before any byte is read,
and the file is opened only after a stat says it is regular, since
opening a named pipe waits for a writer. A refusal names `file_path`,
not the path. The cap sits below Favro's documented limit
([upload a file to card](https://favro.com/developer/#upload-a-file-to-card)).
Checked by the live steps "uploading a path outside the upload directory
is [invalid]" and "uploading a folder is [invalid]".

Rejected: reading any file the account can read, an exfiltration
channel.
Cost: anything in the directory can be sent on the model's word, so the
directory holds only what may leave.

### D-39 No tool writes the server's own settings: the environment, a file or the keyring

No tool writes the server's own settings: the environment, a file or the
keyring.

The model reads card text other people wrote. A tool that could change a
setting would let that text widen the model's reach: turn on the
destructive tools, move the upload directory or swap the credentials.
`TestNoToolWritesTheServersOwnSettings` follows every package the tools
import. It refuses a call that sets the environment, writes a file or
reaches the keyring outside `auth.KeyringSource`, which none of those
packages may name. It also refuses an assignment to the registry's flags
after `Register`. Settings change when the deployer restarts the server
with other ones, and only `favro-mcp auth`, at a terminal, writes the
keyring (D-55).

Rejected: a tool that changes a setting for the session, such as
enabling the destructive tools on request, which the model could be
talked into calling.
Cost: only `auth.KeyringSource` writes the keyring, and only
`favro-mcp auth` uses it, so any change of settings means restarting the
server.

### D-40 Every tool error is `[class] message` from one closed vocabulary, classified by the error's type and never its text, with invalid as the fallback

Every tool error is a result of the form `[class] message`, from one
closed vocabulary declared in `internal/render/class.go`. The class
tells the caller what to do next, so a caller can switch on it, and
[TOOLS.md](TOOLS.md#error-classes) says what each class asks.

`render.Classify` reads the class from the error's type with
`errors.As`, never from its text. Each typed error in
`internal/favroapi` names its own class, and a sentinel is built with
`render.Sentinel`. An error that names no class falls back to `invalid`.
That is measured rather than neutral: every error the server's own layer
raises without a sentinel is an argument the caller got wrong.
`TestEveryErrorTypeNamesItsClass` reads the client's error types, and
`TestEverySentinelIsClassified` reads `internal/tools`,
`internal/service` and `internal/favroapi`. The `classes` gate holds the
vocabulary and its documentation to each other from both sides.

Rejected: matching on an error's text; an `unverified` class (D-93).
Cost: a server bug reaching the fallback is mislabeled, and an inline
`fmt.Errorf` in a handler is checked by nothing. Issue #126 asks whether
`WriteIgnoredError` needs a class of its own, since `[unavailable]`
tells a model to retry a write Favro will ignore again.

### D-41 A 429 is retried once when it says to wait 30 seconds or less, and a read's 5xx up to three attempts; a write is sent once, and a 5xx or a failure after the dial is ambiguous_outcome

A 429 is retried once, and only when it carries a `Retry-After` of 30
seconds or less. Favro's reference documents no `Retry-After`, so a 429
without one is returned at once as `[rate_limited]`
([throttling](https://favro.com/developer/#throttling)). A read's 5xx
gets up to three attempts, with the waits in `transientBackoffSchedule`
between them. A network failure is not retried.

A write is sent once. Its 5xx, or a connection lost after the request
went out, is `[ambiguous_outcome]`. A dial that failed sent nothing and
stays `[unavailable]`. Favro does not deduplicate writes: a repeated
POST creates a second card, and a repeated DELETE answers 404 for the
card the first one removed. The HTTP timeout applies to each attempt, so
a retried read can take longer in all (D-43).

Rejected: retrying every method on a 5xx, which created twice and
retried a DELETE into its own 404.
Cost: a write that may have landed is reported and never repeated. The
caller reads the resource to see what happened.

### D-42 Every response's rate-limit headers are recorded, and favro_rate_limit_status reports the last snapshot without a call

Every response's rate-limit headers are recorded, and
`favro_rate_limit_status` reports the last snapshot without a call. The
snapshot holds the limit, what remains, when the window resets, and
`X-RateLimit-Delay`
([response headers](https://favro.com/developer/#response-headers)).

Favro counts calls per user per hour: 50 on the user-level routes, and
on every other route a number the organization's plan sets, Trial 100,
Lite 0, Standard 1000 and Enterprise 10000
([rate limiting](https://favro.com/developer/#rate-limiting-and-throttling)).
Its table labels those per user, and its text ties them to the plan. It
delays a response before it refuses one, and answers 429 once the needed
delay would pass 10 seconds. A non-zero delay is the warning that comes
first, and a model that can see it can slow down. The reference's table
spells the first header `X-RateLimit-Limt`, and the client reads
`X-RateLimit-Limit`. The live step "read the rate limit status" reads
the snapshot and asserts nothing about it.

Rejected: a status tool that spends a call to read the headers, which
costs a call from the budget it reports.
Cost: the snapshot is as old as the last call, and is empty before the
session's first one.

### D-43 Every HTTP call has a timeout, the startup check and doctor's live half included

Every HTTP call has a timeout. The client sets one on each attempt, the
startup check runs under a shorter one of its own, and `doctor`'s live
half has one across all its requests.

`forbidigo` refuses `http.DefaultClient`, and the package-level
`http.Get`, `http.Head`, `http.Post` and `http.PostForm` that send
through it, because it has no timeout. A host that never answers would
otherwise hold a tool call until the client gives up. At start it would
hold the server, and a client waits on the startup check before it shows
any tool. `TestDefaultValidator_HasReasonableDefaults` holds the
validator's timeout.

Rejected: `net/http.DefaultClient`, which has no timeout.
Cost: the timeout is per attempt, not per call. A read retried after a
5xx can run past it, bounded only by the caller's context (D-41).

### D-44 A host closing the pipe is a clean exit, matched by the SDK's error code

A host closing the pipe is a clean exit, and `cleanDisconnect`
recognizes it by the SDK's error code.

The SDK reports a closed connection as JSON-RPC -32004, with the EOF
only in the message text
([wire.go](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/internal/jsonrpc2/wire.go#L33-L36)).
`errors.Is(err, io.EOF)` cannot match it, and an unmatched error exits
1, which every host logs as a crash. `jsonrpc.Error` is a public alias
of the SDK's wire type, so `errors.As` reaches the code without reading
a string that is free to change. The `smoke` gate closes stdin the
moment the last message is written, because a run that waits first never
sees the bug. `TestCleanDisconnect` holds the match.

Rejected: `errors.Is(err, io.EOF)`, which cannot match the SDK's
closed-connection error.
Cost: the match rests on the SDK's code for a closed connection. An SDK
that changed it would turn every disconnect into a crash, which the
`smoke` gate would catch.

### D-45 Stdout carries only MCP JSON-RPC frames, and logs go to stderr through slog

Stdout carries only MCP JSON-RPC frames, and every log line goes to
stderr through `slog`.

The stdio transport says a server must not write anything to stdout that
is not a valid MCP message
([stdio](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#stdio)).
A client reads each line of stdout as a frame, so one stray print breaks
the session, often with no message a person can read. `main` is the one
place that names the process's streams, and everything below it takes an
`io.Writer`. `forbidigo` refuses `fmt.Print*` and `os.Stdout`, matching
the destination rather than the function's name, since an `fmt.Fprintf`
to stdout corrupts the stream identically. `TestForbidigoGuardsStdout`
fails if the rule leaves the linter's settings, and the `smoke` gate
drives the built binary over stdio.

Rejected: logs or diagnostics on stdout, which a client reads as a
broken frame.
Cost: every package below `main` takes its writer as a parameter.
`scripts/` is outside the `forbidigo` rule, since it is maintainer
tooling run at a terminal.

### D-46 A log line carries the method, the path's shape, the query's parameter names, the attempt and the outcome, never a payload or a value that identifies the subject

A log line carries the method, the path's shape, the query's parameter
names, the attempt and the outcome. It never carries a payload or a
value that identifies the subject.

Favro's query strings carry `cardCommonId`, `widgetCommonId` and
`sequentialId`, and every get-one path is `/cards/{cardId}`, so a logged
URL reconstructs which cards a session touched. `redactPathIDs` keeps a
segment only if it reads as part of an endpoint, lowercase alphanumeric
and shorter than a Favro id, and prints `{id}` for anything else. The
`Authorization` and `organizationId` headers are redacted where headers
are rendered, for the debug line and the dry-run record alike. The
startup line names the version and the credential source, never the
organization. `TestDebugLogNeverCarriesTheSubject` captures every record
at `LevelDebug`, over a list call and a get-one call, and fails on any
marker anywhere.

Rejected: logging the query string or the path whole; a test over one
call site or at the default level, which passes against a leak at
another.
Cost: there is no response log, not even a status code or an elapsed
time, so a failure is diagnosed from its error.

### D-47 The token is never printed: not in a result, a log, a dry-run record, auth status or doctor

The token is never printed: not in a result, a log, a dry-run record,
`auth status` or `doctor`.

The token is the whole credential, and a printed line ends up in a log
file, an issue or a transcript. The `Authorization` header is redacted
at the one place headers are rendered, which the debug line and the
dry-run record share (D-46). `auth status` reports the source and the
organization. `doctor --show-ids` prints ids and addresses in full and
still not the token (D-48). Each output has a test:
`TestAuthStatus_ReportsSourceAndOrgButNeverTheToken`,
`TestAuthLogin_StoresInKeyringWithoutEchoingTheToken`,
`TestMCP_FavroPing_OutputContainsNoSecrets` and
`TestDoctorShowIDsPrintsIDsButNeverTheToken`.

Rejected: printing a prefix of the token to identify it, since a prefix
of a secret is part of a secret.
Cost: nothing the server prints tells two tokens apart, so a person
checks which token is in use by its source, through
`favro-mcp auth which`.

### D-48 doctor's report is written to be pasted in public: the values it hides are registered with the redactor by kind, and --show-ids prints them for the person's own screen

`doctor`'s report is written to be pasted into a public issue, which is
what the bug form asks for. Every line goes through `internal/redact`,
and the values `doctor` knows, the address and the ids, are registered
with the redactor by kind. `--show-ids` prints the same report
unredacted, for the person's own screen.

Registered by kind, an address and an id print as different
placeholders, stable within a run. So a report still shows whether the
bound organization is one of the ids the token can see, which is the
whole diagnosis. Patterns alone pass a value of another shape, and the
ids `doctor` prints arrive from Favro rather than from configuration.
The count of redactions counts substitutions that fired, not
registrations. `TestLiteralRedactsWhatNoPatternMatches` and
`TestCountReportsWhatItRedacted` hold both.

Rejected: the address and the id passed as literal secrets, which
flattened three values to one placeholder; patterns alone, which pass a
value of another shape; counting a registration as a redaction.
Cost: `--show-ids` prints ids and addresses in full, so its output
belongs on the person's own screen and never in an issue.

### D-49 Nothing from a real tenant enters the repository: fixtures are synthetic and name resources by role, and leaks scans the tree

Nothing from a real tenant enters the repository: organization names or
ids, card names, ids or sequential ids, user names or addresses, tokens,
or any content from a real Favro organization. That holds in code,
fixtures, tool descriptions, commit messages, pull requests, the
CHANGELOG and the docs.

The repository is public, and a pushed commit cannot be taken back.
Fixtures are synthetic and name resources by their role, and
`internal/favroapi/favroapitest` holds the one invented credential
triple. The `leaks` gate scans the working tree, and the pre-commit hook
runs it. It anchors on shapes the server's own values cannot take: an
address at a domain, a 24-hex id, a keyword right before an id, a link
into the app. Its exemption lists are asserted to occur, so an exemption
that exempts nothing fails. `TestFindLeaksCatchesEachShape` plants each
shape.

Rejected: a scanner as the whole guard, since a card name is ordinary
words no pattern matches; an allowed prefix to quiet a true finding; an
exemption list trusted rather than asserted.
Cost: the scan cannot catch a name, so the rule rests on synthetic
fixtures and review as much as on the gate.

### D-50 The history scan runs by hand and the history is not rewritten; each known finding is accepted in its own commit by hash, rule and count

`make leaks-history` scans every blob and commit message in the history,
by hand, and the history is not rewritten.

Rewriting a public history with released tags on it breaks every tag and
every install pinned to one. Commit messages that carry real findings
keep them. `knownFindings` in `scripts/gates/leaks.go` accepts each such
finding in its own commit, by hash, rule and count, and never writes the
value down. The same value in any other commit, blob or file is still a
finding. Both scans allow the public trailer addresses of Claude's
`Co-Authored-By` and Dependabot's `Signed-off-by`, each as the whole
address. The history scan reads an annotated tag from its message down,
since the tagger header is the maintainer's own identity, published as
every commit's author is. `TestKnownFindingIsPinnedToItsCommit` holds
the pinning.

Rejected: rewriting a public history with released tags on it.
Cost: the findings stay in the history for good. The history scan is
outside `make check`, because its findings are facts about the past
rather than something a commit can fix.

### D-51 The live driver and the evals print only through internal/redact, and a transcript is never committed

The live driver and the evals print only through `internal/redact`, and
a transcript is never committed.

The `transcript` gate holds both drivers, `scripts/livefavro` and
`scripts/evals`, to one rule: only the printer may name `os.Stdout` or
`os.Stderr`, and the printer redacts. It reads every Go file git lists
under a driver that the driver's build compiles, with the tags its make
target runs it with, a package under it included. A rule over writes to
`os.Stdout` misses a driver that wraps the terminal in a buffered writer
once and prints through it.

The redactor catches what has a shape: a 24-hex id,
an address, a link, a signed attachment URL. A card's name is ordinary
words, and a pattern that caught it would catch the sentence around it.
A verbose run therefore shows real names. That is right in a
maintainer's terminal, showing them an organization they hold a token
for, and wrong in a file.

Rejected: a rule over writes to `os.Stdout`, which a buffered writer
hides; one hard-coded driver directory, which leaves another driver
printing raw.
Cost: names pass the redactor, so a transcript is never committed, and
the `leaks` gate cannot catch one that is.

### D-52 The webhook signing secret has no field, so no result can carry it

The webhook type has no field for its signing secret, so no result can
carry it.

Favro returns the secret that signs the `X-Favro-Webhook` header on
every read of a webhook
([webhook signatures](https://favro.com/developer/#webhook-signatures)).
Anyone holding it can forge an event to whoever consumes that webhook.
`encoding/json` drops a field the type does not declare, so the secret
never reaches a value a tool could return. Having nowhere to put a value
is the strongest way not to leak it to a model.
`testdata/api-fields-waived.tsv` records the field as unmodeled with
this reason, and `TestWebhookSecretIsNeverDecoded` holds it.

Rejected: modeling the secret and redacting it in results, which leaves
it one forgotten redaction away from a result.
Cost: that no secret reaches a result rests on the type, never on a real
payload, since the organization the live run reads has no webhook. The
webhook issue, #120, belongs to D-67.

### D-53 Favro is the only network peer; there is no telemetry, crash reporting or update check

Favro is the only network peer. There is no telemetry, no crash
reporting and no update check.

The server holds a credential and reads card text that is often
confidential. A second destination would be a second place for both to
leak, and one the person did not choose. The client calls
`https://favro.com/api/v1` and nothing else, and every request carries
the person's own credential to Favro alone. A person learns of a release
from the repository's releases, not from the server. The threat model in
[security.md](security.md) rests on this.

Rejected: telemetry, crash reporting and an update check.
Cost: the maintainer learns of a failure only from the issues people
file, which is why `doctor`'s report is written to be pasted into one
(D-48).

### D-54 Favro is authenticated by HTTP Basic, an address and an API token; there is no OAuth flow

Favro is authenticated by HTTP Basic, with the user's address and an API
token, and every request carries the `organizationId` header
([authentication](https://favro.com/developer/#authentication)). There
is no OAuth flow.

Favro publishes no authorization endpoint for its REST API, so the
loopback sign-in that the Google servers run has nothing to talk to.
What carries over from their credential handling does. The token lives
in the OS keyring with an environment override (D-55).
`favro-mcp auth login` stores it, `auth logout` deletes it, and
`auth which` says which source won. The token is never printed (D-47).
`Token` in `internal/auth` sets the header and the organization on every
request, and `TestValidator_AuthorizationHeaderIsBasic` holds the
header.

Rejected: the loopback OAuth flow with PKCE that google-docs-mcp uses,
which Favro does not offer.
Cost: the token carries its user's own permissions with nothing narrower
to ask for (D-56), and a person creates it in Favro by hand
([tokens](https://favro.com/developer/#tokens)).

### D-55 Credentials resolve from the environment, then the OS keyring; the first complete triple wins, and a partial one is an error

Credentials resolve from the environment, then the OS keyring: macOS
Keychain, Windows Credential Manager or the Linux Secret Service. The
first complete triple of address, token and organization id wins, and a
partial one in the environment is an error.

Falling through a partial environment to the keyring would run with the
keyring's token under another address or organization, a mix nobody
configured. An error says which variable is missing, while a fallthrough
would only look like the wrong account. `favro-mcp auth which` says
which source won. A keyring entry that cannot be decoded is an error
too, rather than a reason to look elsewhere.
`TestDefaultSources_OrderIsEnvThenKeyring` and
`TestRunServer_PartialEnvCredentials_Errors` hold the order and the
refusal.

Rejected: falling through a partial environment to the keyring, which
would run with the keyring's token under another address.
Cost: `auth which` says only "(no credentials configured)" when neither
source holds a complete triple, so it does not say which part is
missing.

### D-56 Read-only use is the destructive flag left off plus --dry-run, and the token comes from a least-privileged user

Read-only use is the destructive flag left off plus `--dry-run`. The
token comes from a dedicated Favro user with the least access the work
needs.

A Favro API token carries the issuing user's own permissions, and cannot
be narrowed when it is issued
([tokens](https://favro.com/developer/#tokens)). So there is no
read-only scope to request. With `FAVRO_ENABLE_DESTRUCTIVE` off the
deletes are not registered (D-29), and `--dry-run` turns every remaining
write into a preview (D-28). The token's reach is the user's reach, so
the mitigation is choosing that user, which the README and
[security.md](security.md) advise. The server is not a guard on what the
token may do in Favro.

Rejected: read-only scopes, which a Favro token cannot carry.
Cost: the token carries the issuing user's own permissions. Whatever
that user can do in Favro, a leaked token can do too.

### D-57 Configuration is the environment: every variable is in config.Vars and read through config.Lookup, with the spaces around it dropped

Configuration is the environment. `config.Vars` lists every variable the
server reads, the removed ones and the three credentials included, and
every read goes through `config.Lookup`.

`config.Lookup` drops the spaces around a value, so a value of spaces
alone is unset. Outside `internal/config` a variable is named by
config's constant, never typed. One registry gives "what does this
variable do" a single answer, and lets the `staleness` and `mcpb` gates
derive their lists from it (D-78, D-89). `config.LoadFrom` reads every
setting once at start. The binary binds `--dry-run` and `--dump-schemas`
and no other flag, so no flag mirrors a variable.
`TestEveryVariableTheCodeNamesIsInEnvVars` scans the source for `FAVRO_`
names, and `TestVarsDescribeWhatLoadDoes` holds each entry to what
`config.LoadFrom` does with it.

Rejected: a variable's name typed outside `internal/config`.
Cost: `config.Vars` records what each reader reads from an empty
environment, so a variable read only when another is set would escape
it. The source scan is the backstop.

### D-58 The server refuses to start without a credential Favro accepts, and FAVRO_MCP_SKIP_VALIDATE skips the live check

The server refuses to start without a credential Favro accepts. One
request validates the triple at start, under a short timeout of its own
(D-43). A missing credential, a rejection or an unreachable Favro logs
its reason to stderr, naming the variables to check, and exits non-zero.
`FAVRO_MCP_SKIP_VALIDATE` skips the live check.

Credentials are a deployment fact, and the person who can fix them is
the one reading the host's log at start. A server that started anyway
would answer every call with `[auth]`, and the model would relay a
stream of failures to a person who cannot act on them in the
conversation. This differs from google-docs-mcp, which starts without a
working credential and answers `[auth]`.
`TestMain_ExitsNonZeroOnStartupFailure` and
`TestRejectedCredentialsNameTheVariables` hold the exit and its message.

Rejected: starting anyway and failing on each call with `[auth]`, as
google-docs-mcp does.
Cost: a host shows "failed to connect", and the reason is on stderr, in
a log the person has to find. `doctor` says the same thing at a terminal
(D-59).

### D-59 doctor reports the build and its version source, the credential source, whether Favro accepts the credentials and whether the organization binding is right

`favro-mcp doctor` reports the build and where its version came from,
where the credentials came from, whether Favro accepts them, and whether
the bound organization is one the token can see. It exits non-zero when
a check fails, so it can gate a script.

Most first-run problems are a missing credential or a binding that names
an organization the token cannot see. Neither produces an error a person
can act on, since the server logs the rejection and exits (D-58).
`doctor` names which of these is wrong. Without credentials it skips the
network, and its live half runs under one timeout (D-43). A removed
setting still turned on is a failed check (D-32). Its output is redacted
for a public issue (D-48). `TestDoctorFailsOnWrongOrganizationBinding`
and `TestDoctorReportsUnreachableAPI` hold two of its verdicts.

Rejected: the startup log as the only diagnosis, which says the
credentials were rejected but not which value is wrong.
Cost: `doctor` reads the environment of the shell it runs in. A host
that starts the server with other settings can fail where `doctor`
passes.

### D-60 Every tool registers through addTool, the one place for the destructive gate, confirm, the mark, both result halves and the error class

Every tool registers through `addTool` in `internal/tools/registry.go`,
and `internal/tools/register.go` is the one list of what registers.
`addTool` is the one place for the destructive gate, `confirm`, the
mark, the dry-run context, both result halves and the error class (D-29,
D-30, D-34, D-28, D-61, D-40).

A rule applied in each handler is a rule each handler can forget, and a
fix to one lands in one function rather than in every tool. `addTool`
also corrects the schema of a raw JSON value, which schema inference
describes as an array of bytes. A card with a Members or Tags field then
passes the SDK's output validation
(`TestCardCustomFieldValuesPassOutputValidation`). At registration it
panics on a tool that breaks the pairing of `confirm`, the hint and the
mark.

Rejected: the fix in each of the handlers.
Cost: a mistake in `addTool` reaches every tool at once.
`TestMCP_AllTools_SmokeCallable`, which calls every tool through it, is
what catches one (D-62).

### D-61 content and structuredContent are never the same bytes: addTool fills the readable half from internal/render

`content` and `structuredContent` are never the same bytes. `addTool`
fills the readable half from `internal/render`, and the structured half
follows the output schema.

When a handler returns a typed output and a nil result, the SDK marshals
the output into `structuredContent` and, seeing `content` unset, puts
the same JSON in a text block
([server.go](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/mcp/server.go)).
A client that shows one half then shows JSON either way, and a model
reads the same data twice. `render` writes a summary instead. A list
page's summary says when more pages follow, because this server never
aggregates pages and a caller that misses `next_page` would take a
prefix for the whole (D-16). `TestContentAndStructuredContentDiffer`
holds every tool, and `TestListToolContentSummarizesThePage` the list
tools.

Rejected: a nil `content`, which the SDK fills with the same bytes.
Cost: a shape that needs a better summary than the default implements
`render.Summarizer`.

### D-62 Every registered tool has a smokeToolInputs row

Every registered tool has a row in `smokeToolInputs`, in
`internal/tools/smoke_test.go`, and `TestMCP_AllTools_SmokeCallable`
fails on a registered tool without one.

The test calls every tool through the in-memory MCP session with its
row's arguments. A tool that panics, refuses its own minimal input or
fails output validation fails the build. The list comes from the
registry rather than from a list typed beside it, so a tool added
without a row is caught by having been registered (D-68). The harness's
client also fails any call that puts a question to the person (D-31).

Rejected: a hand-typed list of tools to smoke-test, which misses the
tool added after it was written.
Cost: every tool needs a row of minimal arguments, and a row exercises
only the tool's simplest path. The live driver covers every option
(D-74).

### D-63 Where two tools overlap, each description names the other and says when to choose it

Where two tools overlap, each description names the other and says when
to choose it.

`favro_update_card` can move and archive a card, and so can
`favro_move_card` and `favro_archive_card`. `favro_add_tag_to_card`
overlaps `favro_update_card` with `add_tag_ids`. A model picks a tool
from its description alone. Two descriptions that each claim the same
job leave the choice to chance, and a description that names its twin
turns the choice into a rule the model can follow. The overlap stays
because each tool is the natural one for some calls: a single update
that also moves, and a move with nothing else.

Rejected: descriptions that describe each tool alone, which leave a
model to guess between two that both fit.
Cost: nothing but review holds the cross-references, so a renamed or
narrowed tool can leave its twin's description pointing at the wrong
thing.

### D-64 The released tool surface is a contract: a removed tool, a removed input or a newly required one is a break that schema-diff names

The released tool surface is a contract. A removed tool, a removed input
or an input newly required is a break, and `schema-diff` names each one.

`make schemas` writes `schemas.json`, and the gate only reads. It builds
the binary at the last tag, dumps that build's schemas, and compares
them with the working tree's. The comparison is by tool surface, not by
bytes, because the dump carries the version stamp, which differs between
builds. A break needs a `BREAKING CHANGE:` footer, and
`TestABreakIsABreakOfARelease` holds that a break is measured against
the release rather than against an unreleased change. Adding a tool or
an optional input breaks nothing.

Rejected: a gate that rewrites `schemas.json`, green for everyone who
did not run it; a byte comparison, which the build stamp defeats.
Cost: a break shipped in a minor release needs a CHANGELOG line saying
why, and a caller pinned to the old surface still breaks.

### D-65 A pull request names each changed item of the tool surface in a commit footer

A pull request that changes the tool surface names each changed item in
a commit footer. `SCHEMA-CHANGE:` names a change and `BREAKING CHANGE:`
a break of the released surface. The `schema-ack` gate runs on every
pull request and fails when an item is missing.

The schema diff says what changed, and the footer says the author meant
it. Annotations count, since a flipped `destructiveHint` changes whether
a client asks the person (D-33). Both sides are built from commits, the
merge-base and the head, never from the checkout, which in CI is the
merge commit. Footers are read as Conventional Commits defines them
([specification](https://www.conventionalcommits.org/en/v1.0.0/#specification)).
A token is matched in any case, except `BREAKING CHANGE`, which is upper
case, and a value may wrap onto further lines. A gate that could not run
says so, apart from a refusal.
`TestParityHoldsThePullRequestGates` holds CI to running it.

Rejected: a case-sensitive footer token; a footer value read as one
line, which drops a wrapped value.
Cost: a refusal is fixed by adding the footer in an empty commit, never
by an amend, so a branch carries a commit that changes nothing.

### D-66 --dump-schemas lists the whole surface, whatever the flags, as an in-memory client session sees it

`--dump-schemas` prints the whole tool surface as an in-memory client
session lists it, whatever the switches, and exits. It reads no
credentials.

`internal/server` starts a server with every tool registered, the
destructive and upload tools included, connects an in-memory client to
it, and prints what `tools/list` returns. What a client is told, after
the SDK has applied its defaults and schema inference, is the surface. A
dump built from the server's own structs would miss what the SDK adds.
It runs before credentials are read because the surface does not depend
on who calls. The `schema-diff` gate builds the binary at an old tag and
asks it for its schemas, and that build has no keyring or environment to
read (D-64). `TestDumpSchemasListsEverythingAClientIsTold` and
`TestDumpSchemasNeedsNoCredentials` hold both.

Rejected: a dump of the registered structs, which misses what the SDK
adds before a client sees it.
Cost: the dump shows tools a deployment without the switches never
registers, so it describes the full surface, not any one install.

### D-67 Webhooks are listed and deleted, never registered

Webhooks are listed and deleted, never registered. `favro_list_webhooks`
and `favro_delete_webhook` exist, and `POST /webhooks` has an `out`
verdict in `testdata/api-coverage.tsv`.

Favro calls them outgoing webhooks: registering one points Favro at a
URL that receives card events
([create a webhook](https://favro.com/developer/#create-a-webhook)).
This server is a stdio process a host starts and stops, so it can never
be that URL. A tool that points Favro at somebody else's endpoint
configures a system this server has no part in. Listing and removing
what somebody else registered needs no receiver. `GET /webhooks` answers
with a bare array rather than the paginated envelope, as its reference
says
([get all webhooks](https://favro.com/developer/#get-all-webhooks)), and
`TestListWebhooksDecodesABareArray` holds it. Checked by the live steps
"list a widget's webhooks" and "delete a webhook, dry run".

Rejected: `POST /webhooks`, which points Favro at a URL that must
outlive a stdio process.
Cost: there is no disposable webhook, so `favro_delete_webhook` has run
only as a dry run. Issue #120 asks to check a real webhook: its signing
secret, the spelling of its column scope, and a delete.

### D-68 Every rule adopted is a test that derives its list from the code and asserts a floor on how much it read

Every rule the repository adopts is a test or a gate. It derives its
list from the code rather than from a list typed beside it, and asserts
a floor on how much it read.

A rule stated in a document is true on paper and false in the code at
the same time, and nothing says so. A completeness claim decays the
moment the surface grows, so a check that covers "every tool" reads the
tools from the registry. A checker that read nothing and a checker that
found nothing print the same sentence, so each asserts a floor and fails
below it. An exemption list is asserted to occur, since an exemption
that exempts nothing reads as considered and is not (D-49, D-71). A gate
is trusted only after it has been watched fail on a planted break.
`TestEveryCommandHasATest` reads the gate registry and requires each
command to be exercised in code.

Rejected: a rule stated only in a document; a gate nobody has watched
fail.
Cost: each rule costs a test and a floor, and a rule nobody can derive
from the code, such as one about prose, rests on review (D-63).

### D-69 make check and CI run the same set, matched by what each recipe runs

`make check` and CI run the same set of checks. The `parity` gate holds
every command `make check` runs to a `ci.yml` command in the same shell
words, or to the action that stands in for it, and every go command CI
runs to one `make check` runs. CI's side is the jobs a push to main
runs: the `pr` job runs on a pull request alone, so a check only it runs
is one main never runs. Each gate runs in the pipeline its dispatch
entry records, with the same arguments on both sides. The release is
`.goreleaser.yaml`, `release.yml` and the local workflows it calls.

They are two lists in two files, and an edit reaches one of them.
Deleting a scan from CI would leave `make check` calling itself
everything CI runs while CI ran less. Matching by what each recipe runs,
rather than by target names, means no third list maps names to jobs.
Every file a `//go:build` tag keeps out of a plain build is vetted on
both sides, and a recipe line make runs past a failure, behind `-` or
`.IGNORE`, is refused. `TestParityRefusesToCompareNothing` holds the
floor, and `TestParityAgainstThisRepository` runs the gate on the tree.

Rejected: a map from target names to jobs, the third hand-maintained
list.
Cost: adding a gate means adding it in both places, the `check` target
and the workflow, in the same change.

### D-70 Unit tests use only Go's standard library, against httptest fakes, with the race detector

Unit tests use Go's own `testing` package,
`if got != want { t.Errorf(…) }`, against `httptest` fakes, and run with
the race detector.

A fake that answers as Favro is believed to answer keeps the unit suite
offline, fast and repeatable on every platform CI runs. Go's `testing`
package is enough for the assertions, and an assertion library is a
dependency the tree does not need. `depguard`'s `no-assertion-library`
rule refuses testify and go-difflib in any file. The race detector runs
because the full-card fan-out and the caches are concurrent (D-11,
D-08). The MCP layer is tested in memory over the SDK's transport, and
the built binary over stdio by the `smoke` gate (D-62).

Rejected: testify and go-difflib, two dependencies for what Go's
`testing` package does.
Cost: every fake is written from what Favro is believed to do, so a
green suite proves only that belief. The live run is what tests it
(D-73).

### D-71 Every package holds an 80% statement floor of its own

Every package under `internal/` holds an 80% statement floor of its own.
The `coverage` gate reads the package list from
`go list ./internal/...`, so a package is under the floor from its first
commit.

A floor on the average hides a package at 20% behind four at 95%, so the
floor is per package. An exemption has to be written in
`exemptFromFloor` in `scripts/gates/coverage.go` with its reason, where
a reviewer sees it. `TestEveryExemptionNamesARealPackage` refuses an
entry that names no package, since it would exempt nothing while reading
as a decision. `TestReadProfileRefusesAnEmptyRun` refuses a profile that
measured nothing. `make cover` runs it after the tests.

Rejected: a floor on the average, which hides a package at 20% behind
four at 95%.
Cost: `cmd/favro-mcp` is outside the floor, since the list is read from
`internal/`. Coverage measures what ran, not what was asserted.

### D-72 Every endpoint Favro documents and every field of a modeled type has a verdict, held offline to a snapshot refetched by hand

Every endpoint Favro documents, and every field of a type this server
models, has a written verdict, held offline to a snapshot refetched by
hand.

`make api-diff` fetches Favro's reference
([developer](https://favro.com/developer/)) and writes
`testdata/api-surface.json`, which nothing else writes.
`testdata/api-coverage.tsv` holds one verdict per endpoint, and
`testdata/api-fields-waived.tsv` one per unmodeled field, both by hand.
The `api-coverage` and `api-fields` gates hold both to the snapshot in
every `make check`, from both directions. An endpoint record alone
cannot see a type that drops half of what Favro sends, because
`encoding/json` ignores what it does not recognize. A count of endpoints
in prose is a claim nothing checks. `TestCheckFieldsReportsAStaleWaiver`
refuses a waiver for a field the reference does not have.

Rejected: an endpoint record alone, which cannot see a dropped field; a
prose count of endpoints.
Cost: Favro publishes no machine-readable surface, so the snapshot is
scraped from its reference page. The fetch is by hand, a release step in
[release.md](release.md).

### D-73 A wire contract is verified live, by reading the resource back; the reference page is not proof

A wire contract is verified live, by reading the resource back. Favro's
reference page is a source, not proof.

Favro answers 200 to a request body it ignores, so a wrong field name
looks exactly like success
([update a card](https://favro.com/developer/#update-a-card)). Its
reference has been wrong against the live API in places, and Favro
publishes no discovery document or specification to check a type
against. A fixture written to an assumption agrees with the assumption.
`GET /webhooks` answers with a bare array, as the reference says, and a
fixture in the paginated shape passed the unit tests anyway. So every
belief about Favro that the code depends on is stated at that code, with
the reference linked and the live step that checks it named. A change
that touches the wire runs `make live` before it merges (D-74).

Rejected: the reference page as proof, which disagrees with the live
API; a fixture written to an assumption.
Cost: live verification needs a real organization and a person to run
it, and a belief no live step reaches rests on a probe made by hand.

### D-74 The live driver drives every tool and option through the built binary with dry_run, from the steps internal/livecover holds

The live driver, `scripts/livefavro`, drives every tool and option
through the built binary over stdio against a real organization, with
`dry_run` on every write. Its steps live in `internal/livecover`, each
with a unique `Name`, and `make live` runs it.

A driver covers the surface on the day it is written and stops the
moment the surface grows. So the `live-cover` gate measures the steps
against the binary's own published schema, per option, and fails on any
tool or option no step exercises. A waiver in
`testdata/live-cover-waived.tsv` needs its reason. Holding the steps in
a package the gate imports means the driver and the gate read one list.
The seed steps run first in a written order, because a write needs the
ids the reads supply. A comment that depends on Favro's behavior cites a
step by its `Name` (D-73). How to run it is in
[CONTRIBUTING.md](../CONTRIBUTING.md#the-live-run).

Rejected: steps in alphabetical order, which ran writes before the reads
that supply their ids; a gate that parses the driver's source.
Cost: a dry-run step never observes a result. No live step reads a
second page; issue #127 asks for one.

### D-75 The live driver's write mode writes only to probes it created, and deletes them however the run ends

`go run ./scripts/livefavro -writes` writes only to probes it created: a
probe tag and a probe collection, named so the sweep can find them. It
deletes them however the run ends.

The real writes the dry-run steps cannot observe are checked here:
`confirm` and the widening guards on real objects, a real delete, a real
widening read back (D-30, D-35, D-36). Writing to anything else would
change someone's organization to test a server. Each probe's cleanup is
registered when it is created, and an interrupt runs every cleanup. The
sweep then fails on any probe still there. A deleted collection reads
back as 403, as one the token cannot see, so the check counts
`[not_found]` or `[forbidden]` as gone, and only after the delete
succeeded. Checked by the live steps "the tag is gone" and "the
collection is gone". `TestScratchWritesOnlyToObjectsTheRunCreated` holds
the first rule.

Rejected: probing a public collection, which is the act it would test.
Cost: the probe collection is opened to the organization for a moment,
so everyone in it can see an empty probe until the sweep deletes it.

### D-76 Agent evals score each task by the end state read back and by the trace, in a sandbox the run makes and removes

Agent evals score whether a model can do a task through the tools, by
the end state read back and by the trace. Each run makes a collection
and a board in the live organization, works there, and deletes them.

A unit test cannot see a tool the model never finds, or a description
that sends it the wrong way. A model's account of what it did is the
least reliable thing in the run, so the end state is read back through
this server. A model can also pass by guessing, so the trace is scored
too. The evals live in `scripts/evals` behind the `evals` build tag and
run by `make evals`, through the `claude` CLI. The task table sits
outside the tag, so `go test` walks every prompt without credentials, a
network or a model. `TestEveryTaskIsScorableAndExplained` holds each
task to a scorer and a reason.

Rejected: a model API key of the evals' own, where the `claude` CLI is
already installed and signed in.
Cost: the evals run by hand, against a real organization, and each run
spends money on the model, capped per run.

### D-77 The eval session reaches this server's tools and nothing else, and none that deletes

The eval session reaches this server's tools and nothing else, and none
that deletes. The model's server starts without
`FAVRO_ENABLE_DESTRUCTIVE`, and cleanup runs on the harness's own
session.

The run reads a real organization, so card text written by anyone with
access reaches the model. The process tree holds a live Favro
credential. Any built-in that runs a command would be a path for
injected text to send it somewhere. So the session runs with
`--strict-mcp-config`, `--allowed-tools mcp__favro__*`, `--tools ""`,
the CLI's own switch for no built-ins, and `--setting-sources ""`, so
the maintainer's own permission rules, hooks and skills play no part.
The flags are in `scripts/evals/run.go`.

Rejected: `--allowed-tools` alone, which leaves the host's built-ins; a
denylist of built-in names, which fails open on the next built-in the
CLI adds.
Cost: no test asserts the flags, so review holds them, and a CLI release
that changes a switch's meaning goes unseen.

### D-78 The documentation is held to the code: tool names, counts, settings, gates, the package map and paths

The documentation is held to the code. The `staleness` gate holds the
tool rows in [TOOLS.md](TOOLS.md) and the README to the registry both
ways, and any prose count of the destructive tools to the binary. It
holds every `FAVRO_*` variable the source reads to
[configuration.md](configuration.md) and the README, and the CHANGELOG
to its rules. A commit since the newest release tag HEAD reaches that
changes what users get needs an entry. That is a file other than a
test in a package the binary imports, a file one embeds, a bundle's
manifest, or `go.mod` outside its require and tool lines. The README,
LICENSE and NOTICE the bundles carry are docs, and the release writes
the registry entry, so none of them counts. With no such tag the rule
says it could not run, and the other checks still run.

A document a person copies into a client's configuration is an input,
and a number in prose is a claim about a list beside it. Correcting a
stale count by hand does not stop it being typed by hand, so each rule
derives its expected set from the code. The docs gate holds the rest of
the references: every D-ID citation, every Held by cell, the codemap
against the tree, every path a doc names and every live step a comment
names. `TestToolsDocumentedReadsBothDirections` and
`TestEveryEnvNameReachesBothDocuments` hold two of the rules.

Rejected: correcting a hand-typed number by hand.
Cost: the gate's scope is itself a list of documents in
`scripts/gates/staleness.go`, so a document left off it is held by
nothing.

### D-79 The toolchain is the one go.mod names, everywhere; there is no N-1 matrix entry

The toolchain is the one `go.mod` names, everywhere. Every CI job reads
it with `go-version-file: go.mod`, and there is no N-1 matrix entry.

One line is the single source of truth, so a toolchain change is one
edit that CI, the release and a contributor's `go` command all follow. A
binary is built and tested on one Go, and the release ships what CI
tested. An N-1 entry would test a build nobody ships, for users who
install a binary rather than a library. The README's install command
builds with the same toolchain, which `go` downloads when the local one
is older.

Rejected: an N-1 matrix entry, which tests a toolchain no release is
built with.
Cost: a contributor's Go must be at least the version `go.mod` names, or
`go` downloads it, and a toolchain bump lands for everyone at once.

### D-80 Every action is pinned to a full SHA, every tool to one exact version, and every workflow pins its shell

Every action in a workflow is pinned to a full commit SHA, with its
version in a trailing comment. Every tool a recipe or a workflow
installs is pinned to one exact version, and every workflow pins the
shell its `run` steps use.

A tag or a branch can be moved under a release, and a full-length commit
SHA is the only immutable reference to an action
([security hardening](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions)).
A floating version or `@latest` runs whatever was published last, in the
job that signs the release. A shell left to the runner differs between
Windows and the others. The `pins` gate reads the workflows, the
`Makefile`, the commands `.goreleaser.yaml` runs, the git hook and the
string literals of the Go under `scripts/`, and refuses an action it
cannot classify and a container image with no digest. A known tool's
name as a word of a recipe or hook line, other than the module a
`go run` names, runs whatever version the PATH holds wherever it sits on
the line, so the gate refuses it too.

A known tool's module@version on any line of a workflow or the
`Makefile` is a pin, held to the tool's one version. Every version input
of a step or a reusable call is held, its name read in upper case as the
runner reads it. An `env:` version key is read in any case. An
expression is no exact version, except a context passed to this
repository's own reusable workflow, which holds the input where it uses
it.

setup-go reads `go-version` before `go-version-file`, so one beside
`go.mod` is refused. The `.goreleaser.yaml` reader's key paths are
checked against one goreleaser, and a release that runs another fails
the gate. The readers refuse a line break they read apart from a YAML
parser or make, such as a lone CR, so a step cannot hide after one.
goreleaser runs one branch of a template, so the `.goreleaser.yaml`
reader refuses a command holding a control action other than one
`{{ if }}` or `{{ with }}` wrapping it whole.
`TestExactSemverRejectsWhatCanMoveUnderYou` holds what counts as exact.

Rejected: floating major tags and `@latest`.
Cost: Dependabot or a person bumps each pin by hand, and a security fix
in an action arrives only with the bump.

### D-81 make check scans for vulnerabilities in source mode, licenses against an allowlist and secrets with gitleaks, as CI does

`make check` scans for known vulnerabilities in source mode, for
licenses against an allowlist, and for secrets with gitleaks, as CI
does.

Source mode follows call paths rather than a symbol table, so it reports
a vulnerability the code can reach
([govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck)).
It checks the packages of the Go release on the path, and skips them
under a version string it cannot read, so it runs under an official Go
release. The license check accepts Apache-2.0, BSD, MIT and ISC, and
fails on anything else a dependency carries.

gitleaks scans every commit HEAD reaches for credentials, which the
`leaks` gate does not look for (D-49). CI runs `make secrets`, so the
Makefile holds the scan's version and flags, and the pre-commit hook
scans the staged changes with both, since a credential is in the history
once it is committed. The `parity` gate holds CI to the same
vulnerability, license and secret scans (D-69).

Rejected: govulncheck in binary mode, which reads a symbol table and
reports what the code cannot reach; gitleaks over every branch the clone
holds, its default, which lets a stale branch's finding fail every pull
request.
Cost: `github.com/segmentio/asm` is ignored by path, because go-licenses
cannot name its MIT-0 license, so the check does not see a later change
to that module's license.

### D-82 CI runs on Linux, macOS and Windows

CI runs the unit tests on Linux, macOS and Windows.

A person installs this server on all three, and the release builds for
all three (D-91). Each platform breaks things the others pass. POSIX
regular expressions have no `\s` or `\b`, so a pattern git accepts on
Linux matches nothing on macOS, and a fixture binary on Windows has to
be a real build. Every gate that greps through git therefore greps a
fixed string and applies the pattern in Go, where the engine is the same
everywhere. The workflow's `test` job runs on `ubuntu-latest`,
`macos-latest` and `windows-latest`.

Rejected: Linux alone, which passes what macOS and Windows fail.
Cost: git's GNU regex extensions are off limits, so the gates grep a
fixed string and match in Go, and every pull request waits on three
runners.

### D-83 A tag is the whole release, and its notes are that version's CHANGELOG section

A tag is the whole release. Pushing a version tag runs the release
workflow, which builds, signs and publishes, and the release notes are
that version's CHANGELOG section, taken verbatim.

The CHANGELOG is the one place a release is described, so the notes
cannot disagree with it. The `release-notes` gate takes exactly one
version's section, and refuses a tag that is not a version, which is
what the workflow passes it. A section ends at the next heading of its
level or above, or at the compare-link footer, and a heading in a fence
is an example. `TestChangelogSectionTakesOneVersion` holds the cut. The maintainer cuts every tag. The steps are in
[release.md](release.md).

Rejected: release notes written in the release form, a second
description of the release that can disagree with the CHANGELOG.
Cost: a version with no CHANGELOG section fails at tag time, after the
tag is pushed, and a pushed tag is not moved, so the fix is a patch
version.

### D-84 A release is signed and attested: the checksums cover the archives and both bundles, under keyless cosign and build provenance, with an SBOM per archive and byte-identical rebuilds

A release is signed and attested. The checksums cover every archive, the
`.mcpb` and the `.plugin`, and a keyless cosign signature covers the
checksums. The archives, both bundles and `checksums.txt` carry build
provenance, each archive has an SBOM, and a rebuild is byte-identical.

Both bundles are packed in the universal binary's post hook and named in
`checksum.extra_files` and `release.extra_files` in `.goreleaser.yaml`.
GoReleaser hashes the artifacts it built, and a file a hook dropped into
`dist/` is not one of them. So without those entries a bundle ships
outside the signature while the README tells people to verify it.
Keyless signing ties the signature to the workflow's identity, with no
private key to hold or leak. `mod_timestamp` set to the commit's time
makes a rebuild byte-identical. The `mcpb` gate holds each of these
lines, and `TestReleaseWiringIsHeld` fails when one is deleted. It holds
the attest step in `release.yml`, and in the local workflows it calls,
to name the archives, both bundles and the checksum file too, each in
an entry of its own, which
`TestTheBundlesCarryProvenance` drives. The step sits in each job that
runs goreleaser, through goreleaser-action or a run line, whatever the
subcommand. That job's token writes `id-token` and `attestations`:
another job, or a workflow it calls, can be skipped while the release
ships.

Rejected: a bundle dropped into `dist/` by a hook alone, which ships
outside the signature.
Cost: the signing step runs only in the release workflow, so the local
rehearsal skips signing, the SBOMs and publishing.

### D-85 A tag name reaches no shell: it travels through the environment, and the trigger admits only alphanumeric suffixes

A tag name reaches no shell. The release workflow passes it through the
environment, never into a `run:` line, and the trigger admits only a
version, with an alphanumeric suffix at most.

An Actions expression is substituted as text before bash parses the
line, and double quotes stop word splitting but not command
substitution. Git accepts `$`, a backquote, `;` and braces in a ref
name. A tag substituted into a `run:` line could run a command in the
job that holds the identity that signs the release (D-84). An
environment variable is data to bash, not code. The trigger's patterns
admit `vX.Y.Z` and a suffix of letters and digits, so a tag with shell
syntax starts no release. The `release-notes` gate accepts `vX.Y.Z` and
a prerelease of dot-separated identifiers of letters, digits and
hyphens, each holding a letter or a digit
([semver](https://semver.org/#spec-item-9)), and refuses any other tag.
`TestReleaseNotesRefusesATagThatIsNotAVersion` holds the gate's side of
it.

Rejected: the tag substituted into a `run:` line.
Cost: every step that needs the tag reads it from the environment, and a
tag with an unusual suffix starts no release at all.

### D-86 A go install build reports the module version Go embeds

A `go install` build reports the version Go embeds. `internal/version`
takes the tag and commit from `-ldflags`, and falls back to the module
version and the VCS revision from `debug.ReadBuildInfo`
([ReadBuildInfo](https://pkg.go.dev/runtime/debug#ReadBuildInfo)).

`go install` applies no `-ldflags`, so without the fallback every build
installed that way would report `dev`, and no bug report from one could
be traced to a build. `doctor` prints which source answered, the stamp,
the module or neither, because "dev" and a real version are the same
word to a person saying what they run (D-59).
`TestSourceAndModuleAlwaysAnswer` and `TestStampFrom` hold the fallback.

Rejected: the `-ldflags` stamp alone, which leaves every `go install`
build reporting `dev`.
Cost: a binary built by hand from a checkout carries whatever version Go
derives for that tree. Only a release carries the stamp the release
workflow sets.

### D-87 Changes reach main through a pull request with eight required checks and no bypass actors

Changes reach `main` through a pull request, with the required checks
green and no bypass actors. The required checks are the test job on
Linux, macOS and Windows, and `gates`, `cover`, `static`, `secrets` and
`coverage`.

A rule that only a careful person keeps is broken on the first hurried
day. Branch protection makes the checks the merge condition, and no
bypass actors means the maintainer is held too. The checks are required
by name, so the names in the ruleset and the job names in the workflow
have to agree. The maintainer pushes and merges. The settings live in
the repository's ruleset, which [CONTRIBUTING.md](../CONTRIBUTING.md)
describes.

Rejected: a direct push to `main`, which skips every check CI runs.
Cost: renaming a CI job silently drops its requirement, because the
ruleset waits for a name the workflow does not produce, and only review
notices.

### D-88 Both bundles ship: the .plugin, which is how people install this server, and the .mcpb beside it

Both bundles ship: the `.plugin`, which is how people install this
server, and a Claude Desktop `.mcpb` beside it.

The `.plugin` carries binaries for each platform and a launcher, with an
`.mcp.json` that points at the plugin's root. It is the install path
people use, and dropping it would break them. The `.mcpb` is what Claude
Desktop installs, and the Google servers ship one too. The `plugin` and
`mcpb` gates each validate their manifest against the files the packer
stages, because the failures that matter are referential: an entry point
nothing stages, an override no declared platform can reach, a launcher
choosing between binary names no manifest mentions.
`TestPluginGateAgainstThisRepository` and `TestCommittedManifestPasses`
run them on the tree.

Rejected: dropping the `.plugin` to ship the `.mcpb` alone.
Cost: two manifests and two packers to keep honest, each with its own
gate.

### D-89 The .mcpb asks for every setting the server reads, derived from config.Vars, each defaulting to the server's own default, and is the whole setup

The `.mcpb` asks for every setting the server reads, derived from
`config.Vars`, each defaulting to the server's own default, and it is
the whole setup. It asks for the address, the API token, the
organization id, `enable_destructive` and `upload_dir`.

A Favro credential is a token rather than a sign-in, so the bundle can
collect everything the server needs and work on the first call. The
`mcpb` gate derives what the bundle asks for from `config.Vars`: each
variable is asked for or left out with a reason. The switch defaults to
off and the folder to an empty string, which `internal/config` reads as
unset. The reference host leaves a key with no default as its literal
placeholder
([config.ts](https://github.com/anthropics/mcpb/blob/v2.1.2/src/shared/config.ts)).
Claude Desktop approves per tool and a person can always allow one, so a
switch left off is the guard a bundle install keeps. The token is asked
for as sensitive.

Rejected: an optional key without a default; a `tools` list in the
manifest; a required switch; a manifest saying the bundle does not sign
you in.
Cost: a person turns each switch on by hand. Issue #124 asks to install
the bundle in Claude Desktop itself, since its handling of hints, blank
fields and overrides comes from documentation.

### D-90 A bundle's commands start with ${__dirname}/ and name a staged binary or the launcher, and no platform override sets env or args

A bundle's commands start with `${__dirname}/` and name a staged binary
or the launcher. `mcp_config.args` is empty, and no platform override
sets `env` or `args`.

The reference host takes an override's `env` or `args` in place of the
base's
([config.ts](https://github.com/anthropics/mcpb/blob/v2.1.2/src/shared/config.ts)).
An override's `env`, even an empty one, drops every base variable on
that platform. An argument such as `--dry-run` would configure the
server past every check on `env`. A relative command has no documented
base. The `mcpb` gate refuses each of these, and a command that names
`LICENSE`, `NOTICE` or `README.md`. A claimed platform must start the
file staged for it, and the launcher is generated from the same table
the packer stages from. `TestManifestProblemsRunsEveryCheck` and
`TestLauncherNamesWhatThePackerStages` hold both. The manifest is held
to a vendored copy of its published schema.

Rejected: an override's `env` or `args`, which replace the base's; a
`$schema` that names a branch rather than a tag; manifest version 0.4,
which adds nothing this bundle uses.
Cost: a platform that needs a different setting has no way to say so in
the manifest, and the launcher stands in for it.

### D-91 One release matrix names the six archives, and both bundles run the Windows amd64 binary on arm64

One release matrix, `releaseMatrix` in `scripts/gates/plugin.go`, names
the archives the release builds: darwin, linux and windows, each on
amd64 and arm64. Both bundles run the Windows amd64 binary on arm64.

A manifest names one command per platform and has no key for the
architecture. Windows on arm64 runs an amd64 program under emulation, so
the bundles stage the amd64 binary for `win32`. The windows arm64
archive is for an install from the release page. The `mcpb` and `plugin`
gates both read the one table, so the archives, the staged files and the
launcher cannot disagree. `TestGoreleaserMatrixIsTheOneTheBundleClaims`
holds `.goreleaser.yaml` to it.

Rejected: a native arm64 Windows binary in the bundles, which the
manifest has no key to choose.
Cost: Windows 10 on Arm cannot run x64 code, so a bundle install there
fails. Issue #125 asks whether to ship a native windows arm64 binary in
the bundles.

### D-92 The MCP registry entry obeys the rules the registry enforces in code

The MCP registry entry, `packaging/registry/server.json`, obeys the
rules the registry enforces in its code, and the `registry` gate holds
them in every `make check`.

The registry's schema does not carry those rules: its validator does.
The identifier must be an HTTPS GitHub release asset URL containing
"mcp", it must not sit beside a `registryBaseUrl`, and the bundle's hash
must be 64 hex characters. Each is refused at publish time, after the
sign-in has succeeded, which is the worst moment to find out. The
committed entry carries a placeholder version and hash, since a real
version in the tree is stale at the next release. `registry-publish`
fills both in from the signed checksums at tag time.
`TestMCPRegistryCatchesWhatTheRegistryRefuses` plants each break.

Rejected: holding the entry to the registry's published schema alone,
which passes what its validator refuses.
Cost: the rules are read from the registry's source, so a rule it adds
is unknown here until the entry is refused.

### D-93 A write whose 200 is not trusted carries an `unverified` error class

A write whose 200 the server does not trust would carry an error class
of its own, `unverified`. It is declined.

The message would read
`[unverified] Favro returned 200 and the write was not read back`. It
renders on an error result, which sets `IsError` and tells the caller
the call failed, when the write may well have landed. A caller that
retries on `IsError` posts the comment twice or creates the card twice,
so the class would cause the damage it was meant to warn about. It is
also constant per tool rather than per call, and a constant per tool is
a tool description, which is where the warning lives. The honest signal
for a write that was read back is a field on a successful result (D-24).

Rejected: an `unverified` error class, which marks a call that may have
landed as failed.
Cost: a write Favro may have ignored is reported as a note on a
successful result, which a caller has to read to notice (D-24).

### D-94 The server confirms its riskiest writes with the person, through MCP form elicitation

The server would confirm its riskiest writes with the person, through
MCP form elicitation: a form with no fields, put before the write, with
the write sent only on an accept. It is superseded by D-31, under which
the server asks the person nothing.

The MCP specification gives consent to each tool call to the host, and
never makes an elicitation's answer consent: a client may answer "from
the user or other sources"
([elicitation](https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation)).
Clients answer such a question with nobody there, as a Claude Code
`Elicitation` hook or Codex under full access does, or never draw it. No
client's allow and ask rules match it. Most widely used servers with
risky writes never ask. A question on every call is answered without
reading.

Rejected: confirming writes through form elicitation; Claude Code's mark
sent only to a client that cannot ask.
Cost: while it held, `FAVRO_REQUIRE_PROMPT` refused the asking writes on
a client that could not ask. That setting is refused at start (D-32).

Next ID: D-95
