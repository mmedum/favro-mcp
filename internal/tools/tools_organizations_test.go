package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListOrganizations_HappyPath(t *testing.T) {
	t.Parallel()

	page := favro.PageEnvelope[favro.Organization]{
		Limit:     100,
		Page:      0,
		Pages:     2,
		RequestID: "req-1",
		Entities: []favro.Organization{
			{OrganizationID: "org-1", Name: "Acme"},
		},
	}
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(page)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listOrgsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.Organization]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Name; got != "Acme" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "Acme")
	}
	if got := out.Page; got != 1 {
		t.Errorf("Favro page 0 is page 1 on this surface: got %v, want %v", got, 1)
	}
	if got := out.TotalPages; got != 2 {
		t.Errorf("out.TotalPages = %v, want %v", got, 2)
	}
	if out.NextPage == nil {
		t.Fatal("two-page response must surface next_page")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
	if got := out.RequestID; got != "req-1" {
		t.Errorf("out.RequestID = %v, want %v", got, "req-1")
	}
}

func TestMCP_ListOrganizations_LastPage_NoNextPage(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Organization]{
			Page:      0,
			Pages:     1,
			RequestID: "req-x",
			Entities:  []favro.Organization{{OrganizationID: "o", Name: "Solo"}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listOrgsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	out := decodeStructured[listOutput[favro.Organization]](t, res)
	if out.NextPage != nil {
		t.Errorf("single-page response must omit next_page: %v", out.NextPage)
	}
}

func TestMCP_ListOrganizations_ForwardsRequestIDOnPage2(t *testing.T) {
	t.Parallel()

	var sawRequestID string
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequestID = r.Header.Get("X-Favro-Backend-Identifier")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Organization]{Page: 1, Pages: 2})
	}))

	cs := connectInMemoryWith(t, c)
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: listOrgsToolName,
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

func TestMCP_GetOrganization_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The bound organization, not one the caller named. Favro routes
		// this call by the organizationId header and ignores the path
		// segment, so a tool that took an id handed back whatever the
		// header said while looking as though it had honoured the input.
		// require.* would call t.FailNow from the handler goroutine,
		// which is unsafe; t.Errorf returns control to the handler.
		if r.URL.Path != "/organizations/synthetic-organization" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Organization{
			OrganizationID: "synthetic-organization",
			Name:           "Looked Up",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      getOrgToolName,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.Organization](t, res)
	if got := out.OrganizationID; got != "synthetic-organization" {
		t.Errorf("out.OrganizationID = %v, want the bound organization", got)
	}
	if got := out.Name; got != "Looked Up" {
		t.Errorf("out.Name = %v, want %v", got, "Looked Up")
	}
}

// The tool must not take an organization_id. Hard rule 8 says no tool
// does, and this one did — required, and inert, because Favro routes by
// header. `gates rule8` holds the whole surface; this holds the one tool
// the rule was actually broken by.
func TestMCP_GetOrganization_TakesNoOrganizationID(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Organization{OrganizationID: "synthetic-organization"})
	}))
	cs := connectInMemoryWith(t, c)

	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != getOrgToolName {
			continue
		}
		// Through JSON rather than the SDK's schema type, which is `any`
		// here: the question is what a client receives on the wire.
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "organization_id") {
			t.Error("favro_get_organization declares an organization_id input; Favro ignores it " +
				"and this server is bound to one organization")
		}
		return
	}
	t.Fatalf("%s is not registered; this test is reading nothing", getOrgToolName)
}
