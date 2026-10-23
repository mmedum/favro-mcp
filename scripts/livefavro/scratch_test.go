package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/redact"
)

// fakeFavro records the calls that reached it, and answers each tool
// with the next of its queued results, or an empty result. A queued
// answer that starts with "{" is the result's structured half, one that
// starts with "[" is an error's text, as "[forbidden] …", and anything
// else is the result's readable half.
type fakeFavro struct {
	calls   []string
	results map[string][]string
}

func (f *fakeFavro) call(tool string, args map[string]any) (*callResult, error) {
	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	f.calls = append(f.calls, tool+" "+string(body))
	res := &callResult{StructuredContent: json.RawMessage(`{}`)}
	if queue := f.results[tool]; len(queue) > 0 {
		answer := queue[0]
		f.results[tool] = queue[1:]
		if strings.HasPrefix(answer, "{") {
			res.StructuredContent = json.RawMessage(answer)
			return res, nil
		}
		text, err := json.Marshal(answer)
		if err != nil {
			return nil, err
		}
		frame := fmt.Sprintf(`{"isError":%t,"content":[{"type":"text","text":%s}]}`, strings.HasPrefix(answer, "["), text)
		if err := json.Unmarshal([]byte(frame), res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

var scratchTools = map[string]tool{
	"favro_get_tag":                  {readOnly: true},
	"favro_create_tag":               {},
	"favro_delete_tag":               {},
	"favro_create_card":              {},
	"favro_update_card":              {},
	"favro_resolve_tag":              {readOnly: true},
	"favro_resolve_collection":       {readOnly: true},
	"favro_get_collection":           {readOnly: true},
	"favro_widen_collection_sharing": {},
	"favro_update_collection":        {},
	"favro_delete_collection":        {},
}

// The -writes run sends a write only to an object it created. The guard
// is what holds that, so every refusal here is a call that never
// reaches the server.
func TestScratchWritesOnlyToObjectsTheRunCreated(t *testing.T) {
	favro := &fakeFavro{results: map[string][]string{
		"favro_create_tag": {`{"result":{"tagId":"tag-the-run-made"}}`},
	}}
	sc := newScratch(favro, scratchTools)

	refusals := []struct {
		why  string
		send func() (*callResult, error)
	}{
		{"a delete of a tag the run did not create", func() (*callResult, error) {
			return sc.call("favro_delete_tag", map[string]any{"tag_id": "tag-already-there", "confirm": true})
		}},
		{"a write that names no object", func() (*callResult, error) {
			return sc.call("favro_create_tag", map[string]any{"name": "livefavro-tag-0"})
		}},
		{"a create the run cannot clean up after", func() (*callResult, error) {
			return sc.create("favro_create_card", map[string]any{"name": "livefavro-card-0"})
		}},
		{"a probe without the probe prefix", func() (*callResult, error) {
			return sc.create("favro_create_tag", map[string]any{"name": "a tag"})
		}},
		{"a tool tools/list does not list, naming a foreign id", func() (*callResult, error) {
			return sc.call("favro_not_listed", map[string]any{"card_id": "card-already-there"})
		}},
	}
	for _, r := range refusals {
		if _, err := r.send(); err == nil {
			t.Errorf("%s was sent", r.why)
		}
	}
	if len(favro.calls) != 0 {
		t.Fatalf("refused calls reached the server: %v", favro.calls)
	}

	mustSend := func(res *callResult, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustSend(sc.call("favro_get_tag", map[string]any{"tag_id": "tag-already-there"}))
	mustSend(sc.create("favro_create_tag", map[string]any{"name": "livefavro-tag-0a1b2c3d"}))
	mustSend(sc.call("favro_delete_tag", map[string]any{"tag_id": "tag-the-run-made", "confirm": true}))

	if _, err := sc.call("favro_update_card", map[string]any{
		"card_id": "tag-the-run-made", "add_tag_ids": []any{"tag-the-run-made", "tag-already-there"},
	}); err == nil {
		t.Error("a write naming one created id and one foreign id in a list was sent")
	}

	want := []string{
		`favro_get_tag {"tag_id":"tag-already-there"}`,
		`favro_create_tag {"name":"livefavro-tag-0a1b2c3d"}`,
		`favro_delete_tag {"confirm":true,"tag_id":"tag-the-run-made"}`,
	}
	if strings.Join(favro.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("the server saw:\n%s\nwant:\n%s", strings.Join(favro.calls, "\n"), strings.Join(want, "\n"))
	}
	if got := names(sc.probes()); got != "livefavro-tag-0a1b2c3d" {
		t.Errorf("the ledger holds %q, want the one probe sent", got)
	}
}

// The sweep finds a probe by the exact name the run gave it, so a probe
// whose create was answered with nothing is still deleted, and an object
// whose name only starts the same is not touched.
func TestSweepDeletesTheProbeByItsExactNameAndReadsItBack(t *testing.T) {
	const name = "livefavro-collection-0a1b2c3d"
	found := fmt.Sprintf(`{"candidates":[
		{"collection_id":"col-the-probe","name":%q,"score":1},
		{"collection_id":"col-someone-else","name":%q,"score":0.9}
	]}`, name, name+"-kept")
	favro := &fakeFavro{results: map[string][]string{
		"favro_resolve_collection": {found, `{"candidates":[]}`},
	}}

	err := sweep(favro, []probe{{tool: "favro_create_collection", name: name}}, newPrinter(redact.New()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`favro_resolve_collection {"force_refresh":true,"name":"livefavro-collection-0a1b2c3d"}`,
		`favro_delete_collection {"collection_id":"col-the-probe","confirm":true}`,
		`favro_resolve_collection {"force_refresh":true,"name":"livefavro-collection-0a1b2c3d"}`,
	}
	if strings.Join(favro.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("the server saw:\n%s\nwant:\n%s", strings.Join(favro.calls, "\n"), strings.Join(want, "\n"))
	}
}

// A probe that reads back after its delete is reported by name, and
// fails the run.
func TestSweepFailsOnAProbeThatIsStillThere(t *testing.T) {
	const name = "livefavro-tag-0a1b2c3d"
	found := fmt.Sprintf(`{"candidates":[{"tag_id":"tag-the-probe","name":%q,"score":1}]}`, name)
	favro := &fakeFavro{results: map[string][]string{
		"favro_resolve_tag": {found, found},
	}}

	err := sweep(favro, []probe{{tool: "favro_create_tag", name: name}}, newPrinter(redact.New()))
	if err == nil || err.Error() != "1 probes are still there: livefavro-tag-0a1b2c3d" {
		t.Errorf("sweep returned %v, want the probe named", err)
	}
}
