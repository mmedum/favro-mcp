package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListCollections_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Collection]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-c",
			Entities: []favro.Collection{
				{CollectionID: "c-1", Name: "Engineering", Color: "blue"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listCollectionsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Collection]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Name; got != "Engineering" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "Engineering")
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
	if got := out.RequestID; got != "req-c" {
		t.Errorf("out.RequestID = %v, want %v", got, "req-c")
	}
}

func TestMCP_GetCollection_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/collections/c-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Collection{
			CollectionID: "c-zzz",
			Name:         "Looked Up",
			Archived:     true,
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getCollectionToolName,
		Arguments: map[string]any{
			"collection_id": "c-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.Collection](t, res)
	if got := out.CollectionID; got != "c-zzz" {
		t.Errorf("out.CollectionID = %v, want %v", got, "c-zzz")
	}
	if !out.Archived {
		t.Error("out.Archived = false, want true")
	}
}

func TestMCP_GetCollection_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getCollectionToolName, "collection_id")
}
