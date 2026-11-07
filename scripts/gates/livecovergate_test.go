package main

import (
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/livecover"
)

// TestLiveCoverAgainstThisRepository is the gate against the surface it
// guards, which is the run that matters.
func TestLiveCoverAgainstThisRepository(t *testing.T) {
	t.Chdir(parityRepoRoot)
	var out sink
	if err := liveCover(&out, nil); err != nil {
		t.Fatalf("the live driver does not cover the surface: %v", err)
	}
	out.mustSay(t, "steps over")
	out.mustSay(t, "waived with a reason")
	out.mustSay(t, "each with dry_run: true")
}

// The steps run against objects the organization already holds, so a
// step that writes and does not carry dry_run: true writes to one. The
// message names the step by its Name.
func TestStepsWithoutDryRunNamesEveryWriteThatLacksIt(t *testing.T) {
	readOnly := map[string]bool{"favro_get_card": true, "favro_delete_card": false}
	steps := []livecover.Step{
		{Name: "a read needs none", Tool: "favro_get_card", Args: map[string]any{"card_id": livecover.AnyCardID}},
		{Name: "carries it", Tool: "favro_delete_card", Args: map[string]any{"confirm": true, "dry_run": true}},
		{Name: "omits it", Tool: "favro_delete_card", Args: map[string]any{"confirm": true}},
		{Name: "turns it off", Tool: "favro_delete_card", Args: map[string]any{"confirm": true, "dry_run": false}},
		{Name: "sends a string", Tool: "favro_delete_card", Args: map[string]any{"confirm": true, "dry_run": "true"}},
		{Name: "an unknown tool writes", Tool: "favro_not_in_the_schema", Args: map[string]any{}},
	}

	problems, writes := stepsWithoutDryRun(steps, readOnly)
	if writes != 5 {
		t.Errorf("counted %d steps that write, want 5", writes)
	}
	want := []string{
		`a favro_delete_card step ("omits it") writes without dry_run: true, against an object the run did not create`,
		`a favro_delete_card step ("turns it off") writes without dry_run: true, against an object the run did not create`,
		`a favro_delete_card step ("sends a string") writes without dry_run: true, against an object the run did not create`,
		`a favro_not_in_the_schema step ("an unknown tool writes") writes without dry_run: true, against an object the run did not create`,
	}
	if strings.Join(problems, "\n") != strings.Join(want, "\n") {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(problems, "\n"), strings.Join(want, "\n"))
	}
}

// TestTranscriptAgainstThisRepository likewise.
func TestTranscriptAgainstThisRepository(t *testing.T) {
	t.Chdir(parityRepoRoot)
	var out sink
	if err := transcript(&out, nil); err != nil {
		t.Fatalf("a live driver prints without redacting: %v", err)
	}
	out.mustSay(t, "allowed to have one")
	// Both drivers, by name. The gate read one hard-coded directory
	// while a second driver printed live ids beside it, so "it passed"
	// and "it looked at the driver that was already clean" printed the
	// same sentence.
	for _, d := range transcriptDrivers {
		out.mustSay(t, d.dir)
	}
}

// TestTranscriptSkipsOnlyTheDisabledStub holds the one exemption to its
// shape: a file the driver's build never compiles. If that ever matched a
// real driver file, the gate would read less and still say ok.
func TestTranscriptSkipsOnlyTheDisabledStub(t *testing.T) {
	t.Chdir(parityRepoRoot)
	files, err := docsFiles(".")
	if err != nil {
		t.Fatal(err)
	}
	mk, err := readMakefile()
	if err != nil {
		t.Fatal(err)
	}
	var skipped []string
	for _, d := range transcriptDrivers {
		tags, err := transcriptTags(mk, d)
		if err != nil {
			t.Fatal(err)
		}
		_, s, problems := transcriptFiles(token.NewFileSet(), files, d.dir, tags)
		if len(problems) > 0 {
			t.Fatal(problems)
		}
		skipped = append(skipped, s...)
	}
	if want := []string{"scripts/evals/disabled.go"}; !slices.Equal(skipped, want) {
		t.Errorf("the gate skips %v, want exactly %v — a driver file must not be able to opt out by carrying a build tag", skipped, want)
	}
}

// The gate reads every file of a driver that its build compiles, as git
// lists the checkout: one in a package under it, and one an OS tag builds.
// A file another tag or the driver's own negated tag keeps out is not the
// driver. The tags are the ones the driver's Makefile target runs it with.
func TestTranscriptReadsEveryDriverFile(t *testing.T) {
	const makefile = "GO ?= go\n\nlive:\n\t@$(GO) run ./scripts/livefavro\n\nevals:\n\t$(GO) run -tags=evals ./scripts/evals\n"
	const allowed = "package main\n\nimport \"os\"\n\nfunc newPrinter() { _ = os.Stdout }\n\nfunc fatal() { _ = os.Stderr }\n\n" +
		"func start() { _, _ = os.Stdout, os.Stderr }\n"
	loud := func(build, pkg string) string {
		return build + "package " + pkg + "\n\nimport \"fmt\"\n\nfunc loud() { fmt.Println(\"a card name\") }\n"
	}
	const redact = "; print through the Redactor instead (printer.fail, printer.line, printer.redacted, printer.result)"
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"every file the builds compile", map[string]string{
			"scripts/livefavro/main.go":        allowed,
			"scripts/livefavro/signal.go":      loud("//go:build !windows\n\n", "main"),
			"scripts/livefavro/probe/probe.go": loud("", "probe"),
			"scripts/evals/run.go":             loud("//go:build evals && linux\n\n", "main"),
			"scripts/evals/disabled.go":        loud("//go:build !evals\n\n", "main"),
			"scripts/evals/live.go":            loud("//go:build evals && live\n\n", "main"),
			"scripts/livefavro/testdata/x.go":  loud("", "x"),
			"scripts/other/main.go":            loud("", "main"),
		}, "a live driver prints without redacting:\n" +
			"  scripts/evals/run.go: loud calls fmt.Println" + redact + "\n" +
			"  scripts/livefavro/probe/probe.go: loud calls fmt.Println" + redact + "\n" +
			"  scripts/livefavro/signal.go: loud calls fmt.Println" + redact},
		// One driver read whole is no excuse for the other read not at all.
		{"a driver none of whose files its build compiles", map[string]string{
			"scripts/livefavro/main.go": allowed,
			"scripts/evals/disabled.go": "//go:build !evals\n\npackage main\n",
		}, "a live driver prints without redacting:\n" +
			"  read no Go file of scripts/evals that its build compiles; the drivers table names a directory or a tag that is gone"},
		// A tag the target adds builds a file the driver did not build before.
		{"the tags the Makefile runs a driver with", map[string]string{
			"Makefile":                  "GO ?= go\nevals:\n\t$(GO) run -tags evals,live ./scripts/evals/\nlive:\n\t$(GO) run ./scripts/livefavro\n",
			"scripts/livefavro/main.go": allowed,
			"scripts/evals/live.go":     loud("//go:build evals && live\n\n", "main"),
		}, "a live driver prints without redacting:\n" +
			"  scripts/evals/live.go: loud calls fmt.Println" + redact},
		{"a target that does not run its driver", map[string]string{
			"Makefile":                  "GO ?= go\nevals:\n\t$(GO) run -tags=evals ./scripts/other\nlive:\n\t$(GO) run ./scripts/livefavro\n",
			"scripts/livefavro/main.go": allowed,
			"scripts/evals/run.go":      loud("//go:build evals\n\n", "main"),
		}, "a live driver prints without redacting:\n" +
			"  Makefile: evals runs no `go run` of scripts/evals, so the gate cannot tell which of its files the driver builds"},
		{"fewer mentions of the terminal than the floor", map[string]string{
			"scripts/livefavro/main.go": "package main\n\nimport \"os\"\n\nfunc newPrinter() { _, _, _ = os.Stdout, os.Stderr, os.Stdout }\n",
			"scripts/evals/run.go":      "//go:build evals\n\npackage main\n",
		}, "a live driver prints without redacting:\n" +
			"  read 3 mentions of os.Stdout and os.Stderr, want at least 4: the reader is not seeing its input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := initRepo(t)
			put(t, dir, "Makefile", makefile)
			putAll(t, dir, tc.files)
			err := transcript(io.Discard, nil)
			if err == nil || err.Error() != tc.want {
				t.Errorf("transcript() = %v\nwant %s", err, tc.want)
			}
		})
	}
}

// TestEveryStepNamesARegisteredTool holds the step list against the
// schema from the other direction: a step for a tool that no longer
// exists is a step that silently never runs, and the driver reports it
// as a skip, which looks like an organization that lacks something.
func TestEveryStepNamesARegisteredTool(t *testing.T) {
	schema, err := toolOptions(filepath.Join(parityRepoRoot, "schemas.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schema) < minCoverTools {
		t.Fatalf("read %d tools, expected at least %d", len(schema), minCoverTools)
	}

	steps := livecover.Steps()
	if len(steps) < minCoverSteps {
		t.Fatalf("read %d steps, expected at least %d", len(steps), minCoverSteps)
	}
	for _, s := range steps {
		options, ok := schema[s.Tool]
		if !ok {
			t.Errorf("a step calls %s, which is not a registered tool", s.Tool)
			continue
		}
		declared := map[string]bool{}
		for _, o := range options {
			declared[o] = true
		}
		for arg := range s.Args {
			if !declared[arg] {
				t.Errorf("a step passes %s.%s, which the tool does not declare — the call would be rejected before it ran",
					s.Tool, arg)
			}
		}
	}
}

// TestEveryPlaceholderIsUsed: a placeholder nothing asks for is one the
// driver resolves for nobody.
func TestEveryPlaceholderIsUsed(t *testing.T) {
	used := map[livecover.Placeholder]bool{}
	var mark func(any)
	mark = func(v any) {
		switch t := v.(type) {
		case livecover.Placeholder:
			used[t] = true
		case []any:
			for _, e := range t {
				mark(e)
			}
		case []map[string]any:
			for _, e := range t {
				for _, val := range e {
					mark(val)
				}
			}
		case map[string]any:
			for _, val := range t {
				mark(val)
			}
		}
	}
	for _, s := range livecover.Steps() {
		for _, v := range s.Args {
			mark(v)
		}
	}
	for _, p := range livecover.Placeholders {
		if !used[p] {
			t.Errorf("placeholder %s is declared and no step uses it", p)
		}
	}
}

// TestMeasureCountsWhatItSays covers the model itself, against a
// surface small enough to check by eye.
func TestMeasureCountsWhatItSays(t *testing.T) {
	schema := map[string][]string{
		"tool_a": {"one", "two"},
		"tool_b": {"three"},
		"tool_c": nil,
	}
	steps := []livecover.Step{
		{Tool: "tool_a", Args: map[string]any{"one": 1}},
	}

	cov := livecover.Measure(steps, schema, nil)
	if cov.Tools != 3 || cov.ToolsCovered != 1 {
		t.Errorf("tools: got %d covered of %d, want 1 of 3", cov.ToolsCovered, cov.Tools)
	}
	if cov.Options != 3 || cov.OptionsCovered != 1 {
		t.Errorf("options: got %d covered of %d, want 1 of 3", cov.OptionsCovered, cov.Options)
	}
	if strings.Join(cov.MissingTools, ",") != "tool_b,tool_c" {
		t.Errorf("MissingTools = %v", cov.MissingTools)
	}
	if strings.Join(cov.MissingOptions, ",") != "tool_a.two,tool_b.three" {
		t.Errorf("MissingOptions = %v", cov.MissingOptions)
	}
	if cov.Shortfall() == "" {
		t.Error("a gap must read as a sentence")
	}

	// A waiver on the tool covers its options; a full set has no
	// shortfall to report.
	cov = livecover.Measure(steps, schema, map[string]string{
		"tool_a.two": "reason", "tool_b": "reason", "tool_c": "reason",
	})
	if cov.Shortfall() != "" {
		t.Errorf("a fully waived surface should report no shortfall: %s", cov.Shortfall())
	}
}

func TestReadLiveWaiversRejectsAMalformedRow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "waived.tsv")
	if err := os.WriteFile(p, []byte("# fine\nonly-one-field\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLiveWaivers(p); err == nil {
		t.Error("a one-field row should not parse")
	}
}
