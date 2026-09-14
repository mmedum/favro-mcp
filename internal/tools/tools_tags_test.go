package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListTags_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-tags",
			Entities: []favro.Tag{
				{TagID: "t-1", Name: "blocker", Color: "red"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listTagsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Tag]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Name; got != "blocker" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "blocker")
	}
	if got := out.Items[0].Color; got != "red" {
		t.Errorf("out.Items[0].Color = %v, want %v", got, "red")
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
}

func TestMCP_GetTag_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tags/t-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Tag{
			TagID: "t-zzz",
			Name:  "looked up",
			Color: "lime",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getTagToolName,
		Arguments: map[string]any{
			"tag_id": "t-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.Tag](t, res)
	if got := out.TagID; got != "t-zzz" {
		t.Errorf("out.TagID = %v, want %v", got, "t-zzz")
	}
	if got := out.Name; got != "looked up" {
		t.Errorf("out.Name = %v, want %v", got, "looked up")
	}
	if got := out.Color; got != "lime" {
		t.Errorf("out.Color = %v, want %v", got, "lime")
	}
}

func TestMCP_GetTag_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getTagToolName, "tag_id")
}
