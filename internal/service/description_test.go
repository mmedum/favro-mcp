package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendDescription_NonEmpty(t *testing.T) {
	t.Parallel()

	got := AppendDescription("# heading\n\nbody", "appended")
	if got := got; got != "# heading\n\nbody\n\nappended" {
		t.Errorf("got = %v, want %v", got, "# heading\n\nbody\n\nappended")
	}
}

func TestAppendDescription_EmptyOldDropsSeparator(t *testing.T) {
	t.Parallel()

	if got := AppendDescription("", "appended"); got != "appended" {
		t.Errorf("empty old must NOT add a leading blank line — would render as a stray paragraph: got %v, want %v", got, "appended")
	}
}

func TestPrependDescription_NonEmpty(t *testing.T) {
	t.Parallel()

	got := PrependDescription("# heading\n\nbody", "prepended")
	if got := got; got != "prepended\n\n# heading\n\nbody" {
		t.Errorf("got = %v, want %v", got, "prepended\n\n# heading\n\nbody")
	}
}

func TestPrependDescription_EmptyOldDropsSeparator(t *testing.T) {
	t.Parallel()

	if got := PrependDescription("", "prepended"); got != "prepended" {
		t.Errorf("PrependDescription(\"\", \"prepended\") = %v, want %v", got, "prepended")
	}
}

// TestReplaceInDescription_LiteralDefaultCount pins the count=1
// default — the LLM-facing safety: a common substring must NOT
// rewrite every occurrence accidentally. Hit count must be 1.
func TestReplaceInDescription_LiteralDefaultCount(t *testing.T) {
	t.Parallel()

	old := "find this and find this and find this"
	got, hits, err := ReplaceInDescription(old, "find this", "FOUND", 1, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != "FOUND and find this and find this" {
		t.Errorf("got = %v, want %v", got, "FOUND and find this and find this")
	}
	if got := hits; got != 1 {
		t.Errorf("hits = %v, want %v", got, 1)
	}
}

func TestReplaceInDescription_LiteralReplaceAll(t *testing.T) {
	t.Parallel()

	old := "find this and find this and find this"
	got, hits, err := ReplaceInDescription(old, "find this", "FOUND", 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != "FOUND and FOUND and FOUND" {
		t.Errorf("got = %v, want %v", got, "FOUND and FOUND and FOUND")
	}
	if got := hits; got != 3 {
		t.Errorf("literal path must report actual replacement count: got %v, want %v", got, 3)
	}
}

// TestReplaceInDescription_LiteralNoMatchHitsZero pins the
// no-match → hits=0 contract used by the editor tool to refuse
// PUT-ing an unchanged body.
func TestReplaceInDescription_LiteralNoMatchHitsZero(t *testing.T) {
	t.Parallel()

	got, hits, err := ReplaceInDescription("body without target", "missing", "x", 1, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != "body without target" {
		t.Errorf("got = %v, want %v", got, "body without target")
	}
	if got := hits; got != 0 {
		t.Errorf("hits = %v, want %v", got, 0)
	}
}

func TestReplaceInDescription_RegexBackref(t *testing.T) {
	t.Parallel()

	got, hits, err := ReplaceInDescription("hello world", `(\w+) (\w+)`, "$2 $1", 0, true)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got; got != "world hello" {
		t.Errorf("got = %v, want %v", got, "world hello")
	}
	if got := hits; got != 1 {
		t.Errorf("hits = %v, want %v", got, 1)
	}
}

func TestReplaceInDescription_RegexBadPattern(t *testing.T) {
	t.Parallel()

	_, _, err := ReplaceInDescription("body", `[unclosed`, "x", 0, true)
	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(err.Error(), "regex compile failed") {
		t.Errorf("err.Error() does not contain %q", "regex compile failed")
	}
}

func TestReplaceInDescription_EmptyFind(t *testing.T) {
	t.Parallel()

	_, _, err := ReplaceInDescription("body", "", "x", 0, false)
	if err == nil {
		t.Fatal("err should have failed")
	}
}

// TestLiteralReplaceN_BoundedByCount pins literalReplaceN's behavior
// across count bounds — the editor's no-match guard reads the
// returned count.
func TestLiteralReplaceN(t *testing.T) {
	t.Parallel()

	got, n := literalReplaceN("body", "missing", "x", 1)
	if got := got; got != "body" {
		t.Errorf("got = %v, want %v", got, "body")
	}
	if got := n; got != 0 {
		t.Errorf("n = %v, want %v", got, 0)
	}

	got, n = literalReplaceN("foo bar foo", "foo", "FOO", 1)
	if got := got; got != "FOO bar foo" {
		t.Errorf("got = %v, want %v", got, "FOO bar foo")
	}
	if got := n; got != 1 {
		t.Errorf("n = %v, want %v", got, 1)
	}

	got, n = literalReplaceN("foo bar foo", "foo", "FOO", 0)
	if got := got; got != "FOO bar FOO" {
		t.Errorf("got = %v, want %v", got, "FOO bar FOO")
	}
	if got := n; got != 2 {
		t.Errorf("n = %v, want %v", got, 2)
	}

	got, n = literalReplaceN("foo bar foo", "foo", "FOO", 5)
	if got := got; got != "FOO bar FOO" {
		t.Errorf("count > total must replace all without erroring: got %v, want %v", got, "FOO bar FOO")
	}
	if got := n; got != 2 {
		t.Errorf("n = %v, want %v", got, 2)
	}
}

// TestUnifiedDiff_ProducesValidDiffHeader pins the diff format —
// the LLM expects a unified-diff header so a human reviewer can
// read the change without parsing.
func TestUnifiedDiff_ProducesValidDiffHeader(t *testing.T) {
	t.Parallel()

	out := unifiedDiff("ci-1", "alpha\nbeta\n", "alpha\nGAMMA\nbeta\n")
	if !strings.Contains(out, "--- ci-1 (before)") {
		t.Errorf("out does not contain %q", "--- ci-1 (before)")
	}
	if !strings.Contains(out, "+++ ci-1 (after)") {
		t.Errorf("out does not contain %q", "+++ ci-1 (after)")
	}
	if !strings.Contains(out, "+GAMMA") {
		t.Errorf("out does not contain %q", "+GAMMA")
	}
}

// TestDescriptionEditors_GoldenCorpus walks testdata/markdown/ and
// pins that each editor produces a structurally-sound result on the
// canonical markdown shapes (code fences, lists, headings, tables,
// empty body). "Structurally sound" here means: AppendDescription
// keeps the original prefix intact byte-for-byte; PrependDescription
// keeps the original suffix; ReplaceInDescription with a known
// token only changes the matching region.
//
// This is a regression net for the kind of subtle edit-bug that
// silently corrupts list / fence boundaries — easier to catch via
// concrete corpus than via abstract unit tests.
func TestDescriptionEditors_GoldenCorpus(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(filepath.Join("testdata", "markdown"))
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("corpus must contain at least one .md fixture")
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := e.Name()
		path := filepath.Join("testdata", "markdown", name)
		raw, err := os.ReadFile(path)
		if err := err; err != nil {
			t.Fatalf("err: %v", err)
		}
		body := string(raw)

		t.Run("append/"+name, func(t *testing.T) {
			t.Parallel()
			got := AppendDescription(body, "Appended note.")
			if body == "" {
				if got := got; got != "Appended note." {
					t.Errorf("got = %v, want %v", got, "Appended note.")
				}
				return
			}
			if !strings.HasPrefix(got, body) {
				t.Error("AppendDescription must keep the original body intact as a prefix")
			}
			if !strings.HasSuffix(got, "Appended note.") {
				t.Error("strings.HasSuffix(got, \"Appended note.\") = false, want true")
			}
			if !strings.Contains(got, body+"\n\nAppended note.") {
				t.Errorf("separator must be exactly two newlines: %q missing", body+"\n\nAppended note.")
			}
		})

		t.Run("prepend/"+name, func(t *testing.T) {
			t.Parallel()
			got := PrependDescription(body, "Prepended note.")
			if body == "" {
				if got := got; got != "Prepended note." {
					t.Errorf("got = %v, want %v", got, "Prepended note.")
				}
				return
			}
			if !strings.HasSuffix(got, body) {
				t.Error("PrependDescription must keep the original body intact as a suffix")
			}
			if !strings.HasPrefix(got, "Prepended note.") {
				t.Error("strings.HasPrefix(got, \"Prepended note.\") = false, want true")
			}
		})

		t.Run("replace/"+name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(body, "find this") {
				t.Skip("fixture has no 'find this' token")
			}
			got, _, err := ReplaceInDescription(body, "find this", "FOUND", 1, false)
			if err := err; err != nil {
				t.Fatalf("err: %v", err)
			}
			if !strings.Contains(got, "FOUND") {
				t.Errorf("got does not contain %q", "FOUND")
			}
			before, after, _ := strings.Cut(body, "find this")
			if !strings.HasPrefix(got, before) {
				t.Error("replace must not modify content before the first match")
			}
			if !strings.HasSuffix(got, after) {
				t.Error("replace must not modify content after the first match")
			}
		})
	}
}
