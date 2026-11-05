package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/redact"
)

// A probe the run deleted reads back as gone when Favro refuses the
// read, as not found or as forbidden. Any other answer fails, and so
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

	// The last two lines each run prints are the delete's check and the
	// read-back's, each under the name its probe kind gives it.
	const (
		collectionDeleted = "ok   favro_delete_collection with confirm deletes it"
		collectionGone    = "ok   the collection is gone"
		tagDeleted        = "ok   favro_delete_tag with confirm deletes it"
		tagGone           = "ok   the tag is gone"
	)
	cases := []struct {
		name    string
		probe   func(*scratch, *checks) error
		answers map[string][]string
		checks  int
		failed  int
		tail    [2]string
	}{
		{
			"a collection read as forbidden", probeCollection, collection(deleted, forbidden), 10, 0,
			[2]string{collectionDeleted, collectionGone},
		},
		{
			"a collection read as not found", probeCollection, collection(deleted, notFound), 10, 0,
			[2]string{collectionDeleted, collectionGone},
		},
		{
			"a collection that still reads", probeCollection, collection(deleted, `{"collectionId":"col-probe"}`), 10, 1,
			[2]string{collectionDeleted, "FAIL the collection is gone: "},
		},
		{
			"a collection read as unavailable", probeCollection, collection(deleted, "[unavailable] try again"), 10, 1,
			[2]string{collectionDeleted, "FAIL the collection is gone: [unavailable] try again"},
		},
		{
			"a forbidden read after a failed delete", probeCollection, collection(deleteFails, forbidden), 10, 2,
			[2]string{"FAIL favro_delete_collection with confirm deletes it: " + deleteFails, "FAIL the collection is gone: " + forbidden},
		},
		{
			"a tag read as forbidden", probeTag, tag(deleted, forbidden), 4, 0,
			[2]string{tagDeleted, tagGone},
		},
		{
			"a tag that still reads", probeTag, tag(deleted, `{"tagId":"tag-probe"}`), 4, 1,
			[2]string{tagDeleted, "FAIL the tag is gone: "},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			sc := newScratch(&fakeFavro{results: tc.answers}, scratchTools)
			c := &checks{out: &printer{red: redact.New(), w: bufio.NewWriter(&buf)}}
			if err := tc.probe(sc, c); err != nil {
				t.Fatal(err)
			}
			if c.passed+c.failed != tc.checks || c.failed != tc.failed {
				t.Errorf("%d checks passed and %d failed, want %d passed and %d failed",
					c.passed, c.failed, tc.checks-tc.failed, tc.failed)
			}
			lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
			if got := [2]string(lines[len(lines)-2:]); got != tc.tail {
				t.Errorf("the run ended\n%q\nwant\n%q", got, tc.tail)
			}
		})
	}
}
