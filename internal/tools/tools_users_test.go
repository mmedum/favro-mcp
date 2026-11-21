package tools

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
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

// A second page reaches Favro as its reference asks: the first page's
// requestId as a query parameter, and the X-Favro-Backend-Identifier the
// first page came back with as a header, which the caller never sees.
// https://favro.com/developer/#pagination
func TestMCP_ListUsers_SecondPageSendsTheFirstPagesRouting(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seen []*http.Request
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Clone(r.Context()))
		mu.Unlock()
		page := 0
		if r.URL.Query().Get("page") != "" {
			page = 1
		}
		w.Header().Set("X-Favro-Backend-Identifier", "backend-3")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.User]{Page: page, Pages: 2, RequestID: "req-u"})
	}))

	cs := connectInMemoryWith(t, c)
	for _, args := range []map[string]any{{"limit": 1}, {"page": 2, "request_id": "req-u"}} {
		if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: listUsersToolName, Arguments: args}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("Favro saw %d requests, want 2", len(seen))
	}
	first, second := seen[0].URL.Query(), seen[1].URL.Query()
	if first.Get("limit") != "1" || first.Has("page") || first.Has("requestId") {
		t.Errorf("first page query = %v, want limit=1 alone", first)
	}
	if second.Get("page") != "1" || second.Get("requestId") != "req-u" {
		t.Errorf("second page query = %v, want page=1 and requestId=req-u", second)
	}
	if got := seen[1].Header.Get("X-Favro-Backend-Identifier"); got != "backend-3" {
		t.Errorf("second page X-Favro-Backend-Identifier = %q, want the first page's backend-3", got)
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
