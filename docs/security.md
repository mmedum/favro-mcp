# Security model

The threat model and the invariants the code holds.
[`SECURITY.md`](../SECURITY.md) says how to report a vulnerability.

It applies to one binary, a subprocess of your MCP client on your
machine, with your own Favro address, API token and organization, against
Favro only (D-01, D-53). It opens no listener, and has no telemetry, no
crash reporting and no update check. `make api-diff`, a maintainer
command that `make check` never runs, is the one other fetch: it reads
Favro's published reference.

## Trust boundaries

| Boundary | Crosses | Assumption |
|---|---|---|
| MCP client ↔ this server | An OS pipe | Both run as the same OS user, unauthenticated. Stdout carries only protocol frames (D-45). |
| This server ↔ Favro | HTTPS to `favro.com` | Every call carries HTTP Basic with the address and the token, and the organization header (D-54). Every call has a timeout (D-43). |
| This server ↔ disk | The file system | The upload tools read only inside `FAVRO_UPLOAD_DIR` (D-38). No tool writes the environment, a file or the keyring; only `favro-mcp auth` writes the keyring (D-39). |
| Card authors ↔ the model | Every card, comment and description a tool returns | Written by anyone in the organization, sometimes to steer an assistant. |

## What is worth stealing

- **The API token.** It carries the permissions of the user who issued
  it, in every organization that user can reach, and Favro cannot scope
  it down. It lives in the OS keyring or the environment.
- **Organization content.** It passes through tool results to your
  client, and through nothing else.
- **The organization itself.** A delete, a collection opened wider, or a
  description overwritten. Favro has no undo.

## Adversaries

**A card author writing to steer your assistant.** The server returns
card text faithfully and does not interpret it, so it cannot stop a
model being persuaded. It limits what persuasion reaches. The destructive
tools are absent unless the deployer turns them on (D-29). The ordinary
writes share only inside the organization (D-35), and opening a
collection is a destructive tool of its own (D-36). The upload tools
read one directory (D-38). A tag name with a typo never creates a tag
(D-21), and no tool changes the server's settings (D-39).

**A malicious MCP client, or a persuaded model driving an honest one.**
It can call any registered tool as you, so registration is the control.
`confirm: true` and a collection's name passed as a witness are written
by the model; they are never your approval (D-30, D-36).

**The repository's own live runs.** The evals point a model at a real
organization, so card text reaches a process that holds a live token.
The model gets this server's tools and nothing else, from a server with
no destructive tools (D-77).

**A forged release.** The checksums are signed keylessly and every
archive carries build provenance (D-84). A tag name never reaches a
shell (D-85).

## Invariants the code holds

A test or a gate holds each of these. A way to break one is a
vulnerability.

- **Stdout carries only JSON-RPC.** forbidigo, `make smoke` and
  `TestForbidigoGuardsStdout` hold it (D-45).
- **A log line carries the method, the path's shape, the query's
  parameter names, the attempt and the outcome, never a payload, an id
  or the organization.** `TestDebugLogNeverCarriesTheSubject`,
  `TestRedactPathIDs` and `TestStartupLineNamesNoTenant` hold it (D-46).
- **The token is never printed**: not in a result, a log, a dry-run
  record, `auth status` or `doctor`.
  `TestAuthStatus_ReportsSourceAndOrgButNeverTheToken` and
  `TestSecretsScrubsTheTokenAndLeavesIdentifiers` hold it (D-47).
  `doctor` registers every value it hides with the redactor, which
  `TestDoctorRedactsCredentials` holds (D-48).
- **The destructive tools are absent without `FAVRO_ENABLE_DESTRUCTIVE`,
  and each refuses a call without `confirm: true`.**
  `TestDestructiveToolsAreOptIn` and `TestEveryDestructiveCallNeedsConfirm`
  hold it (D-29, D-30).
- **A dry run sends no write, whatever the handler does.**
  `TestEveryWriteTakesDryRunAndSendsNothing` and
  `TestADryRunWritesNothingWhateverTheHandlerDoes` hold it (D-28).
- **The ordinary writes share only inside the organization, and a
  widening names its collection and is read back.**
  `TestOrdinaryWritesShareOnlyInsideTheOrganization`,
  `TestCollectionWritesRefuseWideSharing` and `TestWidenCollectionSharing`
  hold it (D-35, D-36).
- **The server asks nobody, and no tool writes its own settings.**
  `TestNoToolWritesTheServersOwnSettings` holds the second (D-31, D-39),
  and the tool tests' client fails any call that asks.
- **The upload tools read only inside `FAVRO_UPLOAD_DIR`, a link
  included.** `TestMCP_UploadAttachment_ReadsOnlyTheUploadDir` and
  `TestUploadToolsNeedAnUploadDir` hold it (D-38).
- **A write is sent once, and one whose answer was lost is
  `ambiguous_outcome`.** `TestDo_WriteIsNotRepeatedAfter5xx` and
  `TestDo_WriteCutOffAfterSendIsAmbiguous` hold it (D-41).
- **Every HTTP call has a timeout**, the startup check and `doctor`'s
  live half included. `TestDefaultValidator_HasReasonableDefaults` holds
  it, and forbidigo refuses `http.DefaultClient` and the package-level
  helpers that use it (D-43).
- **No result can carry a webhook's signing secret**, since no type has
  a field for it. `TestWebhookSecretIsNeverDecoded` holds it (D-52).
- **Nothing in the repository identifies a real organization.**
  `make leaks` and gitleaks hold it (D-49). The live driver and the evals
  print only through the redactor, which `make transcript` holds (D-51).

Every regular expression runs on Go's RE2 engine, in linear time.

## Not a vulnerability

Do not report these as vulnerabilities. Open an ordinary issue if one
surprises you.

- **The client did not prompt.** Annotations are hints, and a client can
  run a tool with nobody approving. A destructive tool registered while
  its switch is off is a vulnerability.
- **What runs as you can act as you**, the environment and the keyring
  included.
- **The token reaches everything its user can.** Favro offers no scope
  narrower than the user.
- **A tool result is not masked.** It is your data, returned to you;
  `favro_ping` returns the organization id for the same reason.
- **A model following instructions in a card.** That is the client's
  concern. Content that makes the server itself break an invariant is a
  vulnerability.
- **A live run's output names cards and boards.** The redactor removes
  ids and addresses, not names, and the output is for the maintainer's
  own terminal (D-51).
- **Someone else's code.** A flaw in Favro goes to Favro, and one in a
  dependency to that project.

## Dependency vulnerabilities

`govulncheck` runs in source mode in `make check` and in CI, and fails
when this server's code can reach a vulnerable symbol (D-81). A CVE in a
dependency is a vulnerability here only then. It is fixed in a release
whose CHANGELOG entry names the CVE or GHSA; otherwise the dependency is
updated in the normal course.

## What you are responsible for

- **The token's user.** Issue the token from a user with the least
  access you need, not an administrator. For read-only use, leave
  `FAVRO_ENABLE_DESTRUCTIVE` off and start the server with `--dry-run`
  (D-56). [`troubleshooting.md`](troubleshooting.md#tasks) says how to
  rotate the token, revoke it and handle a suspected exposure.
- **The switches you turn on.** `FAVRO_ENABLE_DESTRUCTIVE` registers
  deletes and the widening tool, so leave it off for a client that runs
  with nobody watching. `FAVRO_UPLOAD_DIR` is a directory whose every
  file a model can send to Favro; point it at one that holds only what
  you would attach to a card, never your home directory.
- **Your client's approvals.** Give the sharing, upload and deleting
  tools an `ask` rule, as the README's
  [Approving calls](../README.md#approving-calls) shows.
- **Where the token lives.** Keep a keyring available, and
  `FAVRO_API_TOKEN` out of shared shells and logs.
- **What you paste.** `doctor` and a debug log are built to be safe
  (D-46, D-48). `doctor --show-ids`, a token and a tool result are not.
