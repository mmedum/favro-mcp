package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[listOutput[favro.User]](t, res)
	require.Len(t, out.Items, 1)
	require.Equal(t, "Alice", out.Items[0].Name)
	require.NotNil(t, out.NextPage, "two-page response must surface next_page")
	require.Equal(t, 1, *out.NextPage)
	require.Equal(t, "req-u", out.RequestID)
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
	require.NoError(t, err)
	require.Equal(t, "req-from-prior-page", sawRequestID,
		"page > 0 must thread request_id back as X-Favro-Backend-Identifier")
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
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[favro.User](t, res)
	require.Equal(t, "u-zzz", out.UserID)
	require.Equal(t, "administrator", out.OrganizationRole)
}

func TestMCP_GetUser_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()

	calls := 0
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      getUserToolName,
		Arguments: map[string]any{}, // no user_id
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "user_id",
		"the LLM-visible error must name the missing field")
	require.Equal(t, 0, calls, "missing id must short-circuit before any Favro call")
}
