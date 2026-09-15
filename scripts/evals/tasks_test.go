package main

import (
	"strings"
	"testing"
)

// evalVals are the placeholders a real run substitutes. Listed here so
// the test walks the same set the runner does — a prompt naming a
// placeholder nobody fills is the failure this whole file exists for.
var evalVals = []string{"board", "card", "newCard", "column", "comment", "tag", "topic"}

// Every prompt substitutes cleanly. A sibling's first full eval run
// passed two tasks while sending the agent a literal `{folder}`; the
// guard belongs here, where it costs nothing, rather than in a live run
// that costs ten minutes and an account.
func TestEveryPromptSubstitutes(t *testing.T) {
	vals := map[string]string{}
	for _, k := range evalVals {
		vals[k] = "x-" + k
	}
	if len(Tasks) < 5 {
		t.Fatalf("%d tasks; this test is reading nothing", len(Tasks))
	}
	for _, task := range Tasks {
		out, err := substitute(task.Prompt, vals)
		if err != nil {
			t.Errorf("%s: %v", task.Name, err)
			continue
		}
		if strings.ContainsAny(out, "{}") {
			t.Errorf("%s: braces survived substitution: %q", task.Name, out)
		}
	}
}

// And the reverse: a placeholder nobody uses is dead weight that reads
// as coverage.
func TestEveryPlaceholderIsUsedBySomeTask(t *testing.T) {
	for _, k := range evalVals {
		used := false
		for _, task := range Tasks {
			if strings.Contains(task.Prompt, "{"+k+"}") {
				used = true
				break
			}
		}
		if !used {
			t.Errorf("placeholder %q is substituted and no task uses it", k)
		}
	}
}

// A task nobody can score six months from now is not a task.
func TestEveryTaskIsScorableAndExplained(t *testing.T) {
	seen := map[string]bool{}
	for _, task := range Tasks {
		if seen[task.Name] {
			t.Errorf("duplicate task name %q; results are keyed by it", task.Name)
		}
		seen[task.Name] = true

		if strings.TrimSpace(task.Why) == "" {
			t.Errorf("%s: no Why", task.Name)
		}
		if task.MaxCalls <= 0 {
			t.Errorf("%s: MaxCalls must bound the run", task.Name)
		}
		if task.EndState == nil && task.Trace == nil {
			t.Errorf("%s: scores nothing", task.Name)
		}
		// §13: where the end state cannot be read, the task must SAY
		// which half went unchecked rather than quietly checking one.
		if task.EndState == nil && strings.TrimSpace(task.Unverifiable) == "" {
			t.Errorf("%s: has no EndState and does not say why", task.Name)
		}
	}
}

// substitute refuses what it cannot fill, rather than passing it on.
func TestSubstituteRefusesAnUnfilledPlaceholder(t *testing.T) {
	if _, err := substitute("move {card} to {nowhere}", map[string]string{"card": "a"}); err == nil {
		t.Fatal("an unsubstituted placeholder was accepted")
	}
	out, err := substitute("move {card}", map[string]string{"card": "a"})
	if err != nil || out != "move a" {
		t.Fatalf("substitute = %q, %v; want \"move a\", nil", out, err)
	}
}

// The refusal matcher decides whether a model admitted a tool refused
// it, so it has to recognise the ordinary ways of saying so — and not
// fire on an ordinary success.
func TestMentionsFailure(t *testing.T) {
	for _, s := range []string{
		"That tag does not exist, so I did not add it.",
		"I couldn't add it — no such tag.",
		"Unable to complete: the tag was not found.",
	} {
		if !mentionsFailure(s) {
			t.Errorf("did not read as a reported failure: %q", s)
		}
	}
	for _, s := range []string{"done", "Added the tag to the card.", ""} {
		if mentionsFailure(s) {
			t.Errorf("read as a failure and is not: %q", s)
		}
	}
}

// clip cuts on a rune boundary. This repository has fixed the byte
// version of this twice.
func TestClipCutsOnARuneBoundary(t *testing.T) {
	got := clip(strings.Repeat("é", 40), 21)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("clip did not mark the truncation: %q", got)
	}
	for i, r := range got {
		if r == '�' {
			t.Errorf("clip split a rune at byte %d: %q", i, got)
		}
	}
}
