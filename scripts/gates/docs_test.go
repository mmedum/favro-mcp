package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/livecover"
)

// The fixture checkout's paths, named once because most rows edit them.
const (
	fxArch       = "docs/architecture.md"
	fxRunbook    = "docs/runbook.md"
	fxReadme     = "README.md"
	fxAgent      = "CLAUDE.md"
	fxChangelog  = "CHANGELOG.md"
	fxClient     = "internal/favroapi/client.go"
	fxClientTest = "internal/favroapi/client_test.go"
	fxServer     = "cmd/server/main.go"
	fxRecord     = "testdata/api-coverage.tsv"
	fxFields     = "testdata/api-fields-waived.tsv"
	fxWaived     = "testdata/live-cover-waived.tsv"
	fxClaims     = "testdata/outcome-claims.tsv"
	fxGolangci   = ".golangci.yml"
	fxMakefile   = "Makefile"
	fxGitignore  = ".gitignore"
	fxGitleaks   = ".gitleaks.toml"
	fxGates      = "scripts/gates/main.go"
	fxDriver     = "scripts/livefavro/main.go"
)

// textLines is a file of these lines, so a fixture's line numbers can be
// read off its source: the nth argument is line n.
func textLines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// The index rows, named because several rows edit them whole.
const (
	fxRow01 = "| D-01 | Retries back off | accepted | TestRetry, smoke |"
	fxRow02 = "| D-02 | Retry forever | superseded by D-03 | — |"
	fxRow03 = "| D-03 | Retry three times | accepted | forbidigo, lint, review |"
)

// The ends of the problems several tests expect, written once.
const (
	notAHolder = "which is not review, a make target, a gates command, an enabled linter or a test or fuzz function"
	bySlug     = "cite a D-ID, docs/<file>.md#<slug> or a source link"
	undefined  = ", which no step or -writes check in internal/livecover is named"
)

// docsFixture is a checkout the docs gate accepts as it stands. Between
// them its files hold one of everything the gate must stay quiet about:
// each kind of Held by item, a superseded decision cited only where that
// is allowed, a repeated heading, links to a file and a directory, a
// codemap naming each package, live steps and -writes checks from
// livecover.Names cited across comment lines, config comments, the
// records' reasons, and citations in a fence, a string literal, a
// testdata page and a released changelog section.
func docsFixture() map[string]string {
	return map[string]string{
		fxMakefile: textLines(
			"# The checks CI runs (D-03).",
			"check: lint docs",
			"",
			".PHONY: lint",
			"lint:",
			"\tgolangci-lint run",
			"",
			".PHONY: docs",
			"docs:",
			"\t@go run ./scripts/gates docs",
			"",
			".PHONY: live",
			"live:",
			"\t@go run ./scripts/livefavro",
		),
		fxGolangci: textLines(
			`version: "2"`,
			"linters:",
			"  default: none",
			"  enable:",
			"    - errcheck",
			"    - forbidigo # stdout carries only frames",
			"  settings:",
			"    govet:",
			"      enable:",
			"        - nilness",
			"    misspell:",
			"      locale: US # American English, as D-01 says",
			"formatters:",
			"  enable:",
			"    - gofmt",
		),
		fxGitignore: textLines("internal/generated/", "# Generated, as docs/runbook.md#steps says (D-01)."),
		fxGitleaks: textLines(
			`title = "fixture"`,
			"# Fixtures are generated (D-01).",
			"[allowlist]",
			"paths = ['''internal/favroapi/testdata/'''] # synthetic, as D-01 says",
		),
		"go.mod": textLines("module example.com/fixture", "", "go 1.21"),
		fxGates:  textLines("package main", "", "func main() {}"),
		fxClient: textLines(
			"package favroapi",
			"",
			"// Retries back off (D-01), as measured live.",
			"// A redirect is followed once, as RFC 9110 §15.4 allows.",
			`func Retry() string { return "D-99 and §4 in a string are data" }`,
			"",
			"// Tags are listed first. Checked by the live",
			`// step "list tags", and by the live steps "the tag`,
			`// is gone" and "ping the server".`,
			"func Tags() {}",
			"",
			"// Deletes need confirm. Checked by the live steps",
			`// "the tag is gone", "list users" and "the collection reads back as organization".`,
			"func Delete() {}",
			"",
			`// Sharing widens. Checked by the live steps "the collection reads back as organization"`,
			`// and "list a widget's columns", and by the live step "list tags".`,
			"func Widen() {}",
		),
		fxClientTest: textLines(
			"package favroapi",
			"",
			`import "testing"`,
			"",
			"func TestRetry(t *testing.T) { _ = Retry() }",
			"",
			"func FuzzRetry(f *testing.F) { f.Fuzz(func(*testing.T, string) { _ = Retry() }) }",
			"",
			"func BenchmarkRetry(b *testing.B) { _ = Retry() }",
			"",
			"func ExampleRetry() { _ = Retry() }",
		),
		fxDriver:                           textLines("package main", "", "func main() {}"),
		"internal/favroapi/testdata/pg.md": textLines("A fixture page citing E-777 and §5, and `internal/gone`."),
		fxServer:                           textLines("package main", "", "func main() {}"),
		fxRecord: textLines(
			"# method\tpath\tverdict\treason",
			"POST\t/webhooks\tout\tpush notifications need an endpoint the server does not have (D-01)",
		),
		fxFields: textLines(
			"# resource\tfield\treason",
			"Card\tcolor\tthe server writes no colors (D-03)",
		),
		fxWaived: textLines(
			"# KEY\tREASON",
			"favro_ping.verbose\tthe live step \"ping the server\" covers it (D-01)",
		),
		fxChangelog: textLines(
			"# Changelog",
			"",
			"## [Unreleased]",
			"",
			"- Retries back off (D-01), as `docs/architecture.md#codemap` says.",
			"",
			"## [1.0.0]",
			"",
			"- Cites D-99 and `internal/gone`, true when written.",
			"",
			"[Unreleased]: https://example.com/compare/v1.0.0...HEAD",
			"[1.0.0]: https://example.com/releases/v1.0.0",
		),
		fxArch: textLines(
			"# Architecture",
			"",
			"## Codemap",
			"",
			"- `internal/favroapi` owns the REST client, in `internal/favroapi/client.go`.",
			"- `cmd/server` is the binary, and `scripts/livefavro` drives it live.",
			"",
			"## Decisions",
			"",
			"| ID | Decision | Status | Held by |",
			"| --- | --- | --- | --- |",
			fxRow01,
			fxRow02,
			fxRow03,
			"",
			"### D-01 Retries back off",
			"",
			"Favro answers a burst with 429, so a retry waits.",
			"",
			"```markdown",
			"### D-09 An example entry, not a decision",
			"It has no Rejected or Cost line.",
			"```",
			"",
			"Rejected: retrying at once, which meets the same 429.",
			"Cost: a burst takes longer to drain.",
			"",
			"### D-02 Retry forever",
			"",
			"Rejected: giving up, which drops the write.",
			"Cost: a write can wait forever.",
			"",
			"### D-03 Retry three times",
			"",
			"Three tries replace D-02, since a write that waits forever never reports.",
			"",
			"Rejected: retrying forever.",
			"Cost: a fourth 429 fails the call.",
			"",
			"Next ID: D-04",
		),
		fxRunbook: textLines(
			"# Runbook",
			"",
			"## Steps",
			"",
			"See [the codemap](architecture.md#codemap) and [the readme](../README.md).",
			"The retry is in `internal/favroapi/client.go`, linked as [the client](../internal/favroapi/client.go).",
			"",
			"## Steps",
			"",
			"Follow the old runbook, in [the server](/cmd/server).",
			"",
			"Back to [the first steps](#steps).",
			"",
			"[map]: architecture.md#codemap",
		),
		fxReadme: textLines(
			"# Fixture",
			"",
			"Read the [decisions](docs/architecture.md#decisions) and [the second steps](docs/runbook.md#steps-1).",
			"Code cites `docs/architecture.md#d-01-retries-back-off`.",
			"A redirect is followed once, as RFC 9110 §15.4 allows.",
			"The [spec](https://example.com/spec#§2) says so too.",
			"",
			"```text",
			"§3, D-99 and [a link](nowhere.md) in a fence are an example.",
			"```",
		),
		fxAgent: textLines(
			"# CLAUDE.md",
			"",
			"Read `docs/architecture.md` and `docs/architecture.md#codemap` first.",
			"The client is in `internal/favroapi/`, and `go.mod` names the module.",
			"Run `make check`, or `go run ./scripts/gates docs` alone; `scripts/gates smoke` needs a binary.",
			"A made-up id says so: `cards/SyntheticCardAAAA`. Streams are named in `main`, never in `cmd/*/x.go`.",
		),
	}
}

// docsEdit changes one fixture file: old replaced by new, the whole file
// written when old is empty, or the file removed when both are.
type docsEdit struct{ file, old, new string }

func replace(file, from, to string) docsEdit { return docsEdit{file, from, to} }
func add(file, body string) docsEdit         { return docsEdit{file, "", body} }
func drop(file string) docsEdit              { return docsEdit{file: file} }

// docsEdited is the fixture with the edits applied. An edit whose old
// text is not there fails the test, so a fixture change cannot quietly
// turn a reject row into a copy of the accepted checkout.
func docsEdited(t *testing.T, edits ...docsEdit) map[string]string {
	t.Helper()
	return applyEdits(t, docsFixture(), edits...)
}

// applyEdits is files with the edits applied, as docsEdited applies them.
func applyEdits(t *testing.T, files map[string]string, edits ...docsEdit) map[string]string {
	t.Helper()
	for _, e := range edits {
		switch body := files[e.file]; {
		case e.old == "" && e.new == "":
			delete(files, e.file)
		case e.old == "":
			files[e.file] = e.new
		case !strings.Contains(body, e.old):
			t.Fatalf("the fixture's %s does not contain %q", e.file, e.old)
		default:
			files[e.file] = strings.Replace(body, e.old, e.new, 1)
		}
	}
	return files
}

// codemapNames adds a line naming pkg to the fixture's codemap, for a
// row that adds a package.
func codemapNames(pkg string) docsEdit {
	return replace(fxArch, "drives it live.\n", "drives it live.\n- `"+pkg+"` is a package.\n")
}

// docsTree writes files to a fresh directory and returns it.
func docsTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	putAll(t, root, files)
	return root
}

// docsFixtureFloors keeps the floors out of the rows whose subject is not a
// floor. The fixture is far smaller than the repository, and a pass that
// reads nothing still fails.
var docsFixtureFloors = docsFloors{links: 1, spanPaths: 1, packages: 1, liveSteps: 1}

// docsRun runs every check over files, listed as git lists them, held to
// floors. Listing a checkout is docsFiles' part, and the tests that need
// git test it.
func docsRun(t *testing.T, files map[string]string, floors docsFloors) *docsCheck {
	t.Helper()
	g := newDocsCheck(docsTree(t, files), slices.Sorted(maps.Keys(files)), floors)
	g.run()
	return g
}

// docsCase is one checkout and every problem the gate reports for it. A
// nil want means the gate accepts it.
type docsCase struct {
	name  string
	edits []docsEdit
	want  []string
}

// checkDocsCases runs each row against its own checkout, in parallel,
// and compares the whole problem list, so a row also fails on a problem
// it did not expect.
func checkDocsCases(t *testing.T, cases []docsCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkDocs(t, docsFixtureFloors, tc.edits, tc.want)
		})
	}
}

// checkDocs runs the checks, held to floors, over the fixture with edits,
// and wants exactly the problems want, or none when want is nil.
func checkDocs(t *testing.T, floors docsFloors, edits []docsEdit, want []string) {
	t.Helper()
	if got := docsRun(t, docsEdited(t, edits...), floors).problems; !slices.Equal(got, want) {
		t.Errorf("problems\n got %q\nwant %q", got, want)
	}
}

func TestDocsGateAcceptsAConsistentCheckout(t *testing.T) {
	t.Parallel()
	checkDocsCases(t, []docsCase{
		{name: "the fixture as it stands"},
		{"a Go file under testdata is neither parsed nor read for citations", []docsEdit{
			add("internal/favroapi/testdata/fixture.go", "package fixture\n\n// Cites D-99.\nfunc {\n"),
		}, nil},
		{"footnotes, and a link out of the repository", []docsEdit{
			replace(fxReadme, "says so too.", "says so too.[^spec] See [issue 51](../../issues/51)."),
			replace(fxReadme, "```text", "[^spec]: The spec's own words.\n\n```text"),
		}, nil},
		{"a nested doc's anchor in a Go comment, and nested names that are not references", []docsEdit{
			add("internal/favroapi/NOTES.md", textLines("# Notes", "", "## Setup")),
			replace(fxClient, "as measured live.", "as measured live; ./internal/favroapi/NOTES.md#setup says how.\n"+
				"// Each run writes out/summary.md, as https://example.com/x/blob/main/MANIFEST.md says."),
		}, nil},
		{"a doc in a Go comment read from the file's directory, up and out of it", []docsEdit{
			replace(fxClient, "as measured live.", "as measured live; testdata/pg.md, ../../README.md#fixture and ../../../outside.md say so."),
		}, nil},
		{"a doc path inside a URL or a query string in a Go comment", []docsEdit{
			replace(fxClient, "as measured live.", "as measured live, at https://pkg.go.dev/example.com/fixture@v1.0.0/RUNBOOK.md#setup\n"+
				"// and at /render?page=docs/gone.md."),
		}, nil},
		{"a root doc's anchor, and a lowercase file name, in a Go comment", []docsEdit{
			replace(fxClient, "§15.4 allows.", "§15.4 allows, as README.md#fixture says.\n// Each retry is logged to retries.md."),
		}, nil},
		{"a doc path in a string of a test-helper package, which is fixture content", []docsEdit{
			codemapNames("internal/favroapi/favroapitest"),
			add("internal/favroapi/favroapitest/fake.go", textLines(
				"package favroapitest",
				"",
				`var files = map[string]string{"docs/guide.md": "# Guide"}`,
			)),
		}, nil},
		{"a bare file name in a Go string, and a doc path in a test's string", []docsEdit{
			replace(fxClient, "D-99 and §4", "D-99, §4 and NOTES.md"),
			replace(fxClientTest, "_ = Retry()", `_ = Retry() + "docs/gone.md"`),
		}, nil},
		{"an escaped pipe in an index cell, unescaped to match the entry", []docsEdit{
			replace(fxArch, "| D-03 | Retry three times |", "| D-03 | Retry `a \\| b` times |"),
			replace(fxArch, "### D-03 Retry three times", "### D-03 Retry `a | b` times"),
		}, nil},
		{"a link written in a code span", []docsEdit{
			replace(fxReadme, "says so too.", "says so too. Write `[x](docs/example.md#slug)` for a link."),
		}, nil},
		{"a placeholder or a wildcard where a make target, a gates command or a path goes", []docsEdit{
			replace(fxAgent, "needs a binary.", "needs a binary; `make <target>` and `go run ./scripts/gates <name>` run one,\n"+
				"`make ...` and `scripts/gates ...` run more, and `go test ./internal/gone/...` tests them."),
		}, nil},
		{"a gates command that runs by hand, in no pipeline", []docsEdit{
			replace(fxAgent, "`scripts/gates smoke` needs a binary.", "`scripts/gates smoke` needs a binary and `scripts/gates api-diff` a network."),
			replace(fxArch, "| forbidigo, lint, review |", "| forbidigo, api-diff, review |"),
		}, nil},
		{"a workflow by its bare name, and an archive's file, which is not a path here", []docsEdit{
			add(".github/workflows/ci.yml", textLines("name: ci")),
			replace(fxAgent, "`go.mod` names the module", "`go.mod` names the module, `ci.yml` checks it and `checksums.txt` lists an archive"),
		}, nil},
		{"a bare file name in a nested doc, read from its directory, then the root, then as a workflow", []docsEdit{
			add(".github/workflows/ci.yml", textLines("name: ci")),
			replace(fxRunbook, "The retry is in", "As `architecture.md#codemap`, `go.mod` and `ci.yml` say, the retry is in"),
		}, nil},
		{"a generated table under a package's testdata, which is not a record", []docsEdit{
			add("internal/favroapi/testdata/golden.tsv", textLines("method\tnote", "cards.get\tD-99 and E-12")),
		}, nil},
		{"what a command writes beside the records: the API snapshot", []docsEdit{
			add("testdata/api-surface.json", textLines(`{"note": "D-99, §4 and E-12"}`)),
		}, nil},
		{"IDs in a YAML or TOML value, after a # with no space before it, and in a Makefile recipe", []docsEdit{
			add(".github/workflows/ci.yml", textLines(
				"name: ci",
				"jobs:",
				"  test:",
				"    steps:",
				"      - run: echo D-99 §4 https://example.com/#E-12",
			)),
			replace(fxGitleaks, `title = "fixture"`, `title = "D-99#E-12"`),
			replace(fxMakefile, "\tgolangci-lint run", "\tgolangci-lint run\n\t@echo D-99"),
		}, nil},
	})
}

// What a pass reads is in its summary, so "found nothing" and "looked at
// nothing" cannot read the same. A failure counts its problems and prints
// nothing. The live steps are livecover.Names'.
func TestDocsGateSaysWhatItRead(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	if err := docsRun(t, docsFixture(), docsFixtureFloors).verdict(&out); err != nil {
		t.Fatalf("the fixture failed: %v", err)
	}
	steps := strconv.Itoa(len(livecover.Names()))
	want := "docs ok: 3 decisions, 2 packages, " + steps + " live steps, 8 links and 14 code-span paths, in 19 files\n"
	if out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}

	out.Reset()
	edited := docsEdited(t, replace(fxClient, "(D-01)", "(D-07)"), replace(fxReadme, "says so too.", "says so too, as D-08 did."))
	err := docsRun(t, edited, docsFixtureFloors).verdict(&out)
	want = "2 problem(s):\n  README.md:6: cites D-08, which is not a decision\n  internal/favroapi/client.go:3: cites D-07, which is not a decision"
	if err == nil || err.Error() != want || out.Len() != 0 {
		t.Errorf("failed with %v after printing %q, want %q and nothing printed", err, out.String(), want)
	}
}

// The gate reads what git would commit, as it is on disk: an untracked
// doc is read and an ignored file is not. A file deleted in the worktree
// stays in the index until the deletion is staged, and has nothing to
// read.
func TestDocsGateReadsWhatGitWouldCommit(t *testing.T) {
	isolateGit(t)
	root := t.TempDir()
	t.Chdir(root)
	putAll(t, root, docsEdited(t,
		add("internal/old/old.go", "package old\n"),
		add("internal/generated/broken.go", "this is not Go\n"),
	))
	mustGit(t, "init", "-q")
	mustGit(t, "add", "-A")
	if err := os.Remove(filepath.Join(root, "internal", "old", "old.go")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "docs/new.md", "A new page cites D-07.\n")
	err := docsGateAt(root, docsFixtureFloors, io.Discard)
	if want := "1 problem(s):\n  docs/new.md:1: cites D-07, which is not a decision"; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

// Outside a git checkout the gate has no file list. It reached no
// verdict, so it says so and exits 2, as a gate that could not run does.
func TestDocsGateNeedsAGitCheckout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Setenv("LC_ALL", "C") // git's own words, untranslated
	err := docsGateAt(dir, docsFixtureFloors, io.Discard)
	if !errors.As(err, new(couldNotRun)) || exitCode(err) != 2 ||
		!strings.HasPrefix(err.Error(), "could not run: git -C "+dir+" ls-files: exit status 128: ") ||
		!strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("got %v; want a git ls-files failure that could not run and says why", err)
	}
}

func TestDocsGateHoldsDecisionIntegrity(t *testing.T) {
	t.Parallel()
	checkDocsCases(t, []docsCase{
		{"an ID in the index twice", []docsEdit{
			replace(fxArch, fxRow03, fxRow03+"\n"+fxRow03),
		}, []string{"docs/architecture.md:15: D-03 is in the index twice"}},
		{"an ID with two entries", []docsEdit{
			replace(fxArch, "Next ID: D-04", "### D-03 Retry three times\n\nRejected: none.\nCost: none.\n\nNext ID: D-04"),
		}, []string{"docs/architecture.md:40: D-03 has two entries"}},
		{"an ID at Next ID", []docsEdit{
			replace(fxArch, "Next ID: D-04", "Next ID: D-03"),
		}, []string{"docs/architecture.md:14: D-03 is not below Next ID D-03"}},
		{"an ID below Next ID that is gone", []docsEdit{
			replace(fxArch, "Next ID: D-04", "Next ID: D-05"),
		}, []string{"docs/architecture.md:40: D-04 is below Next ID D-05 and is missing; an ID is never deleted"}},
		{"no Next ID line", []docsEdit{
			replace(fxArch, "\n\nNext ID: D-04", ""),
		}, []string{`docs/architecture.md:38: the last line is not "Next ID: D-NN"`}},
		{"a status outside the set", []docsEdit{
			replace(fxArch, "| D-01 | Retries back off | accepted |", "| D-01 | Retries back off | proposed |"),
		}, []string{`docs/architecture.md:12: D-01 has status "proposed", want accepted, rejected, deprecated or superseded by D-NN`}},
		{"a superseded status that names no decision ID", []docsEdit{
			replace(fxArch, "| superseded by D-03 |", "| superseded by D-3 |"),
		}, []string{
			`docs/architecture.md:13: D-02 has status "superseded by D-3", want accepted, rejected, deprecated or superseded by D-NN`,
			`docs/architecture.md:13: "D-3" is not written as a decision ID like D-03`,
		}},
		{"a superseded status naming a decision that does not exist", []docsEdit{
			replace(fxArch, "| superseded by D-03 |", "| superseded by D-07 |"),
		}, []string{"docs/architecture.md:13: cites D-07, which is not a decision"}},
		{"an entry titled unlike its index row", []docsEdit{
			replace(fxArch, "| D-01 | Retries back off |", "| D-01 | Retries back off fast |"),
		}, []string{`docs/architecture.md:16: D-01's entry is titled "Retries back off" and its index row "Retries back off fast"`}},
		{"an entry with no Rejected line", []docsEdit{
			replace(fxArch, "Rejected: giving up, which drops the write.\n", ""),
		}, []string{`docs/architecture.md:28: D-02 has no "Rejected:" line`}},
		{"an entry with no Cost line", []docsEdit{
			replace(fxArch, "Cost: a fourth 429 fails the call.\n", ""),
		}, []string{`docs/architecture.md:33: D-03 has no "Cost:" line`}},
		{"an index row with no entry", []docsEdit{
			replace(fxArch, fxRow03, fxRow03+"\n| D-04 | Log every retry | accepted | live |"),
			replace(fxArch, "Next ID: D-04", "Next ID: D-05"),
		}, []string{"docs/architecture.md:15: D-04 has no entry"}},
		{"an entry with no index row", []docsEdit{
			replace(fxArch, "Next ID: D-04", "### D-04 Log every retry\n\nRejected: none.\nCost: none.\n\nNext ID: D-05"),
		}, []string{"docs/architecture.md:40: D-04 has no index row"}},
		{"an ID with one digit, which the citation scan also reads, reported once", []docsEdit{
			replace(fxArch, "| D-03 | Retry three", "| D-3 | Retry three"),
		}, []string{
			`docs/architecture.md:14: "D-3" is not written as a decision ID like D-03`,
			"docs/architecture.md:33: D-03 has no index row",
		}},
		{"a pipe left unescaped in an index cell, even in a code span", []docsEdit{
			replace(fxArch, "| D-03 | Retry three times |", "| D-03 | Retry `a | b` times |"),
		}, []string{
			"docs/architecture.md:14: an index row has 5 cells, want 4",
			"docs/architecture.md:33: D-03 has no index row",
		}},
		{"an index with a column too many", []docsEdit{
			replace(fxArch, "| Held by |\n| --- | --- | --- | --- |", "| Held by | Links |\n| --- | --- | --- | --- | --- |"),
		}, []string{
			`docs/architecture.md:10: the decision index has the columns ["ID" "Decision" "Status" "Held by" "Links"], ` +
				`want ["ID" "Decision" "Status" "Held by"]`,
			"docs/architecture.md: no decisions in the index: this gate would be looking at nothing",
			"docs/architecture.md:16: D-01 has no index row",
			"docs/architecture.md:28: D-02 has no index row",
			"docs/architecture.md:33: D-03 has no index row",
		}},
		{"an index that is not directly under its heading", []docsEdit{
			replace(fxArch, "## Decisions\n\n|", "## Decisions\n\nThe index.\n\n|"),
		}, []string{
			`docs/architecture.md:8: the decision index is not a table directly under "## Decisions"`,
			"docs/architecture.md: no decisions in the index: this gate would be looking at nothing",
			"docs/architecture.md:18: D-01 has no index row",
			"docs/architecture.md:30: D-02 has no index row",
			"docs/architecture.md:35: D-03 has no index row",
		}},
	})
}

func TestDocsGateHoldsHeldByToWhatExists(t *testing.T) {
	t.Parallel()
	checkDocsCases(t, []docsCase{
		{"a fuzz target, whose seeds go test runs", []docsEdit{
			replace(fxArch, "| TestRetry, smoke |", "| FuzzRetry, smoke |"),
		}, nil},
		{"a test function that does not exist", []docsEdit{
			replace(fxArch, "| TestRetry, smoke |", "| TestRetryLater, smoke |"),
		}, []string{`docs/architecture.md:12: D-01 is held by "TestRetryLater", ` + notAHolder}},
		{"a benchmark or an example, which go test does not run by name", []docsEdit{
			replace(fxArch, "| TestRetry, smoke |", "| BenchmarkRetry, ExampleRetry, smoke |"),
		}, []string{
			`docs/architecture.md:12: D-01 is held by "BenchmarkRetry", ` + notAHolder,
			`docs/architecture.md:12: D-01 is held by "ExampleRetry", ` + notAHolder,
		}},
		{"a make target beside a quoted variable is the only target", []docsEdit{
			replace(fxAgent, "`make check`", "`make verify LIVE_ARGS=\"-run Word\"`"),
		}, []string{"CLAUDE.md:5: names make verify, which is not a Makefile target"}},
		{"a gates command that does not exist", []docsEdit{
			replace(fxArch, "| TestRetry, smoke |", "| TestRetry, smok |"),
		}, []string{`docs/architecture.md:12: D-01 is held by "smok", ` + notAHolder}},
		{"a linter configured but not enabled", []docsEdit{
			replace(fxArch, "| forbidigo, lint, review |", "| misspell, lint, review |"),
		}, []string{`docs/architecture.md:14: D-03 is held by "misspell", ` + notAHolder}},
		{"a linter list whose keys carry a comment", []docsEdit{
			replace(fxGolangci, "linters:\n  default: none\n  enable:\n", "linters: # the checks\n  default: none\n  enable: # the list\n"),
		}, nil},
		{"a linter enabled only inside another linter's settings", []docsEdit{
			replace(fxArch, "| forbidigo, lint, review |", "| nilness, lint, review |"),
		}, []string{`docs/architecture.md:14: D-03 is held by "nilness", ` + notAHolder}},
		{"an accepted decision held by a dash", []docsEdit{
			replace(fxArch, "| forbidigo, lint, review |", "| — |"),
		}, []string{"docs/architecture.md:14: D-03 is accepted and held by —; an accepted decision names what holds it"}},
		{"a decision held by nothing", []docsEdit{
			replace(fxArch, "| TestRetry, smoke |", "| |"),
		}, []string{"docs/architecture.md:12: D-01 is held by nothing"}},
		// live is the Makefile's target, so it holds only while `make live` does.
		{"live, while the Makefile has its target", []docsEdit{
			replace(fxArch, "| TestRetry, smoke |", "| TestRetry, live |"),
		}, nil},
		{"live, with no make live", []docsEdit{
			replace(fxArch, "| TestRetry, smoke |", "| TestRetry, live |"),
			replace(fxMakefile, "\n.PHONY: live\nlive:\n\t@go run ./scripts/livefavro\n", ""),
		}, []string{`docs/architecture.md:12: D-01 is held by "live", ` + notAHolder}},
	})
}

// Evidence has no IDs: what was verified upstream is stated at the line
// that depends on it. An E- citation fails wherever another citation is read.
func TestDocsGateRefusesEvidenceIDs(t *testing.T) {
	t.Parallel()
	const noIDs = "; evidence has no IDs: state the behavior at the line that depends on it, with its source link"
	checkDocsCases(t, []docsCase{
		{"an evidence ID in a Go comment", []docsEdit{
			replace(fxClient, "as measured live.", "as measured in E-001."),
		}, []string{"internal/favroapi/client.go:3: cites E-001" + noIDs}},
		{"an evidence ID in a doc, at any width", []docsEdit{
			replace(fxArch, "Favro answers a burst with 429, so", "Favro answers a burst with 429 (E-12), so"),
		}, []string{"docs/architecture.md:18: cites E-12" + noIDs}},
		{"an evidence ID in a record's reason", []docsEdit{
			replace(fxRecord, "(D-01)", "(E-12)"),
		}, []string{"testdata/api-coverage.tsv:2: cites E-12" + noIDs}},
		{"an evidence ID in a fenced block", []docsEdit{
			replace(fxReadme, "§3, D-99 and", "§3, D-99, E-001 and"),
		}, nil},
		{"an evidence ID in a string literal", []docsEdit{
			replace(fxClient, "D-99 and §4", "D-99, E-001 and §4"),
		}, nil},
	})
}

// A comment names the live step that checks it, and the name has to be
// one livecover.Names gives: a step's or a -writes check's.
func TestDocsGateHoldsLiveStepsToTheLiveRun(t *testing.T) {
	t.Parallel()
	cites := "Code cites `docs/architecture.md#d-01-retries-back-off`."
	checkDocsCases(t, []docsCase{
		{"a Markdown paragraph, across a line break", []docsEdit{
			replace(fxReadme, cites, cites+" The live\nstep \"list the tags\" checks it."),
		}, []string{`README.md:5: names the live step "list the tags"` + undefined}},
		{"a Markdown paragraph naming a step and a check", []docsEdit{
			replace(fxReadme, cites, cites+" The live steps \"list tags\" and \"the tag is gone\" check it."),
		}, nil},
		{"a placeholder showing the form", []docsEdit{
			replace(fxReadme, cites, cites+" Cite it as `Checked by the live step \"<name>\".`"),
		}, nil},
		{"each record's reason", []docsEdit{
			replace(fxFields, "the server writes no colors (D-03)", "the live step \"a card is colored\" sends none (D-03)"),
			replace(fxWaived, `"ping the server"`, `"ping it"`),
			add(fxClaims, textLines("# file\tfield\treason", "internal/tools/x.go\tdry_run\tthe live step \"a dry run\" shows it")),
		}, []string{
			`testdata/api-fields-waived.tsv:2: names the live step "a card is colored"` + undefined,
			`testdata/live-cover-waived.tsv:2: names the live step "ping it"` + undefined,
			`testdata/outcome-claims.tsv:2: names the live step "a dry run"` + undefined,
		}},
		{"a name wrapped across comment lines", []docsEdit{
			replace(fxClient, "// is gone\"", "// is deleted\""),
		}, []string{`internal/favroapi/client.go:8: names the live step "the tag is deleted"` + undefined}},
		{"the second name of a list", []docsEdit{
			replace(fxClient, `"list users" and`, `"list the users" and`),
		}, []string{`internal/favroapi/client.go:13: names the live step "list the users"` + undefined}},
		{"a step's tool, which is not its name", []docsEdit{
			replace(fxClient, `"list users" and`, `"favro_list_users" and`),
		}, []string{`internal/favroapi/client.go:13: names the live step "favro_list_users"` + undefined}},
	})
}

func TestDocsGateResolvesCitations(t *testing.T) {
	t.Parallel()
	checkDocsCases(t, []docsCase{
		{"a superseded decision cited from a Go comment", []docsEdit{
			replace(fxClient, "(D-01)", "(D-02)"),
		}, []string{"internal/favroapi/client.go:3: cites D-02, which is superseded by D-03"}},
		{"a deprecated decision cited from a doc", []docsEdit{
			replace(fxArch, fxRow03, fxRow03+"\n| D-04 | Log every retry | deprecated | — |"),
			replace(fxArch, "Next ID: D-04", "### D-04 Log every retry\n\nRejected: none.\nCost: none.\n\nNext ID: D-05"),
			replace(fxReadme, "#d-01-retries-back-off`.", "#d-01-retries-back-off`. Logging was D-04."),
		}, []string{"README.md:4: cites D-04, which is deprecated"}},
		{"an unknown decision cited twice on a line, reported once", []docsEdit{
			replace(fxClient, "(D-01)", "(D-07, and D-07 again)"),
		}, []string{"internal/favroapi/client.go:3: cites D-07, which is not a decision"}},
		{"an unknown decision in each record's reason", []docsEdit{
			replace(fxRecord, "(D-01)", "(D-07)"),
			replace(fxFields, "(D-03)", "(D-05), as §4 says"),
		}, []string{
			"testdata/api-coverage.tsv:2: cites D-07, which is not a decision",
			"testdata/api-fields-waived.tsv:2: cites D-05, which is not a decision",
			"testdata/api-fields-waived.tsv:2: cites a section by number (§4); " + bySlug,
		}},
		{"a link slug no heading has", []docsEdit{
			replace(fxReadme, "docs/architecture.md#decisions", "docs/architecture.md#decision"),
		}, []string{"README.md:3: links to docs/architecture.md#decision, and docs/architecture.md has no heading with that slug"}},
		{"a code span slug no heading has", []docsEdit{
			replace(fxReadme, "#d-01-retries-back-off`", "#d-01-retry`"),
		}, []string{"README.md:4: names docs/architecture.md#d-01-retry, and docs/architecture.md has no heading with that slug"}},
		{"a repeated heading's suffix past the last repeat", []docsEdit{
			replace(fxReadme, "#steps-1", "#steps-2"),
		}, []string{"README.md:3: links to docs/runbook.md#steps-2, and docs/runbook.md has no heading with that slug"}},
		{"a same-file anchor no heading has", []docsEdit{
			replace(fxRunbook, "(#steps)", "(#step)"),
		}, []string{"docs/runbook.md:12: links to docs/runbook.md#step, and docs/runbook.md has no heading with that slug"}},
		{"a reference definition no heading answers", []docsEdit{
			replace(fxRunbook, "[map]: architecture.md#codemap", "[map]: architecture.md#codemaps"),
		}, []string{"docs/runbook.md:14: links to docs/architecture.md#codemaps, and docs/architecture.md has no heading with that slug"}},
		{"a link to a doc that does not exist", []docsEdit{
			replace(fxReadme, "says so too.", "says so too, in [the guide](docs/gone.md)."),
		}, []string{"README.md:6: links to docs/gone.md, which does not exist"}},
		{"a link written from the root instead of the linking file", []docsEdit{
			replace(fxRunbook, "](architecture.md#codemap)", "](docs/architecture.md#codemap)"),
		}, []string{"docs/runbook.md:5: links to docs/docs/architecture.md, which does not exist"}},
		{"a docs citation in a Go comment to a file that does not exist", []docsEdit{
			replace(fxClient, "as measured live.", "as measured live and in docs/retries.md#backoff."),
		}, []string{"internal/favroapi/client.go:3: cites docs/retries.md, which does not exist"}},
		{"a bare section number in a Go comment", []docsEdit{
			replace(fxClient, "as RFC 9110 §15.4 allows", "as §15.4 allows"),
		}, []string{"internal/favroapi/client.go:4: cites a section by number (§15); " + bySlug}},
		{"a section number not right after an RFC or inside a URL on its line", []docsEdit{
			replace(fxClient, "RFC 9110 §15.4 allows", "RFC 9110 allows and §15.4 says"),
			replace(fxReadme, "RFC 9110 §15.4", "RFC 9110  §15.4"),
			replace(fxReadme, "spec#§2) says", "spec) §2 says"),
		}, []string{
			"README.md:5: cites a section by number (§15); " + bySlug,
			"README.md:6: cites a section by number (§2); " + bySlug,
			"internal/favroapi/client.go:4: cites a section by number (§15); " + bySlug,
		}},
		{"a bare section number in the changelog's Unreleased section", []docsEdit{
			replace(fxChangelog, "#codemap` says.", "#codemap` and §3 say."),
		}, []string{"CHANGELOG.md:5: cites a section by number (§3); " + bySlug}},
		{"a bare section number in a released changelog section", []docsEdit{
			replace(fxChangelog, "Cites D-99 and", "Cites D-99, §3 and"),
		}, nil},
		{"IDs written at the wrong width, in a Go comment and a doc", []docsEdit{
			replace(fxClient, "(D-01)", "(D-1)"),
			replace(fxReadme, "says so too.", "says so too, as D-001 did."),
		}, []string{
			`README.md:6: "D-001" is not written as a decision ID like D-01`,
			`internal/favroapi/client.go:3: "D-1" is not written as a decision ID like D-01`,
		}},
		{"a one-line code span in fence backticks opens no fence", []docsEdit{
			replace(fxReadme, "says so too.\n", "says so too.\n```go``` is a span, not a fence.\nAs D-07 said.\n"),
		}, []string{"README.md:8: cites D-07, which is not a decision"}},
		{"a nested doc in a Go comment that does not exist, or has no such heading", []docsEdit{
			add("internal/favroapi/NOTES.md", textLines("# Notes", "", "## Setup")),
			replace(fxClient, "as measured live.", "as measured live, packaging/README.md and internal/favroapi/NOTES.md#teardown."),
		}, []string{
			"internal/favroapi/client.go:3: cites packaging/README.md, which does not exist",
			"internal/favroapi/client.go:3: cites internal/favroapi/NOTES.md#teardown, and internal/favroapi/NOTES.md has no heading with that slug",
		}},
		{"a doc in a Go comment missing from the file's directory, or up from it", []docsEdit{
			replace(fxClient, "as measured live.", "as measured live, testdata/gone.md and ../gone.md."),
		}, []string{
			"internal/favroapi/client.go:3: cites internal/favroapi/testdata/gone.md, which does not exist",
			"internal/favroapi/client.go:3: cites internal/gone.md, which does not exist",
		}},
		{"a root doc in a Go comment that does not exist, or has no such heading", []docsEdit{
			replace(fxClient, "as measured live.", "as measured live, RUNBOOK.md and README.md#retries."),
		}, []string{
			"internal/favroapi/client.go:3: cites RUNBOOK.md, which does not exist",
			"internal/favroapi/client.go:3: cites README.md#retries, and README.md has no heading with that slug",
		}},
		{"doc paths in a Go string that do not resolve", []docsEdit{
			replace(fxClient, "in a string are data", "in a string are data; docs/setup.md and README.md#setup are not"),
		}, []string{
			"internal/favroapi/client.go:5: names docs/setup.md, which does not exist",
			"internal/favroapi/client.go:5: names README.md#setup, and README.md has no heading with that slug",
		}},
		{"a doc path in an ordinary package's string, and in a test-helper package's comment", []docsEdit{
			codemapNames("internal/favroapi/favroapiutil"),
			codemapNames("internal/favroapi/favroapitest"),
			add("internal/favroapi/favroapiutil/fake.go", textLines(
				"package favroapiutil",
				"",
				`var files = map[string]string{"docs/guide.md": "# Guide"}`,
			)),
			add("internal/favroapi/favroapitest/fake.go", textLines(
				"package favroapitest",
				"",
				"// The fake's files, as docs/fake.md lists them.",
				`var files = map[string]string{"docs/guide.md": "# Guide"}`,
			)),
		}, []string{
			"internal/favroapi/favroapitest/fake.go:3: cites docs/fake.md, which does not exist",
			"internal/favroapi/favroapiutil/fake.go:3: names docs/guide.md, which does not exist",
		}},
		{"a comment line in each kind of config file", []docsEdit{
			replace(fxMakefile, "(D-03)", "(D-3)"),
			replace(fxGitignore, "#steps says", "#stairs says"),
			replace(fxGitleaks, "generated (D-01)", "generated (D-07)"),
			add(".github/workflows/ci.yml", textLines(
				"name: ci",
				"jobs:",
				"  test:",
				"    steps:",
				"      # Pinned, as E-12 found.",
				"      - run: go test ./...",
			)),
			add(parityGoreleaser, textLines("version: 2", "# Archives follow §3.")),
		}, []string{
			".github/workflows/ci.yml:5: cites E-12; evidence has no IDs: state the behavior at the line that depends on it, with its source link",
			".gitignore:2: cites docs/runbook.md#stairs, and docs/runbook.md has no heading with that slug",
			".gitleaks.toml:2: cites D-07, which is not a decision",
			".goreleaser.yaml:2: cites a section by number (§3); " + bySlug,
			`Makefile:1: "D-3" is not written as a decision ID like D-03`,
		}},
		{"a trailing comment on a YAML or TOML line", []docsEdit{
			replace(fxGolangci, "as D-01 says", "as D-07 says"),
			replace(fxGitleaks, "synthetic, as D-01 says", "synthetic, as D-08 says"),
		}, []string{
			".gitleaks.toml:4: cites D-08, which is not a decision",
			".golangci.yml:12: cites D-07, which is not a decision",
		}},
		{"a TOML multi-line string's # is text, and a comment after it is read", []docsEdit{
			replace(fxGitleaks, "as D-01 says\n", "as D-01 says\n"+textLines(
				"description = '''",
				"# D-99 is text in a literal string,",
				"'''",
				`note = """`,
				"as is a # before D-97 in a basic one.",
				`""" # as D-08 says`,
			)),
		}, []string{".gitleaks.toml:10: cites D-08, which is not a decision"}},
	})
}

func TestDocsGateHoldsPathsToTheRepository(t *testing.T) {
	t.Parallel()
	checkDocsCases(t, []docsCase{
		{"a path in CLAUDE.md that does not exist", []docsEdit{
			replace(fxAgent, "`internal/favroapi/`", "`internal/api/`"),
		}, []string{"CLAUDE.md:4: names internal/api/, which does not exist"}},
		{"a path in another doc that does not exist", []docsEdit{
			replace(fxRunbook, "`internal/favroapi/client.go`", "`internal/favroapi/retry.go`"),
		}, []string{"docs/runbook.md:6: names internal/favroapi/retry.go, which does not exist"}},
		{"a package in the codemap that does not exist", []docsEdit{
			replace(fxArch, "drives it live.", "drives it live.\n- `internal/gone` was removed."),
		}, []string{"docs/architecture.md:7: names internal/gone, which does not exist"}},
		{"a link to a file that does not exist", []docsEdit{
			replace(fxRunbook, "(../internal/favroapi/client.go)", "(../internal/favroapi/retry.go)"),
		}, []string{"docs/runbook.md:6: links to internal/favroapi/retry.go, which does not exist"}},
		{"a link from the root to a directory that does not exist", []docsEdit{
			replace(fxRunbook, "(/cmd/server)", "(/cmd/client)"),
		}, []string{"docs/runbook.md:10: links to cmd/client, which does not exist"}},
		{"a file:line pointer, with punctuation after it", []docsEdit{
			replace(fxRunbook, "`internal/favroapi/client.go`", "`(see internal/favroapi/client.go:3:5),` and `internal/favroapi/client.go:3`"),
		}, nil},
		{"a file:line pointer to a file that does not exist", []docsEdit{
			replace(fxRunbook, "`internal/favroapi/client.go`", "`internal/favroapi/retry.go:12`"),
		}, []string{"docs/runbook.md:6: names internal/favroapi/retry.go:12, which does not exist"}},
		{"a Go name, plain or qualified, in a package that exists", []docsEdit{
			replace(fxRunbook, "`internal/favroapi/client.go`", "`internal/favroapi.Retry` and `internal/favroapi.Client.Do`"),
		}, nil},
		{"a missing file whose name less its extension is a directory", []docsEdit{
			codemapNames("internal/service/approver"),
			add("internal/service/approver/main.go", "package main\n"),
			replace(fxRunbook, "`internal/favroapi/client.go`", "`internal/service/approver.go` and `scripts/gates.sh`"),
		}, []string{
			"docs/runbook.md:6: names internal/service/approver.go, which does not exist",
			"docs/runbook.md:6: names scripts/gates.sh, which does not exist",
		}},
		{"a Go name in a package that does not exist", []docsEdit{
			replace(fxRunbook, "`internal/favroapi/client.go`", "`internal/api.Retry`"),
		}, []string{"docs/runbook.md:6: names internal/api.Retry, which does not exist"}},
		{"a root file and a source directory that are gone, recognized by their shape", []docsEdit{
			replace(fxAgent, "`go.mod` names the module", "`go.mod` names the module, `.goreleaser.yaml` the archives and `packaging/mcpb/manifest.json` the bundle"),
		}, []string{
			"CLAUDE.md:4: names .goreleaser.yaml, which does not exist",
			"CLAUDE.md:4: names packaging/mcpb/manifest.json, which does not exist",
		}},
		{"a bare workflow name no workflow has", []docsEdit{
			replace(fxAgent, "`go.mod` names the module", "`go.mod` names the module, and `release.yml` releases it"),
		}, []string{"CLAUDE.md:4: names release.yml, which does not exist"}},
		{"a bare file name in a nested doc, with no such heading in its directory, or found nowhere", []docsEdit{
			replace(fxRunbook, "The retry is in", "As `architecture.md#codemaps` and `gone.md` say, the retry is in"),
		}, []string{
			"docs/runbook.md:6: names docs/architecture.md#codemaps, and docs/architecture.md has no heading with that slug",
			"docs/runbook.md:6: names gone.md, which does not exist",
		}},
		{"a file named as a directory", []docsEdit{
			replace(fxAgent, "`go.mod`", "`go.mod/`"),
		}, []string{"CLAUDE.md:4: names go.mod/, which is not a directory"}},
		{"a make target that does not exist", []docsEdit{
			replace(fxAgent, "`make check`", "`make verify`"),
		}, []string{"CLAUDE.md:5: names make verify, which is not a Makefile target"}},
		{"a gates command that does not exist", []docsEdit{
			replace(fxAgent, "`scripts/gates smoke`", "`scripts/gates smok`"),
		}, []string{"CLAUDE.md:5: names scripts/gates smok, which is not a gates command"}},
		{"no CLAUDE.md", []docsEdit{
			drop(fxAgent),
		}, []string{"CLAUDE.md is missing"}},
	})
}

// A gate that read nothing passes everything, so each input it depends on
// reading has a floor, and each row here sets one off.
func TestDocsGateRefusesToReadNothing(t *testing.T) {
	t.Parallel()
	checkDocsCases(t, []docsCase{
		{"no Go files", []docsEdit{
			drop(fxClient), drop(fxClientTest), drop(fxServer), drop(fxDriver), drop(fxGates),
		}, []string{
			"no .go files read: this gate would be looking at nothing",
			`docs/architecture.md:12: D-01 is held by "TestRetry", ` + notAHolder,
			"CLAUDE.md:5: names ./scripts/gates, which does not exist",
			"CLAUDE.md:5: names scripts/gates, which does not exist",
			"docs/architecture.md:5: names internal/favroapi/client.go, which does not exist",
			"docs/architecture.md:6: names cmd/server, which does not exist",
			"docs/architecture.md:6: names scripts/livefavro, which does not exist",
			"docs/runbook.md:6: links to internal/favroapi/client.go, which does not exist",
			"docs/runbook.md:6: names internal/favroapi/client.go, which does not exist",
			"docs/runbook.md:10: links to cmd/server, which does not exist",
			"0 package directories under internal/ and cmd/, fewer than 1: the tree is not being read",
		}},
		{"an index with no rows", []docsEdit{
			replace(fxArch, fxRow01+"\n"+fxRow02+"\n"+fxRow03+"\n", ""),
		}, []string{
			"docs/architecture.md: no decisions in the index: this gate would be looking at nothing",
			"docs/architecture.md:13: D-01 has no index row",
			"docs/architecture.md:25: D-02 has no index row",
			"docs/architecture.md:30: D-03 has no index row",
		}},
		{"a config that enables no linter", []docsEdit{
			replace(fxGolangci, "  enable:\n    - errcheck\n    - forbidigo # stdout carries only frames\n", ""),
		}, []string{
			".golangci.yml: no enabled linters read: a Held by naming one would never match",
			`docs/architecture.md:14: D-03 is held by "forbidigo", ` + notAHolder,
		}},
		// The reader takes a block list only. A flow list is read as none,
		// and the floor says so rather than passing over it.
		{"linters listed in flow style", []docsEdit{
			replace(fxGolangci, "  enable:\n    - errcheck\n    - forbidigo # stdout carries only frames\n", "  enable: [errcheck, forbidigo]\n"),
		}, []string{
			".golangci.yml: no enabled linters read: a Held by naming one would never match",
			`docs/architecture.md:14: D-03 is held by "forbidigo", ` + notAHolder,
		}},
		{"a Makefile with no rules", []docsEdit{
			add(fxMakefile, "GO ?= go\n"),
		}, []string{
			"Makefile: no targets read: a Held by naming one would never match",
			`docs/architecture.md:14: D-03 is held by "lint", ` + notAHolder,
			"CLAUDE.md:5: names make check, which is not a Makefile target",
		}},
		{"no architecture document", []docsEdit{
			drop(fxArch),
		}, []string{
			"docs/architecture.md is missing",
			"CHANGELOG.md:5: names docs/architecture.md#codemap, which does not exist",
			"CLAUDE.md:3: names docs/architecture.md, which does not exist",
			"CLAUDE.md:3: names docs/architecture.md#codemap, which does not exist",
			"README.md:3: links to docs/architecture.md, which does not exist",
			"README.md:4: names docs/architecture.md#d-01-retries-back-off, which does not exist",
			"docs/runbook.md:5: links to docs/architecture.md, which does not exist",
			"docs/runbook.md:14: links to docs/architecture.md, which does not exist",
		}},
		{"a Go file that does not parse", []docsEdit{
			add("internal/favroapi/broken.go", "package favroapi\n\nfunc {\n"),
		}, []string{"gates cannot parse internal/favroapi/broken.go: internal/favroapi/broken.go:3:6: expected 'IDENT', found '{'"}},
	})
}

// Each pass has a floor of its own, so one that keeps reading cannot make
// up for another that stops. Each row raises one floor past what the
// fixture holds: its links, code-span paths and packages by one, and the
// live steps, which are internal/livecover's however many it holds, past
// any it could.
func TestDocsGateHoldsEachPassToItsFloor(t *testing.T) {
	t.Parallel()
	atFloor := docsFloors{links: 8, spanPaths: 14, packages: 2, liveSteps: 1}
	raised := func(raise func(*docsFloors)) docsFloors {
		f := atFloor
		raise(&f)
		return f
	}
	for _, tc := range []struct {
		name   string
		floors docsFloors
		want   []string
	}{
		{"each pass at its floor", atFloor, nil},
		{
			"links one under their floor", raised(func(f *docsFloors) { f.links++ }),
			[]string{"8 Markdown links checked, fewer than 9: the links are not being read"},
		},
		{
			"code-span paths one under their floor", raised(func(f *docsFloors) { f.spanPaths++ }),
			[]string{"14 code-span paths checked, fewer than 15: the code spans are not being read"},
		},
		{
			"packages one under their floor", raised(func(f *docsFloors) { f.packages++ }),
			[]string{"2 package directories under internal/ and cmd/, fewer than 3: the tree is not being read"},
		},
		{
			"live steps under their floor", raised(func(f *docsFloors) { f.liveSteps = 100_000 }),
			[]string{fmt.Sprintf("%d live steps read from internal/livecover, fewer than 100000: "+
				"the driver's steps are not being read", len(livecover.Names()))},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkDocs(t, tc.floors, nil, tc.want)
		})
	}
}

// Under [Unreleased]'s Removed heading a code span or a live step names
// what the release takes away, so it need not exist. Decisions and links
// are still held, and only the changelog has the section.
func TestDocsGateLetsTheRemovedSectionNameWhatIsGone(t *testing.T) {
	t.Parallel()
	unreleased := "#codemap` says.\n"
	checkDocsCases(t, []docsCase{
		{"a removed path, target and live step", []docsEdit{
			replace(fxChangelog, unreleased, unreleased+"\n### Removed\n\n- `internal/gone`, `make gone` and the live step \"gone now\".\n"),
		}, nil},
		{"a level-4 heading inside Removed keeps it", []docsEdit{
			replace(fxChangelog, unreleased, unreleased+"\n### Removed\n\n#### Tools\n\n- `internal/gone`.\n"),
		}, nil},
		{"a decision and a link under Removed", []docsEdit{
			replace(fxChangelog, unreleased, unreleased+"\n### Removed\n\n- D-07, and [the page](docs/gone.md).\n"),
		}, []string{
			"CHANGELOG.md:9: cites D-07, which is not a decision",
			"CHANGELOG.md:9: links to docs/gone.md, which does not exist",
		}},
		{"the heading after Removed ends it", []docsEdit{
			replace(fxChangelog, unreleased, unreleased+"\n### Removed\n\n- `internal/gone`.\n\n### Fixed\n\n- `internal/lost` and the live step \"lost\".\n"),
		}, []string{
			"CHANGELOG.md:13: names internal/lost, which does not exist",
			`CHANGELOG.md:13: names the live step "lost"` + undefined,
		}},
		{"a Removed heading outside the changelog", []docsEdit{
			replace(fxRunbook, "Back to [the first steps](#steps).", "### Removed\n\n`internal/gone`."),
		}, []string{"docs/runbook.md:14: names internal/gone, which does not exist"}},
	})
}

// The scan reads what release-notes lifts, through the one CHANGELOG
// reader: a heading in a fence ends no section.
func TestDocsGateScansWhatTheReleaseNoteLifts(t *testing.T) {
	t.Parallel()
	unreleased := "#codemap` says.\n"
	checkDocsCases(t, []docsCase{
		{"a heading in a fence", []docsEdit{
			replace(fxChangelog, unreleased, unreleased+"\n~~~markdown\n## [1.0.0]\n~~~\n\n- As D-07 says.\n"),
		}, []string{"CHANGELOG.md:11: cites D-07, which is not a decision"}},
		{"a heading with a tab", []docsEdit{
			replace(fxChangelog, unreleased, unreleased+"\n##\tLater\n\n- As D-07 says.\n"),
		}, nil},
		// A link footer in any case ends the section, as release-notes cuts it.
		{"a lowercase footer", []docsEdit{
			replace(fxChangelog, unreleased, unreleased+"\n[unreleased]: https://example.com/compare/v1.0.0...HEAD\n- As D-07 says.\n"),
		}, nil},
	})
}

// The codemap is held to the tree: every package directory under
// internal/ and cmd/, nested ones included, is named in a code span in
// the "## Codemap" section, and every directory it names holds Go.
func TestDocsGateHoldsTheCodemapToTheTree(t *testing.T) {
	t.Parallel()
	const redact = "internal/redact/redact.go"
	checkDocsCases(t, []docsCase{
		{"a package named under a subheading of the codemap", []docsEdit{
			add(redact, "package redact\n"),
			replace(fxArch, "drives it live.\n", "drives it live.\n\n### The logs\n\n`internal/redact` keeps the payload out.\n"),
		}, nil},
		{"a package the codemap leaves out", []docsEdit{
			add(redact, "package redact\n"),
		}, []string{`docs/architecture.md:3: "## Codemap" does not name internal/redact`}},
		{"a nested package the codemap leaves out", []docsEdit{
			add("internal/favroapi/favroapitest/server.go", "package favroapitest\n"),
		}, []string{`docs/architecture.md:3: "## Codemap" does not name internal/favroapi/favroapitest`}},
		{"a package named only by a file in it", []docsEdit{
			add(redact, "package redact\n"),
			codemapNames(redact),
		}, []string{`docs/architecture.md:3: "## Codemap" does not name internal/redact`}},
		{"a package named only in a fenced example", []docsEdit{
			add(redact, "package redact\n"),
			replace(fxArch, "drives it live.\n", "drives it live.\n\n```markdown\n- `internal/redact` keeps the payload out.\n```\n"),
		}, []string{`docs/architecture.md:3: "## Codemap" does not name internal/redact`}},
		{"a package named only after the section ends", []docsEdit{
			add(redact, "package redact\n"),
			replace(fxArch, "so a retry waits.", "so a retry waits, logged by `internal/redact`."),
		}, []string{`docs/architecture.md:3: "## Codemap" does not name internal/redact`}},
		{"a directory the codemap names that holds no .go file", []docsEdit{
			add("internal/assets/logo.txt", "a logo\n"),
			codemapNames("internal/assets"),
		}, []string{`docs/architecture.md:7: "## Codemap" names internal/assets, which holds no .go file`}},
		{"no codemap heading", []docsEdit{
			replace(fxArch, "## Codemap", "## Packages"),
		}, []string{
			"CHANGELOG.md:5: names docs/architecture.md#codemap, and docs/architecture.md has no heading with that slug",
			"CLAUDE.md:3: names docs/architecture.md#codemap, and docs/architecture.md has no heading with that slug",
			"docs/runbook.md:5: links to docs/architecture.md#codemap, and docs/architecture.md has no heading with that slug",
			"docs/runbook.md:14: links to docs/architecture.md#codemap, and docs/architecture.md has no heading with that slug",
			`docs/architecture.md: no "## Codemap" heading`,
		}},
	})
}

// A package directory holds a .go file of its own under internal/ or
// cmd/, at any depth, and outside testdata.
func TestDocsPackageDirs(t *testing.T) {
	files := []string{
		"cmd/favro-mcp/main.go",
		"cmd/favro-mcp/main_test.go",
		"internal/doc.go",
		"internal/client/client.go",
		"internal/client/clienttest/server.go",
		"internal/client/testdata/fixture.go",
		"internal/render/testdata/golden/card.txt",
		"internal/assets/logo.txt",
		"scripts/gates/main.go",
		"README.md",
	}
	want := []string{"cmd/favro-mcp", "internal/client", "internal/client/clienttest"}
	if got := docsPackageDirs(files); !slices.Equal(got, want) {
		t.Errorf("docsPackageDirs = %q, want %q", got, want)
	}
}

// These slugs are what GitHub renders for the same headings.
func TestGitHubSlug(t *testing.T) {
	for _, tc := range []struct{ heading, want string }{
		{"Codemap", "codemap"},
		{"D-01 Stdout carries only MCP JSON-RPC frames", "d-01-stdout-carries-only-mcp-json-rpc-frames"},
		{"What a write may reach", "what-a-write-may-reach"},
		{"The `register` function", "the-register-function"},
		{"C# and F#?", "c-and-f"},
		{"snake_case stays", "snake_case-stays"},
		{"Two  spaces", "two--spaces"},
		{"Straße über alles", "straße-über-alles"},
		{"A [link](https://example.com) here", "a-link-here"},
	} {
		if got := docsGitHubSlug(tc.heading); got != tc.want {
			t.Errorf("docsGitHubSlug(%q) = %q, want %q", tc.heading, got, tc.want)
		}
	}
}
