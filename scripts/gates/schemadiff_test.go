package main

import (
	"encoding/json"
	"io"
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

// compareSurfaces decides what counts as a breaking change, which is
// the whole of this repository's semver promise: a tool removed, an
// input removed, or an input newly required. Every other change is
// listed. Each rule is watched failing.
func TestCompareSurfacesNamesEveryBreakingChange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		old, fresh []schemaTool
		want       string
	}{
		{
			name:  "nothing changed",
			old:   []schemaTool{tool("favro_get_card", []string{"card_id"}, "card_id")},
			fresh: []schemaTool{tool("favro_get_card", []string{"card_id"}, "card_id")},
			want:  "against the tag: 0 added, 0 changed, 0 breaking\n",
		},
		{
			name:  "a tool added",
			old:   []schemaTool{tool("favro_get_card", nil, "card_id")},
			fresh: []schemaTool{tool("favro_get_card", nil, "card_id"), tool("favro_list_webhooks", nil)},
			want:  "against the tag: 1 added, 0 changed, 0 breaking\n  + favro_list_webhooks\n",
		},
		{
			name:  "a tool removed",
			old:   []schemaTool{tool("favro_get_card", nil, "card_id"), tool("favro_get_tag", nil)},
			fresh: []schemaTool{tool("favro_get_card", nil, "card_id")},
			want:  "against the tag: 0 added, 0 changed, 1 breaking\n  ! favro_get_tag removed  BREAKING\n",
		},
		{
			name:  "a field removed",
			old:   []schemaTool{tool("favro_get_card", nil, "card_id", "sequential_id")},
			fresh: []schemaTool{tool("favro_get_card", nil, "card_id")},
			want: "against the tag: 0 added, 1 changed, 1 breaking\n  ~ favro_get_card (inputSchema.properties)\n" +
				"  ! favro_get_card: input field sequential_id removed  BREAKING\n",
		},
		{
			// The one that is easy to miss: nothing disappeared, but
			// every existing caller that omitted the field now fails.
			name:  "a field became required",
			old:   []schemaTool{tool("favro_list_cards", nil, "widget_common_id")},
			fresh: []schemaTool{tool("favro_list_cards", []string{"widget_common_id"}, "widget_common_id")},
			want: "against the tag: 0 added, 1 changed, 1 breaking\n  ~ favro_list_cards (inputSchema.required)\n" +
				"  ! favro_list_cards: input field widget_common_id newly required  BREAKING\n",
		},
		{
			name:  "a field added, optional",
			old:   []schemaTool{tool("favro_list_cards", nil, "widget_common_id")},
			fresh: []schemaTool{tool("favro_list_cards", nil, "widget_common_id", "unique")},
			want:  "against the tag: 0 added, 1 changed, 0 breaking\n  ~ favro_list_cards (inputSchema.properties)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			was, err := readToolSurface(dumpOf(t, tc.old...))
			if err != nil {
				t.Fatal(err)
			}
			now, err := readToolSurface(dumpOf(t, tc.fresh...))
			if err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			compareSurfaces(was, now).write(&out, "the tag")
			if out.String() != tc.want {
				t.Errorf("the report is\n%s\nwant\n%s", out.String(), tc.want)
			}
		})
	}
}

// The committed dump is compared by surface, not by bytes: the build
// stamp differs between a maintainer's build and CI's. Everything else
// counts, an annotation the old projection did not read included.
func TestSameSurfaceIgnoresTheBuildStamp(t *testing.T) {
	tools := []schemaTool{tool("favro_ping", nil)}
	committed := []byte(`{"version":"v1.1.2 (abc1234)","sdk":"v1.7.0","tools":` + string(mustTools(t, tools)) + `}`)
	current := []byte(`{"version":"dev (unknown)","sdk":"v1.8.0","tools":` + string(mustTools(t, tools)) + `}`)
	if err := sameSurface(io.Discard, committed, current); err != nil {
		t.Errorf("two dumps of the same surface differ only by the stamp, and sameSurface = %v; this gate would be "+
			"red on every commit for a reason that has nothing to do with the tool surface", err)
	}

	for _, changed := range [][]byte{
		[]byte(`{"tools":` + string(mustTools(t, []schemaTool{tool("favro_ping", nil, "verbose")})) + `}`),
		[]byte(`{"tools":[{"name":"favro_ping","annotations":{"idempotentHint":true},"inputSchema":{"properties":{}}}]}`),
		[]byte(`{"tools":` + string(mustTools(t, tools)) + `,"instructions":"Start with favro_ping."}`),
	} {
		var out sink
		if err := sameSurface(&out, committed, changed); err == nil || !strings.Contains(err.Error(), "make schemas") {
			t.Errorf("a surface change passed, or failed without naming make schemas: %v\n%s", err, out.String())
		}
	}
}

func TestSameSurfaceRefusesADocumentThatIsNotOne(t *testing.T) {
	good := dumpOf(t, tool("favro_ping", nil))
	if err := sameSurface(io.Discard, []byte(`{"tools":[]}`), good); err == nil {
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
