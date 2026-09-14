package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListUsers_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.User]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-u",
			Entities: []favro.User{
				{UserID: "u-1", Name: "Alice", OrganizationRole: "fullMember"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listUsersToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.User]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Name; got != "Alice" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "Alice")
	}
	if out.NextPage == nil {
		t.Fatal("two-page response must surface next_page")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
	if got := out.RequestID; got != "req-u" {
		t.Errorf("out.RequestID = %v, want %v", got, "req-u")
	}
}

func TestMCP_ListUsers_ForwardsRequestIDOnPage2(t *testing.T) {
	t.Parallel()

	var sawRequestID string
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequestID = r.Header.Get("X-Favro-Backend-Identifier")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.User]{Page: 1, Pages: 2})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listUsersToolName,
		Arguments: map[string]any{
			"page":       1,
			"request_id": "req-from-prior-page",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := sawRequestID; got != "req-from-prior-page" {
		t.Errorf("page > 0 must thread request_id back as X-Favro-Backend-Identifier: got %v, want %v", got, "req-from-prior-page")
	}
}

func TestMCP_GetUser_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/u-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.User{
			UserID:           "u-zzz",
			Name:             "Looked Up",
			OrganizationRole: "administrator",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getUserToolName,
		Arguments: map[string]any{
			"user_id": "u-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.User](t, res)
	if got := out.UserID; got != "u-zzz" {
		t.Errorf("out.UserID = %v, want %v", got, "u-zzz")
	}
	if got := out.OrganizationRole; got != "administrator" {
		t.Errorf("out.OrganizationRole = %v, want %v", got, "administrator")
	}
}

func TestMCP_GetUser_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getUserToolName, "user_id")
}
