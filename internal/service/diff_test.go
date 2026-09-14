package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnifiedDiffMatchesGNUDiff is the assertion that matters: the
// output is a unified diff, and "a unified diff" means what `diff -u`
// produces, not what a hand-written renderer happens to emit.
//
// This is why the replacement for go-difflib is checked against the
// tool rather than against a golden file somebody typed. go-difflib
// itself was wrong here in two ways that only a comparison shows: it
// appended a synthetic empty line, which surfaced as a stray context
// line at the end of a hunk, and it dropped the "\ No newline at end
// of file" marker, which is the one piece of information a diff cannot
// recover from the lines alone.
//
// Skipped where diff is not installed, which is the Windows runner.
//
// Every case here has a unique minimal edit script. That is a
// requirement, not a coincidence: where the longest common subsequence
// is ambiguous, greedy Myers and GNU diff pick different arrangements
// of the same number of edits, and both are correct. A case with
// repeated lines added to this table would fail for that reason and
// not for a defect.
func TestUnifiedDiffMatchesGNUDiff(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("no diff(1) on this machine")
	}

	cases := []struct{ name, before, after string }{
		{"one line changed", "alpha\nbeta\n", "alpha\nGAMMA\nbeta\n"},
		{"appended", "one\ntwo\n", "one\ntwo\n\nappended\n"},
		{"prepended", "one\ntwo\n", "prepended\n\none\ntwo\n"},
		{"first line", "a\nb\nc\nd\ne\nf\ng\nh\n", "A\nb\nc\nd\ne\nf\ng\nh\n"},
		{"last line", "a\nb\nc\nd\ne\nf\ng\nh\n", "a\nb\nc\nd\ne\nf\ng\nH\n"},
		{
			"two hunks far apart",
			"a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\n",
			"A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\nP\n",
		},
		{"pure insert", "a\nb\n", "a\nx\ny\nz\nb\n"},
		{"pure delete", "a\nx\ny\nz\nb\n", "a\nb\n"},
		{"block replaced", "a\n1\n2\n3\nb\n", "a\nX\nY\nb\n"},
		{"no trailing newline", "a\nb", "a\nB"},
		{"newline added at end", "a\nb", "a\nb\n"},
		{"emptied", "old body\n", ""},
		{"filled from empty", "", "new body\n"},
		{"unicode", "héllo\nwörld\n", "héllo\nWÖRLD\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			want := gnuDiff(t, tc.before, tc.after)
			got := unifiedDiff("ci-1", tc.before, tc.after)
			// The headers name the card rather than two temp files, so
			// they are compared separately from the body.
			got = strings.TrimPrefix(got, "--- ci-1 (before)\n+++ ci-1 (after)\n")
			if got != want {
				t.Errorf("diff body differs from diff -u\n got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// gnuDiff returns what `diff -u` produces for the two bodies, without
// its file headers.
func gnuDiff(t *testing.T, before, after string) string {
	t.Helper()

	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old")
	newPath := filepath.Join(dir, "new")
	if err := os.WriteFile(oldPath, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(after), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("diff", "-u", oldPath, newPath).Output()
	if err != nil {
		// diff exits 1 when the files differ, which is the normal case.
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 {
			t.Fatalf("diff -u: %v", err)
		}
	}
	lines := strings.SplitAfter(string(out), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			return strings.Join(lines[i:], "")
		}
	}
	return "" // no hunks: the bodies are identical
}

// TestUnifiedDiffHeaderNamesTheCard pins what the header is for: a
// person shown the diff should be able to tell which card it is.
func TestUnifiedDiffHeaderNamesTheCard(t *testing.T) {
	t.Parallel()

	out := unifiedDiff("ci-1", "alpha\nbeta\n", "alpha\nGAMMA\nbeta\n")
	for _, want := range []string{"--- ci-1 (before)", "+++ ci-1 (after)", "+GAMMA"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff does not contain %q:\n%s", want, out)
		}
	}
}

// TestUnifiedDiffIdenticalIsEmpty: nothing changed, nothing to say.
func TestUnifiedDiffIdenticalIsEmpty(t *testing.T) {
	t.Parallel()

	if got := unifiedDiff("ci-1", "same\n", "same\n"); got != "" {
		t.Errorf("unifiedDiff of identical bodies = %q, want empty", got)
	}
}

// TestUnifiedDiffScalesWithChangeNotSize is the bug the cap had when it
// was written: it bounded the search by the size of the bodies rather
// than by how much of them differed.
//
// Three thousand shared lines with one line changed at each end is an
// edit distance of four. Bounded by size, that exceeded the cap and
// fell to the wholesale fallback — a diff that deleted three thousand
// lines and re-inserted them, seventy kilobytes of it, handed to a
// model as `unified_diff`. Bounded by distance, it is two small hunks.
//
// Both ends matter: trimming the common prefix and suffix handles a
// change at one end on its own, so only a change at both ends reaches
// the search at all.
func TestUnifiedDiffScalesWithChangeNotSize(t *testing.T) {
	t.Parallel()

	var shared strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&shared, "shared line %d\n", i)
	}
	before := "FIRST before\n" + shared.String() + "LAST before\n"
	after := "FIRST after\n" + shared.String() + "LAST after\n"

	out := unifiedDiff("ci-1", before, after)

	if lines := strings.Count(out, "\n"); lines > 20 {
		t.Errorf("a four-line edit produced a %d-line diff; the search is bounded by size rather than by edit distance", lines)
	}
	// Counted as header lines: each header carries "@@" twice.
	hunks := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "@@") {
			hunks++
		}
	}
	if hunks != 2 {
		t.Errorf("got %d hunks, want two — one per changed end:\n%s", hunks, out)
	}
	for _, want := range []string{"-FIRST before", "+FIRST after", "-LAST before", "+LAST after"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff does not contain %q:\n%s", want, out)
		}
	}
}

// TestUnifiedDiffBoundsTheSearch covers the other side of the cap: two
// bodies that really do differ by more than maxDiffEdits fall back to a
// wholesale replacement, which is valid and non-minimal, and must still
// show both sides rather than giving up.
func TestUnifiedDiffBoundsTheSearch(t *testing.T) {
	t.Parallel()

	var before, after strings.Builder
	for i := range maxDiffEdits {
		fmt.Fprintf(&before, "old line %d\n", i)
		fmt.Fprintf(&after, "new line %d\n", i)
	}

	out := unifiedDiff("ci-1", before.String(), after.String())
	if !strings.Contains(out, "-old line") || !strings.Contains(out, "+new line") {
		t.Error("a capped diff must still show both sides")
	}
}

func TestSplitLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a\n", []string{"a\n"}},
		{"a\nb", []string{"a\n", "b"}},
		{"a\nb\n", []string{"a\n", "b\n"}},
		{"\n", []string{"\n"}},
	}
	for _, tc := range cases {
		got := splitLines(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitLines(%q) = %q, want %q", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitLines(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
