package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// dumpOf builds a schema document with the given tools, so a test can
// state a surface rather than a JSON literal.
func dumpOf(t *testing.T, tools ...schemaTool) []byte {
	t.Helper()
	b, err := json.Marshal(struct {
		Version string       `json:"version"`
		Tools   []schemaTool `json:"tools"`
	}{"test", tools})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tool(name string, required []string, fields ...string) schemaTool {
	var s schemaTool
	s.Name = name
	s.InputSchema.Required = required
	s.InputSchema.Properties = map[string]json.RawMessage{}
	for _, f := range fields {
		s.InputSchema.Properties[f] = json.RawMessage(`{"type":"string"}`)
	}
	return s
}

// diff decides what counts as a breaking change, which is the whole of
// this repository's semver promise. Each rule is watched failing.
func TestDiffNamesEveryBreakingChange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		old, fresh []schemaTool
		breaking   bool
		want       string
	}{
		{
			name:  "nothing changed",
			old:   []schemaTool{tool("favro_get_card", []string{"card_id"}, "card_id")},
			fresh: []schemaTool{tool("favro_get_card", []string{"card_id"}, "card_id")},
			want:  "breaking changes: none",
		},
		{
			name:  "a tool added",
			old:   []schemaTool{tool("favro_get_card", nil, "card_id")},
			fresh: []schemaTool{tool("favro_get_card", nil, "card_id"), tool("favro_list_webhooks", nil)},
			want:  "added tools: [favro_list_webhooks]",
		},
		{
			name:     "a tool removed",
			old:      []schemaTool{tool("favro_get_card", nil, "card_id"), tool("favro_get_tag", nil)},
			fresh:    []schemaTool{tool("favro_get_card", nil, "card_id")},
			breaking: true,
			want:     "tool removed: favro_get_tag",
		},
		{
			name:     "a field removed",
			old:      []schemaTool{tool("favro_get_card", nil, "card_id", "sequential_id")},
			fresh:    []schemaTool{tool("favro_get_card", nil, "card_id")},
			breaking: true,
			want:     "field removed sequential_id",
		},
		{
			// The one that is easy to miss: nothing disappeared, but
			// every existing caller that omitted the field now fails.
			name:     "a field became required",
			old:      []schemaTool{tool("favro_list_cards", nil, "widget_common_id")},
			fresh:    []schemaTool{tool("favro_list_cards", []string{"widget_common_id"}, "widget_common_id")},
			breaking: true,
			want:     "new required field widget_common_id",
		},
		{
			name:  "a field added, optional",
			old:   []schemaTool{tool("favro_list_cards", nil, "widget_common_id")},
			fresh: []schemaTool{tool("favro_list_cards", nil, "widget_common_id", "unique")},
			want:  "breaking changes: none",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldDump, err := parseSchemas(dumpOf(t, tc.old...))
			if err != nil {
				t.Fatal(err)
			}
			freshDump, err := parseSchemas(dumpOf(t, tc.fresh...))
			if err != nil {
				t.Fatal(err)
			}
			var out sink
			if got := diff(&out, oldDump, freshDump); got != tc.breaking {
				t.Errorf("diff() breaking = %v, want %v (%s)", got, tc.breaking, out.String())
			}
			out.mustSay(t, tc.want)
		})
	}
}

// The committed dump is compared by surface, not by bytes: it also
// carries the version this binary was stamped with, which differs
// between a maintainer's build and CI's.
func TestSameSurfaceIgnoresTheVersionStamp(t *testing.T) {
	tools := []schemaTool{tool("favro_ping", nil)}
	committed := []byte(`{"version":"v1.1.2 (abc1234)","sdk":"v1.7.0","tools":` + string(mustTools(t, tools)) + `}`)
	current := []byte(`{"version":"dev (unknown)","sdk":"v1.7.0","tools":` + string(mustTools(t, tools)) + `}`)

	same, err := sameSurface(committed, current)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Error("two dumps of the same surface differ only by the version stamp; this gate would be " +
			"red on every commit for a reason that has nothing to do with the tool surface")
	}

	changed := []byte(`{"version":"dev (unknown)","tools":` +
		string(mustTools(t, []schemaTool{tool("favro_ping", nil, "verbose")})) + `}`)
	same, err = sameSurface(committed, changed)
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Error("a new field is a surface change and must be reported")
	}
}

func TestSameSurfaceRefusesADocumentThatIsNotOne(t *testing.T) {
	good := dumpOf(t, tool("favro_ping", nil))
	if _, err := sameSurface([]byte(`{"tools":[]}`), good); err == nil {
		t.Error("an empty committed dump should name itself rather than compare equal to nothing")
	} else if !strings.Contains(err.Error(), "make schemas") {
		t.Errorf("the failure should say how to fix it, got %q", err)
	}
}

func mustTools(t *testing.T, tools []schemaTool) []byte {
	t.Helper()
	b, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
