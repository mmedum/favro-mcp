package main

import (
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/redact"
)

// A probe the run deleted reads back as gone when Favro refuses the read
// as not found or as forbidden: on 2026-10-23 it answered 403 for a
// collection the run had just deleted. Any other answer fails, and so
// does either refusal after a delete that failed.
func TestTheReadBackAfterADeleteCountsNotFoundAndForbiddenAsGone(t *testing.T) {
	const (
		deleted     = `{}`
		deleteFails = "[ambiguous_outcome] the delete was sent and no answer came back"
		forbidden   = "[forbidden] Favro denied access (HTTP 403)"
		notFound    = "[not_found] no such object (HTTP 404)"
	)
	collection := func(deleteAnswer, readBack string) map[string][]string {
		return map[string][]string{
			"favro_create_collection":  {"[blocked] a public collection", `{"result":{"collectionId":"col-probe"}}`},
			"favro_resolve_collection": {`{"candidates":[]}`},
			"favro_widen_collection_sharing": {
				"[conflict] not this collection's name",
				`opened; verified by reading the collection back: public_sharing is "organization"`,
			},
			"favro_get_collection": {
				`{"publicSharing":"users"}`, `{"publicSharing":"organization"}`, `{"publicSharing":"users"}`, readBack,
			},
			"favro_update_collection": {`{}`},
			"favro_delete_collection": {"[blocked] confirm: true is required", deleteAnswer},
		}
	}
	tag := func(deleteAnswer, readBack string) map[string][]string {
		return map[string][]string{
			"favro_create_tag": {`{"result":{"tagId":"tag-probe"}}`},
			"favro_delete_tag": {"[blocked] confirm: true is required", deleteAnswer},
			"favro_get_tag":    {`{"tagId":"tag-probe"}`, readBack},
		}
	}

	cases := []struct {
		name    string
		probe   func(*scratch, *checks) error
		answers map[string][]string
		checks  int
		failed  int
	}{
		{"a collection read as forbidden", probeCollection, collection(deleted, forbidden), 10, 0},
		{"a collection read as not found", probeCollection, collection(deleted, notFound), 10, 0},
		{"a collection that still reads", probeCollection, collection(deleted, `{"collectionId":"col-probe"}`), 10, 1},
		{"a collection read as unavailable", probeCollection, collection(deleted, "[unavailable] try again"), 10, 1},
		{"a forbidden read after a failed delete", probeCollection, collection(deleteFails, forbidden), 10, 2},
		{"a tag read as forbidden", probeTag, tag(deleted, forbidden), 4, 0},
		{"a tag that still reads", probeTag, tag(deleted, `{"tagId":"tag-probe"}`), 4, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := newScratch(&fakeFavro{results: tc.answers}, scratchTools)
			c := &checks{out: newPrinter(redact.New())}
			if err := tc.probe(sc, c); err != nil {
				t.Fatal(err)
			}
			if c.passed+c.failed != tc.checks || c.failed != tc.failed {
				t.Errorf("%d checks passed and %d failed, want %d passed and %d failed",
					c.passed, c.failed, tc.checks-tc.failed, tc.failed)
			}
		})
	}
}
