package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// plain is a released surface: two tools and a resource template.
const plain = `{"server":"favro-mcp","sdk":"v1.8.0","tools":[
	{"name":"favro_update_card","annotations":{"destructiveHint":false},
	 "inputSchema":{"type":"object","properties":{"card_id":{"type":"string"}}}},
	{"name":"favro_get_card","annotations":{"readOnlyHint":true},
	 "inputSchema":{"type":"object","properties":{"card_id":{"type":"string"}}}}],
	"resourceTemplates":[{"uriTemplate":"favro://cards/{id}","name":"card"}]}`

// flipped is plain with favro_update_card made destructive, and renamed
// is plain with favro_update_card's card_id input renamed, which breaks
// it.
var (
	flipped = strings.Replace(plain, `"destructiveHint":false`, `"destructiveHint":true`, 1)
	renamed = strings.Replace(plain, `"properties":{"card_id"`, `"properties":{"card_ids"`, 1)
)

// renameBreak is the break renamed makes of plain.
var renameBreak = []change{{"favro_update_card", "favro_update_card: input field card_id removed", "input card_id"}}

// surfaceOf reads a dump the tests write out.
func surfaceOf(t *testing.T, dump string) toolSurface {
	t.Helper()
	s, err := readToolSurface([]byte(dump))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// schema-ack fails a pull request unless its commits name every changed
// item in a footer, and every break of a release in a BREAKING CHANGE:
// footer.
func TestSchemaAckNeedsEveryItemNamed(t *testing.T) {
	relabeled := strings.Replace(plain, `"name":"card"`, `"name":"a card"`, 1)
	withoutTemplate := strings.Replace(plain, `{"uriTemplate":"favro://cards/{id}","name":"card"}`, ``, 1)
	reworded := strings.Replace(plain, `"sdk":"v1.8.0",`, `"sdk":"v1.8.0","instructions":"new",`, 1)
	const (
		wantUnnamed      = "no footer names favro_update_card"
		wantUnnamedBreak = "no BREAKING CHANGE: footer names favro_update_card"
	)
	for _, tc := range []struct {
		name, built, message, wantErr string
	}{
		{"an unchanged surface needs nothing", plain, "Tidy the README", ""},
		{"a change needs a footer", flipped, "Mark favro_update_card destructive", wantUnnamed},
		{"a footer naming the tool acknowledges it", flipped, "Mark it\n\nSCHEMA-CHANGE: favro_update_card: destructiveHint true", ""},
		{"a footer naming another tool does not", flipped, "Mark it\n\nSCHEMA-CHANGE: favro_get_card: destructiveHint true", wantUnnamed},
		{
			"a name in the description names nothing", flipped,
			"Mark it\n\nSCHEMA-CHANGE: favro_get_card: as favro_update_card is now", wantUnnamed,
		},
		{"a footer with no description names its list", flipped, "Mark it\n\nSCHEMA-CHANGE: favro_update_card", ""},
		{"a name is a whole token", flipped, "Mark it\n\nSCHEMA-CHANGE: favro_update_cards: destructiveHint true", wantUnnamed},
		{"a continuation line can name it", flipped, "Mark them\n\nSCHEMA-CHANGE: favro_get_card,\n  favro_update_card: destructiveHint true", ""},
		{"an empty footer says nothing", flipped, "Mark it\n\nSCHEMA-CHANGE:", wantUnnamed},
		{"the next line is not the footer's text", flipped, "Mark it\n\nSCHEMA-CHANGE:\nCo-Authored-By: favro_update_card", wantUnnamed},
		{"a footer must open its line", flipped, "Mark it, see SCHEMA-CHANGE: favro_update_card", wantUnnamed},
		{"a line of prose is not a footer", flipped, "Mark it\n\nThe gate wants a\nSCHEMA-CHANGE: favro_update_card footer, which this is not.", wantUnnamed},
		{"a subject is not a footer", flipped, "SCHEMA-CHANGE: favro_update_card", wantUnnamed},
		{"an indented block is not a footer block", flipped, "Mark it\n\nSCHEMA-CHANGE: favro_update_card: hints\n\n    go run ./scripts/gates schema-ack", wantUnnamed},
		{"a footer above the co-author trailer", flipped, "Mark it\n\nSCHEMA-CHANGE: favro_update_card: hints\n\nCo-Authored-By: someone <x@example.com>", ""},
		{"a footer above three blank lines", flipped, "Mark it\n\nSCHEMA-CHANGE: favro_update_card: hints\n\n\n\nCo-Authored-By: someone <x@example.com>", ""},
		{"a footer beside the co-author trailer", flipped, "Mark it\n\nCo-Authored-By: someone <x@example.com>\nSCHEMA-CHANGE: favro_update_card: hints", ""},
		{"a resource template by its URI", relabeled, "Rename it\n\nSCHEMA-CHANGE: favro://cards/{id}: renamed", ""},
		{"a resource template unnamed", relabeled, "Rename it\n\nSCHEMA-CHANGE: favro_update_card: renamed", "no footer names favro://cards/{id}"},
		{"an instructions change needs a footer", reworded, "Reword them", "no footer names instructions"},
		{"the instructions by their key", reworded, "Reword them\n\nSCHEMA-CHANGE: instructions: reworded", ""},
		{"a break named only as a change", renamed, "Rename the field\n\nSCHEMA-CHANGE: favro_update_card: card_id is card_ids", wantUnnamedBreak},
		{"BREAKING CHANGE: acknowledges a break", renamed, "Rename the field\n\nBREAKING CHANGE: favro_update_card: card_id is card_ids", ""},
		{"BREAKING-CHANGE: is its synonym", renamed, "Rename the field\n\nBREAKING-CHANGE: favro_update_card: card_id is card_ids", ""},
		{"SCHEMA-CHANGE: in any case", flipped, "Mark it\n\nSchema-Change: favro_update_card: destructiveHint true", ""},
		{"BREAKING CHANGE: only in uppercase", renamed, "Rename the field\n\nBreaking-Change: favro_update_card: card_id is card_ids", wantUnnamedBreak},
		{
			"a removed template needs BREAKING CHANGE:", withoutTemplate, "Drop it\n\nSCHEMA-CHANGE: favro://cards/{id}: gone",
			"no BREAKING CHANGE: footer names favro://cards/{id}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := compareSurfaces(surfaceOf(t, plain), surfaceOf(t, tc.built))
			err := checkAck(io.Discard, c, []string{"Tidy the docs", tc.message}, "base")
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("checkAck = %v, want it to pass", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("checkAck = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

// The fix a refusal names is the footer that fixes it: BREAKING CHANGE:
// for an unnamed break, SCHEMA-CHANGE: for the rest, each naming its
// items, and a message that ends with it passes.
func TestARefusalNamesTheFooterThatFixesIt(t *testing.T) {
	both := strings.Replace(renamed, `"name":"card"`, `"name":"a card"`, 1)
	for _, tc := range []struct{ name, built, wantFix string }{
		{"a change", flipped, "SCHEMA-CHANGE: favro_update_card: what changed"},
		{"a break", renamed, "BREAKING CHANGE: favro_update_card: what broke"},
		{"a break and a change", both, "BREAKING CHANGE: favro_update_card: what broke\nSCHEMA-CHANGE: favro://cards/{id}: what changed"},
		{
			"a break and a change to one tool", strings.Replace(renamed, `"destructiveHint":false`, `"destructiveHint":true`, 1),
			"BREAKING CHANGE: favro_update_card: what broke",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := compareSurfaces(surfaceOf(t, plain), surfaceOf(t, tc.built))
			err := checkAck(io.Discard, c, []string{"Change it"}, "base")
			if err == nil {
				t.Fatal("checkAck passed with no footer")
			}
			_, fix, _ := strings.Cut(err.Error(), "\n\n")
			if fix != tc.wantFix {
				t.Errorf("the refusal's fix is %q, want %q", fix, tc.wantFix)
			}
			if err := checkAck(io.Discard, c, []string{"Change it", "Name it\n\n" + fix}, "base"); err != nil {
				t.Errorf("checkAck = %v after the fix it named", err)
			}
		})
	}
}

// A name in backticks is taken whole, so a URI with a comma in it can be
// named; split on its comma, it names nothing.
func TestAFooterCanNameAURIWithACommaInBackticks(t *testing.T) {
	const tmpl = "favro://cards/{id}{?view,page}"
	was := strings.Replace(plain, `"favro://cards/{id}"`, `"`+tmpl+`"`, 1)
	now := strings.Replace(was, `"name":"card"`, `"name":"a card"`, 1)
	c := compareSurfaces(surfaceOf(t, was), surfaceOf(t, now))
	if err := checkAck(io.Discard, c, []string{"Rename\n\nSCHEMA-CHANGE: `" + tmpl + "`: renamed"}, "base"); err != nil {
		t.Errorf("checkAck = %v, want the backticked URI to name the template", err)
	}
	if err := checkAck(io.Discard, c, []string{"Rename\n\nSCHEMA-CHANGE: " + tmpl + ": renamed"}, "base"); err == nil {
		t.Error("checkAck passed with the URI split on its comma, which names nothing")
	}
}

// The fix a refusal prints puts a name the footer would split or trim in
// backticks, so the footer it prints is one that passes.
func TestARefusalQuotesANameTheFooterWouldSplit(t *testing.T) {
	for _, uri := range []string{"favro://cards/{id}{?view,page}", "favro://cards/{id}."} {
		t.Run(uri, func(t *testing.T) {
			was := strings.Replace(plain, `"favro://cards/{id}"`, `"`+uri+`"`, 1)
			c := compareSurfaces(surfaceOf(t, was), surfaceOf(t, strings.Replace(was, `"name":"card"`, `"name":"a card"`, 1)))
			err := checkAck(io.Discard, c, []string{"Rename it"}, "base")
			_, fix, _ := strings.Cut(fmt.Sprint(err), "\n\n")
			if want := "SCHEMA-CHANGE: `" + uri + "`: what changed"; fix != want {
				t.Fatalf("the refusal's fix is %q, want %q", fix, want)
			}
			if err := checkAck(io.Discard, c, []string{"Rename it", "Name it\n\n" + fix}, "base"); err != nil {
				t.Errorf("checkAck = %v after the fix it named", err)
			}
		})
	}
}

// A break of the released surface needs BREAKING CHANGE:; removing a tool
// added since the release breaks no caller, so it is a plain change, and
// the report lists it as one.
func TestABreakIsABreakOfARelease(t *testing.T) {
	release := surfaceOf(t, plain)
	base := surfaceOf(t, strings.Replace(plain, `"tools":[`, `"tools":[{"name":"draft_tool"},`, 1))
	head := surfaceOf(t, renamed)
	c := releaseBreaksOnly(compareSurfaces(base, head), compareSurfaces(release, head).breaking)
	if !slices.Equal(c.breaking, renameBreak) {
		t.Errorf("breaking = %v, want %v", c.breaking, renameBreak)
	}
	var report strings.Builder
	footer := "Drop and rename\n\nSCHEMA-CHANGE: draft_tool: never released\nBREAKING CHANGE: favro_update_card: card_id is card_ids"
	if err := checkAck(&report, c, []string{footer}, "base"); err != nil {
		t.Errorf("checkAck = %v, want both acknowledged", err)
	}
	want := "against the merge-base base: 0 added, 2 changed, 1 breaking\n" +
		"  ~ favro_update_card (inputSchema.properties)\n" +
		"  ~ draft_tool removed\n" +
		"  ! favro_update_card: input field card_id removed  BREAKING\n" +
		"the change since base is acknowledged\n"
	if report.String() != want {
		t.Errorf("the report is\n%s\nwant\n%s", report.String(), want)
	}
}

// A tool the base branch already broke since the release, in one field,
// does not make a pull request's break of another field, one no release
// shipped, a break of the release.
func TestABreakOfTheReleaseIsMatchedByItsField(t *testing.T) {
	withInputs := func(props string) toolSurface {
		return surfaceOf(t, strings.Replace(plain, `"properties":{"card_id":{"type":"string"}}`, `"properties":{`+props+`}`, 1))
	}
	release := withInputs(`"card_id":{"type":"string"},"note":{"type":"string"}`)
	base := withInputs(`"card_id":{"type":"string"},"draft":{"type":"string"}`)
	head := withInputs(`"card_id":{"type":"string"}`)
	c := releaseBreaksOnly(compareSurfaces(base, head), compareSurfaces(release, head).breaking)
	if len(c.breaking) != 0 {
		t.Errorf("breaking = %v, want draft, which no release shipped, as a change", c.breaking)
	}
}

// The release is the schemas.json of the newest tag the base branch
// reaches: not the working tree's, not a later commit's, and not an
// older tag a branch cut before the last release reaches. Before the
// first tag there is none, and nothing breaks one.
func TestReleasedBreaksReadTheBaseBranchTag(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	gitIn(t, "init", "-q", "-b", "main")
	gitIn(t, "commit", "--quiet", "--allow-empty", "-m", "Start")
	gitIn(t, "branch", "before-any-release")
	head := surfaceOf(t, renamed)

	breaks, err := releasedBreaks("HEAD", head)
	if err != nil || len(breaks) != 0 {
		t.Fatalf("releasedBreaks with no tag = %v, %v; want none", breaks, err)
	}

	commit := func(body, message string) {
		t.Helper()
		put(t, dir, "schemas.json", body)
		gitIn(t, "add", "schemas.json")
		gitIn(t, "commit", "--quiet", "-m", message)
	}
	commit(plain, "Release 1")
	gitIn(t, "tag", "v1.0.0")
	gitIn(t, "branch", "feature")
	// The next release renamed the input already, so it breaks nothing.
	commit(renamed, "Release 2")
	gitIn(t, "tag", "v2.0.0")
	gitIn(t, "switch", "--quiet", "feature")
	// A later commit and the working tree are not the release.
	commit(`{"tools":[{"name":"other"}]}`, "After")
	put(t, dir, "schemas.json", `{"tools":[{"name":"another"}]}`)

	if breaks, err := releasedBreaks("main", head); err != nil || len(breaks) != 0 {
		t.Errorf("releasedBreaks against main = %v, %v; want none, as v2.0.0 shipped the rename", breaks, err)
	}
	if breaks, err := releasedBreaks("feature", head); err != nil || !slices.Equal(breaks, renameBreak) {
		t.Errorf("releasedBreaks against feature = %v, %v; want %v, from v1.0.0", breaks, err, renameBreak)
	}
	// A branch whose history reaches no tag has no release, although the
	// repository has tags.
	if breaks, err := releasedBreaks("before-any-release", head); err != nil || len(breaks) != 0 {
		t.Errorf("releasedBreaks against a branch that reaches no tag = %v, %v; want none", breaks, err)
	}
}

// A gate that could not run says so and exits 2; a refusal exits 1. The
// message is what tells them apart in CI, where `go run` exits 1 for
// both.
func TestSchemaAckTellsARefusalFromAFailure(t *testing.T) {
	refused := checkAck(io.Discard, compareSurfaces(surfaceOf(t, plain), surfaceOf(t, flipped)), nil, "base")
	if refused == nil || exitCode(refused) != 1 || errors.As(refused, new(couldNotRun)) {
		t.Errorf("a refusal (%v) exits %d, want 1", refused, exitCode(refused))
	}
	t.Chdir(t.TempDir())
	gitIn(t, "init", "-q")
	gitIn(t, "commit", "--quiet", "--allow-empty", "-m", "Start")
	// git's own words, which a locale would translate.
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANGUAGE", "")
	err := schemaAck(io.Discard, []string{"no-such-ref", "HEAD"})
	const want = "could not run: git merge-base no-such-ref HEAD: exit status 128: fatal: Not a valid object name no-such-ref"
	if exitCode(err) != 2 || err == nil || err.Error() != want {
		t.Errorf("schemaAck with no base = %v, exit %d; want exit 2 and %q", err, exitCode(err), want)
	}
	if err := schemaAck(io.Discard, []string{"HEAD"}); err == nil || err.Error() != "usage: gates schema-ack BASE HEAD" {
		t.Errorf("schemaAck with one argument = %v, want its usage", err)
	}
}

// gitIn runs git in the working directory as a fixture identity,
// reading none of the machine's configuration: no signing key, no
// hooks, no default branch of its own.
func gitIn(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.com",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
