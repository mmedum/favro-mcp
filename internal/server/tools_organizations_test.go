package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.Organization]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "Acme", out.Items[0].Name)
	require.Equal(t, 0, out.Page)
	require.Equal(t, 2, out.TotalPages)
	require.NotNil(t, out.NextPage, "two-page response must surface next_page")
	require.Equal(t, 1, *out.NextPage)
	require.Equal(t, "req-1", out.RequestID)
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
	require.NoError(t, err)
	out := decodeStructured[listOutput[favro.Organization]](t, res)
	require.Nil(t, out.NextPage, "single-page response must omit next_page")
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
	require.NoError(t, err)
	require.Equal(t, "req-from-prior-page", sawRequestID,
		"page > 0 must thread request_id back as X-Favro-Backend-Identifier")
}

func TestMCP_GetOrganization_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// require.* would call t.FailNow from the handler goroutine,
		// which is unsafe; t.Errorf returns control to the handler.
		if r.URL.Path != "/organizations/org-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Organization{
			OrganizationID: "org-zzz",
			Name:           "Looked Up",
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getOrgToolName,
		Arguments: map[string]any{
			"organization_id": "org-zzz",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.Organization](t, res)
	require.Equal(t, "org-zzz", out.OrganizationID)
	require.Equal(t, "Looked Up", out.Name)
}

func TestMCP_GetOrganization_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertGetMissingIDFails(t, getOrgToolName, "organization_id")
}
