package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
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
		if got := (listInput{Page: in}.favroPage()); got != want {
			t.Errorf("page %d: got %v, want %v", in, got, want)
		}
	}
}

// TestListToolPageNumberingIsOneIndexed holds that a caller's page 1
// reaches Favro as its page 0. Sent untouched, page 1 would serve
// Favro's page 1, the second page, and nothing would fail: a valid page
// of real results comes back and the first page's rows are simply
// absent.
//
// The assertion is on the query Favro actually received, because that
// is the only place the off-by-one is visible.
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
			if err := err; err != nil {
				t.Fatalf("err: %v", err)
			}
			if res.IsError {
				t.Errorf("the call must succeed: %v", res.Content)
			}

			if got := gotQuery; got != tc.wantQuery {
				t.Errorf("tool page %v must reach Favro as page %q: got %v, want %v", tc.arguments["page"], tc.wantQuery, got, tc.wantQuery)
			}
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
	if got := out.Page; got != 1 {
		t.Errorf("out.Page = %v, want %v", got, 1)
	}
	if got := out.TotalPages; got != 3 {
		t.Errorf("out.TotalPages = %v, want %v", got, 3)
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}

	// And passing that next_page back asks Favro for its page 1.
	if got := (listInput{Page: *out.NextPage}.favroPage()); got != 1 {
		t.Errorf("listInput{Page: *out.NextPage}.favroPage() = %v, want %v", got, 1)
	}

	// The last page offers nothing to follow.
	out = newListOutput(favro.PageEnvelope[favro.Organization]{Page: 2, Pages: 3})
	if got := out.Page; got != 3 {
		t.Errorf("out.Page = %v, want %v", got, 3)
	}
	if out.NextPage != nil {
		t.Errorf("out.NextPage = %v, want nil", out.NextPage)
	}
}
