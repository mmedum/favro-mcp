package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListGroups_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Group]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-g",
			Entities: []favro.Group{
				{
					GroupID: "g-1",
					Name:    "Engineers",
					Members: []favro.GroupMember{
						{UserID: "u-1", Role: "administrator"},
					},
				},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listGroupsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Group]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Name; got != "Engineers" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "Engineers")
	}
	if len(out.Items[0].Members) != 1 {
		t.Fatalf("len(out.Items[0].Members) = %d, want 1", len(out.Items[0].Members))
	}
	if got := out.Items[0].Members[0].UserID; got != "u-1" {
		t.Errorf("out.Items[0].Members[0].UserID = %v, want %v", got, "u-1")
	}
	if got := out.Items[0].Members[0].Role; got != "administrator" {
		t.Errorf("out.Items[0].Members[0].Role = %v, want %v", got, "administrator")
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
}

func TestMCP_GetGroup_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/groups/g-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Group{
			GroupID: "g-zzz",
			Name:    "looked up",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getGroupToolName,
		Arguments: map[string]any{
			"group_id": "g-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.Group](t, res)
	if got := out.GroupID; got != "g-zzz" {
		t.Errorf("out.GroupID = %v, want %v", got, "g-zzz")
	}
	if got := out.Name; got != "looked up" {
		t.Errorf("out.Name = %v, want %v", got, "looked up")
	}
	if len(out.Members) != 0 {
		t.Errorf("groups without members must NOT carry the field: got %v", out.Members)
	}
}

func TestMCP_GetGroup_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getGroupToolName, "group_id")
}
