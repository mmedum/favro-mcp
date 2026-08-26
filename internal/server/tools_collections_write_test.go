package server

import (
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_CreateCollection_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("create_collection must POST; got %s", r.Method)
		}
		if r.URL.Path != "/collections" {
			t.Errorf("expected /collections; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectionId":"c-new","name":"Eng","color":"blue"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCollectionToolName,
		Arguments: map[string]any{
			"name":  "Eng",
			"color": "blue",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Collection]](t, res)
	require.False(t, out.DryRun)
	require.Equal(t, "c-new", out.Result.CollectionID)
}

func TestMCP_CreateCollection_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createCollectionToolName,
		Arguments: map[string]any{"name": "preview", "dry_run": true},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Collection]](t, res)
	require.True(t, out.DryRun)
	require.Equal(t, http.MethodPost, out.WouldCall.Method)
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_CreateCollection_MissingName(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, createCollectionToolName, "name")
}

func TestMCP_UpdateCollection_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_collection must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectionId":"c-1","name":"renamed"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateCollectionToolName,
		Arguments: map[string]any{"collection_id": "c-1", "name": "renamed"},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Collection]](t, res)
	require.Equal(t, "renamed", out.Result.Name)
}

func TestMCP_UpdateCollection_NoChanges_DryRun(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateCollectionToolName,
		Arguments: map[string]any{"collection_id": "c-1", "dry_run": true},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Collection]](t, res)
	require.Contains(t, out.PredictedStateDiff, "no-op")
}

func TestMCP_UpdateCollection_MissingCollectionID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateCollectionToolName, "collection_id")
}

func TestMCP_DeleteCollection_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_collection must DELETE; got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteCollectionToolName,
		Arguments: map[string]any{"collection_id": "c-1"},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.False(t, out.DryRun)
	require.NotNil(t, out.Result)
}

func TestMCP_DeleteCollection_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteCollectionToolName,
		Arguments: map[string]any{"collection_id": "c-1", "dry_run": true},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[struct{}]](t, res)
	require.True(t, out.DryRun)
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_DeleteCollection_MissingCollectionID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteCollectionToolName, "collection_id")
}

// Collection membership has an asymmetric wire contract: members come
// back on read in `sharedToUsers`, but invites are sent in
// `shareToUsers` and role changes in `members`. Posting the read-shaped
// key is accepted by Favro and silently drops the invites, so pin the
// write keys.
func TestMCP_CreateCollection_SendsShareToUsers(t *testing.T) {
	t.Parallel()

	var body []byte
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectionId":"c-new","name":"Eng"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCollectionToolName,
		Arguments: map[string]any{
			"name": "Eng",
			"share_to_users": []map[string]any{
				{"email": "someone@example.invalid", "role": "edit"},
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	require.Contains(t, string(body), `"shareToUsers":[{"email":"someone@example.invalid","role":"edit"}]`)
	require.NotContains(t, string(body), `"sharedToUsers"`,
		"the read-shaped key is ignored by Favro on write")
}

func TestMCP_UpdateCollection_SeparatesInvitesFromMemberChanges(t *testing.T) {
	t.Parallel()

	var body []byte
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collectionId":"c-1","name":"Eng"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCollectionToolName,
		Arguments: map[string]any{
			"collection_id": "c-1",
			"share_to_users": []map[string]any{
				{"email": "new@example.invalid", "role": "view"},
			},
			"members": []map[string]any{
				{"userId": "u-1", "role": "admin"},
				{"userId": "u-2", "delete": true},
			},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	require.Contains(t, string(body), `"shareToUsers":[{"email":"new@example.invalid","role":"view"}]`)
	require.Contains(t, string(body), `"members":[{"userId":"u-1","role":"admin"},{"userId":"u-2","delete":true}]`)
}
