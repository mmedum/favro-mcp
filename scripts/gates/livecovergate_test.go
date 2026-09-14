package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/livecover"
)

// TestLiveCoverAgainstThisRepository is the gate against the surface it
// guards, which is the run that matters.
func TestLiveCoverAgainstThisRepository(t *testing.T) {
	var out sink
	if err := liveCover(&out, nil); err != nil {
		t.Fatalf("the live driver does not cover the surface: %v", err)
	}
	out.mustSay(t, "steps over")
	out.mustSay(t, "waived with a reason")
}

// TestTranscriptAgainstThisRepository likewise.
func TestTranscriptAgainstThisRepository(t *testing.T) {
	var out sink
	if err := transcript(&out, nil); err != nil {
		t.Fatalf("the live driver prints without redacting: %v", err)
	}
	out.mustSay(t, "allowed to have one")
}

// TestEveryStepNamesARegisteredTool holds the step list against the
// schema from the other direction: a step for a tool that no longer
// exists is a step that silently never runs, and the driver reports it
// as a skip, which looks like an organization that lacks something.
func TestEveryStepNamesARegisteredTool(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	schema, err := toolOptions(filepath.Join(root, "schemas.json"))
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
