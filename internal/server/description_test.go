package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppendDescription_NonEmpty(t *testing.T) {
	t.Parallel()

	got := appendDescription("# heading\n\nbody", "appended")
	require.Equal(t, "# heading\n\nbody\n\nappended", got)
}

func TestAppendDescription_EmptyOldDropsSeparator(t *testing.T) {
	t.Parallel()

	require.Equal(t, "appended", appendDescription("", "appended"),
		"empty old must NOT add a leading blank line — would render as a stray paragraph")
}

func TestPrependDescription_NonEmpty(t *testing.T) {
	t.Parallel()

	got := prependDescription("# heading\n\nbody", "prepended")
	require.Equal(t, "prepended\n\n# heading\n\nbody", got)
}

func TestPrependDescription_EmptyOldDropsSeparator(t *testing.T) {
	t.Parallel()

	require.Equal(t, "prepended", prependDescription("", "prepended"))
}

// TestReplaceInDescription_LiteralDefaultCount pins the count=1
// default — the LLM-facing safety: a common substring must NOT
// rewrite every occurrence accidentally. Hit count must be 1.
func TestReplaceInDescription_LiteralDefaultCount(t *testing.T) {
	t.Parallel()

	old := "find this and find this and find this"
	got, hits, err := replaceInDescription(old, "find this", "FOUND", 1, false)
	require.NoError(t, err)
	require.Equal(t, "FOUND and find this and find this", got)
	require.Equal(t, 1, hits)
}

func TestReplaceInDescription_LiteralReplaceAll(t *testing.T) {
	t.Parallel()

	old := "find this and find this and find this"
	got, hits, err := replaceInDescription(old, "find this", "FOUND", 0, false)
	require.NoError(t, err)
	require.Equal(t, "FOUND and FOUND and FOUND", got)
	require.Equal(t, 3, hits, "literal path must report actual replacement count")
}

// TestReplaceInDescription_LiteralNoMatchHitsZero pins the
// no-match → hits=0 contract used by the editor tool to refuse
// PUT-ing an unchanged body.
func TestReplaceInDescription_LiteralNoMatchHitsZero(t *testing.T) {
	t.Parallel()

	got, hits, err := replaceInDescription("body without target", "missing", "x", 1, false)
	require.NoError(t, err)
	require.Equal(t, "body without target", got)
	require.Equal(t, 0, hits)
}

func TestReplaceInDescription_RegexBackref(t *testing.T) {
	t.Parallel()

	got, hits, err := replaceInDescription("hello world", `(\w+) (\w+)`, "$2 $1", 0, true)
	require.NoError(t, err)
	require.Equal(t, "world hello", got)
	require.Equal(t, 1, hits)
}

func TestReplaceInDescription_RegexBadPattern(t *testing.T) {
	t.Parallel()

	_, _, err := replaceInDescription("body", `[unclosed`, "x", 0, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "regex compile failed")
}

func TestReplaceInDescription_EmptyFind(t *testing.T) {
	t.Parallel()

	_, _, err := replaceInDescription("body", "", "x", 0, false)
	require.Error(t, err)
}

// TestLiteralReplaceN_BoundedByCount pins literalReplaceN's behavior
// across count bounds — the editor's no-match guard reads the
// returned count.
func TestLiteralReplaceN(t *testing.T) {
	t.Parallel()

	got, n := literalReplaceN("body", "missing", "x", 1)
	require.Equal(t, "body", got)
	require.Equal(t, 0, n)

	got, n = literalReplaceN("foo bar foo", "foo", "FOO", 1)
	require.Equal(t, "FOO bar foo", got)
	require.Equal(t, 1, n)

	got, n = literalReplaceN("foo bar foo", "foo", "FOO", 0)
	require.Equal(t, "FOO bar FOO", got)
	require.Equal(t, 2, n)

	got, n = literalReplaceN("foo bar foo", "foo", "FOO", 5)
	require.Equal(t, "FOO bar FOO", got, "count > total must replace all without erroring")
	require.Equal(t, 2, n)
}

// TestUnifiedDiff_ProducesValidDiffHeader pins the diff format —
// the LLM expects a unified-diff header so a human reviewer can
// read the change without parsing.
func TestUnifiedDiff_ProducesValidDiffHeader(t *testing.T) {
	t.Parallel()

	out := unifiedDiff("ci-1", "alpha\nbeta\n", "alpha\nGAMMA\nbeta\n")
	require.Contains(t, out, "--- ci-1 (before)")
	require.Contains(t, out, "+++ ci-1 (after)")
	require.Contains(t, out, "+GAMMA")
}

// TestDescriptionEditors_GoldenCorpus walks testdata/markdown/ and
// pins that each editor produces a structurally-sound result on the
// canonical markdown shapes (code fences, lists, headings, tables,
// empty body). "Structurally sound" here means: appendDescription
// keeps the original prefix intact byte-for-byte; prependDescription
// keeps the original suffix; replaceInDescription with a known
// token only changes the matching region.
//
// This is a regression net for the kind of subtle edit-bug that
// silently corrupts list / fence boundaries — easier to catch via
// concrete corpus than via abstract unit tests.
func TestDescriptionEditors_GoldenCorpus(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(filepath.Join("testdata", "markdown"))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "corpus must contain at least one .md fixture")

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := e.Name()
		path := filepath.Join("testdata", "markdown", name)
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		body := string(raw)

		t.Run("append/"+name, func(t *testing.T) {
			t.Parallel()
			got := appendDescription(body, "Appended note.")
			if body == "" {
				require.Equal(t, "Appended note.", got)
				return
			}
			require.True(t, strings.HasPrefix(got, body),
				"appendDescription must keep the original body intact as a prefix")
			require.True(t, strings.HasSuffix(got, "Appended note."))
			require.Contains(t, got, body+"\n\nAppended note.",
				"separator must be exactly two newlines")
		})

		t.Run("prepend/"+name, func(t *testing.T) {
			t.Parallel()
			got := prependDescription(body, "Prepended note.")
			if body == "" {
				require.Equal(t, "Prepended note.", got)
				return
			}
			require.True(t, strings.HasSuffix(got, body),
				"prependDescription must keep the original body intact as a suffix")
			require.True(t, strings.HasPrefix(got, "Prepended note."))
		})

		t.Run("replace/"+name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(body, "find this") {
				t.Skip("fixture has no 'find this' token")
			}
			got, _, err := replaceInDescription(body, "find this", "FOUND", 1, false)
			require.NoError(t, err)
			require.Contains(t, got, "FOUND")
			before, after, _ := strings.Cut(body, "find this")
			require.True(t, strings.HasPrefix(got, before),
				"replace must not modify content before the first match")
			require.True(t, strings.HasSuffix(got, after),
				"replace must not modify content after the first match")
		})
	}
}
