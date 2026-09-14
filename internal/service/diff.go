package service

import (
	"fmt"
	"strings"
)

// Unified-diff generation, replacing github.com/pmezard/go-difflib.
//
// The output is what a description-editor tool hands back as
// `unified_diff`, so it is read by a model and, through it, by a
// person. It is a real unified diff — the same shape `diff -u` emits —
// and nothing else here depends on its exact bytes.

// diffContext is how many unchanged lines surround each change.
// Three is what `diff -u` defaults to and what a reader expects.
const diffContext = 3

// maxDiffEdits bounds the search by edit distance — the D in Myers'
// O((N+M)·D) — and deliberately not by the size of the inputs.
//
// The distinction is the whole point. Two thousand-line bodies that
// differ in one line at the top and one at the bottom have an edit
// distance of four, and bounding by size would send them to the
// wholesale fallback: a diff that deletes every line and re-inserts it,
// which is valid, useless, and what a caller would actually receive.
// Bounding by distance means the cost tracks how much changed rather
// than how much there is.
//
// A thousand is generous for a card description and bounds the memory:
// the trace keeps one row per round, so the worst case this admits is
// about 16 MB and a few tens of milliseconds. Past it, two bodies
// really are unrelated, and saying so beats proving it line by line.
const maxDiffEdits = 1000

// unifiedDiff renders a unified diff between old and new. cardID is
// interpolated into the file headers so the diff reads on its own when
// a person is shown it.
func unifiedDiff(cardID, oldBody, newBody string) string {
	oldLines, newLines := splitLines(oldBody), splitLines(newBody)
	header := "--- " + cardID + " (before)\n+++ " + cardID + " (after)\n"

	ops := diffOps(oldLines, newLines)
	hunks := groupHunks(ops, diffContext)
	if len(hunks) == 0 {
		return "" // identical; difflib said nothing here too
	}

	var b strings.Builder
	b.WriteString(header)
	for _, h := range hunks {
		writeHunk(&b, h, oldLines, newLines)
	}
	return b.String()
}

// splitLines splits into lines that keep their newline. A trailing
// newline does not produce a final empty line, which is where
// go-difflib differed: it appended one and then printed it as a stray
// context line at the end of the last hunk.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// opKind is what happened to one line.
type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

// op is one line of the diff: which side it came from and its index
// into that side.
type op struct {
	kind     opKind
	oldIndex int // valid for opEqual and opDelete
	newIndex int // valid for opEqual and opInsert
}

// diffOps returns the line-by-line edit script.
//
// The common prefix and suffix are stripped first. That is not just an
// optimisation: append and prepend — two of the three tools that call
// this — differ from their input only at one end, so trimming reduces
// the search to almost nothing and makes the common case exact.
func diffOps(oldLines, newLines []string) []op {
	var ops []op

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		ops = append(ops, op{opEqual, prefix, prefix})
		prefix++
	}

	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	midOld := oldLines[prefix : len(oldLines)-suffix]
	midNew := newLines[prefix : len(newLines)-suffix]

	for _, o := range myers(midOld, midNew) {
		o.oldIndex += prefix
		o.newIndex += prefix
		ops = append(ops, o)
	}

	for i := 0; i < suffix; i++ {
		ops = append(ops, op{opEqual, len(oldLines) - suffix + i, len(newLines) - suffix + i})
	}
	return ops
}

// myers runs the greedy edit-script search, recording the furthest
// reaching path at each edit distance so the script can be walked back
// once the end is reached.
func myers(a, b []string) []op {
	n, m := len(a), len(b)
	switch {
	case n == 0 && m == 0:
		return nil
	case n == 0:
		return allOf(opInsert, m)
	case m == 0:
		return allOf(opDelete, n)
	}

	// The search can need at most n+m edits, so there is no point
	// looking further than that even when the cap would allow it.
	limit := min(n+m, maxDiffEdits)

	// v[k] is the furthest x reached on diagonal k; trace keeps a copy
	// per round so the path can be recovered. Sizing both by limit
	// rather than by n+m is what bounds the memory: |k| never exceeds
	// the round number, and the round number never exceeds limit.
	offset := limit
	v := make([]int, 2*limit+1)
	var trace [][]int

	for d := 0; d <= limit; d++ {
		trace = append(trace, append([]int(nil), v...))
		if advance(v, offset, d, a, b) {
			return backtrack(trace, a, b, offset)
		}
	}

	// Reached only when the two sides differ by more than maxDiffEdits.
	return append(allOf(opDelete, n), allOf(opInsert, m)...)
}

// advance extends every diagonal by one edit, and reports whether the
// end of both inputs has been reached. v is updated in place: v[k] is
// the furthest x reached on diagonal k.
func advance(v []int, offset, d int, a, b []string) bool {
	n, m := len(a), len(b)
	for k := -d; k <= d; k += 2 {
		var x int
		if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
			x = v[offset+k+1] // came down: an insertion
		} else {
			x = v[offset+k-1] + 1 // came right: a deletion
		}
		y := x - k
		// Slide along the diagonal for as long as the lines match;
		// this is the "snake" that makes the algorithm cheap when the
		// two sides mostly agree, which is the usual case here.
		for x < n && y < m && a[x] == b[y] {
			x++
			y++
		}
		v[offset+k] = x
		if x >= n && y >= m {
			return true
		}
	}
	return false
}

// backtrack walks the recorded traces from the end to the start,
// producing the edit script in forward order.
func backtrack(trace [][]int, a, b []string, offset int) []op {
	x, y := len(a), len(b)
	var rev []op

	for d := len(trace) - 1; d > 0; d-- {
		prevX, prevY := previous(trace[d], offset, d, x, y)

		// Walk back down the snake first: those lines matched.
		for x > prevX && y > prevY {
			x--
			y--
			rev = append(rev, op{opEqual, x, y})
		}
		switch {
		case x > prevX:
			x--
			rev = append(rev, op{opDelete, x, y})
		case y > prevY:
			y--
			rev = append(rev, op{opInsert, x, y})
		}
	}
	for x > 0 && y > 0 {
		x--
		y--
		rev = append(rev, op{opEqual, x, y})
	}

	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// previous returns the point this one was reached from, by asking the
// same question advance asked on the way out.
func previous(v []int, offset, d, x, y int) (int, int) {
	k := x - y
	prevK := k - 1
	if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
		prevK = k + 1
	}
	prevX := v[offset+prevK]
	return prevX, prevX - prevK
}

// allOf builds a run of one kind, for the degenerate sides.
func allOf(kind opKind, n int) []op {
	ops := make([]op, n)
	for i := range ops {
		ops[i] = op{kind: kind, oldIndex: i, newIndex: i}
	}
	return ops
}

// hunk is a contiguous run of ops with its context already included.
type hunk struct {
	ops                []op
	oldStart, oldCount int
	newStart, newCount int
}

// groupHunks keeps the changed ops plus context lines around them,
// merging two changes whose contexts touch into one hunk — which is
// what makes a diff readable rather than a list of one-line windows.
func groupHunks(ops []op, context int) []hunk {
	var changed []int
	for i, o := range ops {
		if o.kind != opEqual {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return nil
	}

	var hunks []hunk
	start, end := changed[0], changed[0]
	flush := func() {
		lo := start - context
		if lo < 0 {
			lo = 0
		}
		hi := end + context
		if hi > len(ops)-1 {
			hi = len(ops) - 1
		}
		hunks = append(hunks, newHunk(ops[lo:hi+1]))
	}
	for _, i := range changed[1:] {
		if i-end > 2*context+1 {
			flush()
			start = i
		}
		end = i
	}
	flush()
	return hunks
}

// newHunk computes the @@ line numbers for a span of ops.
func newHunk(ops []op) hunk {
	h := hunk{ops: ops}
	h.oldStart, h.newStart = -1, -1
	for _, o := range ops {
		if o.kind != opInsert {
			if h.oldStart < 0 {
				h.oldStart = o.oldIndex
			}
			h.oldCount++
		}
		if o.kind != opDelete {
			if h.newStart < 0 {
				h.newStart = o.newIndex
			}
			h.newCount++
		}
	}
	if h.oldStart < 0 {
		h.oldStart = 0
	}
	if h.newStart < 0 {
		h.newStart = 0
	}
	return h
}

// writeHunk renders one hunk, including the "\ No newline at end of
// file" marker that tells a reader the body does not end in a newline —
// the one piece of information a diff loses otherwise.
func writeHunk(b *strings.Builder, h hunk, oldLines, newLines []string) {
	fmt.Fprintf(b, "@@ -%s +%s @@\n", span(h.oldStart, h.oldCount), span(h.newStart, h.newCount))
	for _, o := range h.ops {
		switch o.kind {
		case opEqual:
			writeLine(b, " ", oldLines[o.oldIndex])
		case opDelete:
			writeLine(b, "-", oldLines[o.oldIndex])
		case opInsert:
			writeLine(b, "+", newLines[o.newIndex])
		}
	}
}

// span renders a hunk range the way unified diff does: a count of one
// is written as the line number alone.
func span(start, count int) string {
	if count == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if count == 1 {
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, count)
}

func writeLine(b *strings.Builder, prefix, line string) {
	b.WriteString(prefix)
	b.WriteString(line)
	if !strings.HasSuffix(line, "\n") {
		b.WriteString("\n\\ No newline at end of file\n")
	}
}
