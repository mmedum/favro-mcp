package server

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// descriptionSeparator is the default joiner for append / prepend.
// A blank line preserves markdown paragraph boundaries; without it
// the appended text would fuse into the prior paragraph and (for
// list / heading contexts) silently change the rendered structure.
const descriptionSeparator = "\n\n"

// editorResult is the shape every Phase 6 description-editor tool
// returns: the body before, the body after, and a unified diff so an
// LLM can confirm the change is structurally what it intended (or
// surface a surprise to the user). Old / New are full bodies — the
// caller can re-PUT the full New on retry.
type editorResult struct {
	Old         string `json:"old" jsonschema:"the description body before the edit"`
	New         string `json:"new" jsonschema:"the description body after the edit"`
	UnifiedDiff string `json:"unified_diff" jsonschema:"unified-diff representation of the change (RFC-ish three-context-line format)"`
}

// appendDescription returns old + separator + text. When old is empty
// the separator is dropped so a fresh card body doesn't lead with a
// blank line.
func appendDescription(old, text string) string {
	if old == "" {
		return text
	}
	return old + descriptionSeparator + text
}

// prependDescription returns text + separator + old. When old is
// empty the separator is dropped (symmetric with appendDescription).
func prependDescription(old, text string) string {
	if old == "" {
		return text
	}
	return text + descriptionSeparator + old
}

// replaceInDescription returns (new, hits, err) — old with the first
// `count` matches of find replaced by replace. count <= 0 means
// "replace all matches". useRegex compiles find as a Go regexp;
// otherwise find is a literal substring. hits is always the actual
// number of replacements performed (0 means find matched nothing —
// the editor tool surfaces that as a typed error so the LLM doesn't
// accidentally PUT an unchanged body).
//
// On a regex compile failure, returns the body unchanged with a
// non-nil error so the tool can surface the regex syntax error to
// the LLM intact.
func replaceInDescription(old, find, replace string, count int, useRegex bool) (string, int, error) {
	if find == "" {
		return old, 0, fmt.Errorf("find: empty find string")
	}
	if useRegex {
		re, err := regexp.Compile(find)
		if err != nil {
			return old, 0, fmt.Errorf("find: regex compile failed: %w", err)
		}
		out, hits := regexReplaceN(old, re, replace, count)
		return out, hits, nil
	}
	out, hits := literalReplaceN(old, find, replace, count)
	return out, hits, nil
}

// regexReplaceN replaces the first `count` regex matches; count <= 0
// means "replace all". Returns (new, hits) where hits is the actual
// replacement count.
//
// The count-bounded path uses ReplaceAllStringFunc + an inner
// ReplaceAllString(m, replace) so the `replace` template's $N
// backrefs are honored against m's capture groups — a plain string
// replace would output the literal "$1" instead of the captured
// substring.
func regexReplaceN(old string, re *regexp.Regexp, replace string, count int) (string, int) {
	if count <= 0 {
		matches := re.FindAllStringIndex(old, -1)
		return re.ReplaceAllString(old, replace), len(matches)
	}
	n := 0
	out := re.ReplaceAllStringFunc(old, func(m string) string {
		if n >= count {
			return m
		}
		n++
		return re.ReplaceAllString(m, replace)
	})
	return out, n
}

// literalReplaceN wraps strings.Replace and recovers the replacement
// count via strings.Count (strings.Replace itself doesn't return one).
// count <= 0 means "replace all".
func literalReplaceN(old, find, replace string, count int) (string, int) {
	total := strings.Count(old, find)
	n := count
	if n <= 0 || n > total {
		n = total
	}
	if n == 0 {
		return old, 0
	}
	return strings.Replace(old, find, replace, n), n
}

// unifiedDiff renders a 3-context-line unified diff between old and
// new. cardID is interpolated into the From/To file headers so the
// diff reads naturally when the LLM surfaces it to a human reviewer.
func unifiedDiff(cardID, oldBody, newBody string) string {
	d := difflib.UnifiedDiff{
		A:        difflib.SplitLines(oldBody),
		B:        difflib.SplitLines(newBody),
		FromFile: cardID + " (before)",
		ToFile:   cardID + " (after)",
		Context:  3,
	}
	out, err := difflib.GetUnifiedDiffString(d)
	if err != nil {
		// Fall back to a raw delimiter diff so the tool can still
		// return a useful answer; difflib's only error mode is a
		// nil-input case that we don't reach.
		return "--- before\n+++ after\n" + oldBody + "\n>>>\n" + newBody
	}
	return out
}

// makeEditorResult bundles the (old, new, diff) projection every
// Phase 6 editor tool returns, threading the cardID through the diff
// header so the unified diff is human-readable on its own.
func makeEditorResult(cardID, oldBody, newBody string) editorResult {
	return editorResult{
		Old:         oldBody,
		New:         newBody,
		UnifiedDiff: unifiedDiff(cardID, oldBody, newBody),
	}
}
