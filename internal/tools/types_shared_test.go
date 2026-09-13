package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// TestFavroPage is the conversion on its own: this surface counts from
// one, Favro counts from zero, and an omitted page is the first page
// either way.
func TestFavroPage(t *testing.T) {
	t.Parallel()

	cases := map[int]int{
		0:  0, // omitted
		1:  0, // the first page
		2:  1,
		3:  2,
		-1: 0, // nonsense in, the first page out — never a negative page
	}
	for in, want := range cases {
		require.Equal(t, want, listInput{Page: in}.favroPage(), "page %d", in)
	}
}

// TestListToolPageNumberingIsOneIndexed is the regression test for the
// bug this conversion fixes.
//
// The schema has said "1-indexed" since these tools shipped and the
// value went to Favro untouched, so a caller that believed the schema
// and asked for page 1 was served Favro's page 1 — the second page —
// and never saw the first. Nothing failed: a valid page of real
// results came back and the missing rows were simply absent, which is
// why no test caught it and a live run had to.
//
// The assertion is on the query Favro actually received, because that
// is the only place the off-by-one was ever visible.
func TestListToolPageNumberingIsOneIndexed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		arguments map[string]any
		wantQuery string // what Favro's `page` parameter should be
	}{
		{"omitted is the first page", map[string]any{}, ""},
		{"page 1 is the first page", map[string]any{"page": 1}, ""},
		{"page 2 is Favro's page 1", map[string]any{"page": 2}, "1"},
		{"page 3 is Favro's page 2", map[string]any{"page": 3}, "2"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotQuery string
			c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query().Get("page")
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Organization]{
					Page: 0, Pages: 1, Entities: []favro.Organization{{Name: "Fixture"}},
				})
			}))

			cs := connectInMemoryWith(t, c)
			args := map[string]any{}
			for k, v := range tc.arguments {
				args[k] = v
			}
			if _, ok := args["page"]; ok {
				// Favro's pagination needs the prior response's
				// request_id for any page but the first, and the tool
				// requires it too.
				args["request_id"] = "req-fixture"
			}
			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      listOrgsToolName,
				Arguments: args,
			})
			require.NoError(t, err)
			require.False(t, res.IsError, "the call must succeed: %v", res.Content)

			require.Equal(t, tc.wantQuery, gotQuery,
				"tool page %v must reach Favro as page %q", tc.arguments["page"], tc.wantQuery)
		})
	}
}

// TestNewListOutputCountsFromOne is the other half: what comes back
// reads the way the input schema promises, and `next_page` can be
// handed straight back to the same tool.
func TestNewListOutputCountsFromOne(t *testing.T) {
	t.Parallel()

	// Favro's first page of three.
	out := newListOutput(favro.PageEnvelope[favro.Organization]{Page: 0, Pages: 3})
	require.Equal(t, 1, out.Page)
	require.Equal(t, 3, out.TotalPages)
	require.NotNil(t, out.NextPage)
	require.Equal(t, 2, *out.NextPage)

	// And passing that next_page back asks Favro for its page 1.
	require.Equal(t, 1, listInput{Page: *out.NextPage}.favroPage())

	// The last page offers nothing to follow.
	out = newListOutput(favro.PageEnvelope[favro.Organization]{Page: 2, Pages: 3})
	require.Equal(t, 3, out.Page)
	require.Nil(t, out.NextPage)
}
