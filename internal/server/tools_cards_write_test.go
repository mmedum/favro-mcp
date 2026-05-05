package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_CreateCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("create_card must POST; got %s", r.Method)
		}
		if r.URL.Path != "/cards" {
			t.Errorf("expected /cards; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-new","cardCommonId":"cc-new","name":"hello","widgetCommonId":"w-1"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCardToolName,
		Arguments: map[string]any{
			"name":             "hello",
			"widget_common_id": "w-1",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.False(t, out.DryRun)
	require.Equal(t, "ci-new", out.Result.CardID)
}

func TestMCP_CreateCard_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCardToolName,
		Arguments: map[string]any{
			"name":             "preview",
			"widget_common_id": "w-1",
			"column_id":        "col-1",
			"dry_run":          true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.True(t, out.DryRun)
	require.Equal(t, http.MethodPost, out.WouldCall.Method)
	require.Contains(t, out.WouldCall.URL, "/cards")
	require.Contains(t, out.PredictedStateDiff, "preview")
	require.Contains(t, out.PredictedStateDiff, "w-1")
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_CreateCard_MissingName(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, createCardToolName, "name")
}

// TestMCP_CreateCard_InvalidatesSearchCacheOnSuccess pins the
// contract that a successful live create busts the search-cards
// cache (otherwise the new card stays invisible until 60s pass).
// Dry-run must NOT invalidate.
func TestMCP_CreateCard_InvalidatesSearchCacheOnSuccess(t *testing.T) {
	t.Parallel()

	var listCalls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{
				Pages: 1,
				Entities: []favro.Card{
					{CardID: "ci-1", CardCommonID: "cc-1", Name: "alpha", WidgetCommonID: "w-1"},
				},
			})
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cardId":"ci-new","cardCommonId":"cc-new","name":"alpha","widgetCommonId":"w-1"}`))
		}
	}))

	cs := connectInMemoryWith(t, c)

	// Warm the search-cards cache.
	_, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "alpha",
			"widget_common_id": "w-1",
		},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, listCalls.Load())

	// Re-search — must hit the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "alpha",
			"widget_common_id": "w-1",
		},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, listCalls.Load(), "second search must hit the cache")

	// Dry-run create — must NOT invalidate.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCardToolName,
		Arguments: map[string]any{
			"name":             "preview",
			"widget_common_id": "w-1",
			"dry_run":          true,
		},
	})
	require.NoError(t, err)
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "alpha",
			"widget_common_id": "w-1",
		},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, listCalls.Load(),
		"dry_run create_card must NOT invalidate the search-cards cache")

	// Live create — must invalidate.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCardToolName,
		Arguments: map[string]any{
			"name":             "actual",
			"widget_common_id": "w-1",
		},
	})
	require.NoError(t, err)
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "alpha",
			"widget_common_id": "w-1",
		},
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, listCalls.Load(),
		"live create_card must invalidate the search-cards cache so the next search re-fetches")
}

func TestMCP_UpdateCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_card must PUT; got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/cards/") {
			t.Errorf("expected /cards/{id}; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"renamed"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id": "ci-1",
			"name":    "renamed",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.Equal(t, "renamed", out.Result.Name)
}

func TestMCP_UpdateCard_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":     "ci-1",
			"name":        "renamed",
			"add_tag_ids": []string{"t-1", "t-2"},
			"dry_run":     true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.True(t, out.DryRun)
	require.Equal(t, http.MethodPut, out.WouldCall.Method)
	require.Contains(t, out.WouldCall.URL, "/cards/ci-1")
	require.Contains(t, out.PredictedStateDiff, "renamed")
	require.Contains(t, out.PredictedStateDiff, "+2 tag")
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_UpdateCard_NoChanges_DryRun(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateCardToolName,
		Arguments: map[string]any{"card_id": "ci-1", "dry_run": true},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.Contains(t, out.PredictedStateDiff, "no-op",
		"a dry-run with no fields set must report a no-op")
}

func TestMCP_UpdateCard_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateCardToolName, "card_id")
}

func TestMCP_ArchiveCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("archive_card must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","archived":true}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      archiveCardToolName,
		Arguments: map[string]any{"card_id": "ci-1"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.True(t, out.Result.IsArchived)
}

func TestMCP_ArchiveCard_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      archiveCardToolName,
		Arguments: map[string]any{"card_id": "ci-1", "dry_run": true},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.True(t, out.DryRun)
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_ArchiveCard_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, archiveCardToolName, "card_id")
}

func TestMCP_UnarchiveCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unarchive_card must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","archived":false}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      unarchiveCardToolName,
		Arguments: map[string]any{"card_id": "ci-1"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.False(t, out.Result.IsArchived)
}

func TestMCP_UnarchiveCard_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, unarchiveCardToolName, "card_id")
}

func TestMCP_MoveCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("move_card must PUT; got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","columnId":"col-2"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: moveCardToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"column_id": "col-2",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.Equal(t, "col-2", out.Result.ColumnID)
}

func TestMCP_MoveCard_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: moveCardToolName,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-2",
			"column_id":        "col-2",
			"dry_run":          true,
		},
	})
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.True(t, out.DryRun)
	require.Contains(t, out.PredictedStateDiff, "w-2")
	require.Contains(t, out.PredictedStateDiff, "col-2")
	require.EqualValues(t, 0, calls.Load())
}

// TestMCP_MoveCard_EmptyMove_SurfacesFavroError pins the contract
// that a fully-empty move surfaces the favro-layer typed error
// rather than silently PUT-ing a no-op.
func TestMCP_MoveCard_EmptyMove_SurfacesFavroError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: moveCardToolName,
		Arguments: map[string]any{
			"card_id": "ci-1",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "fully-empty move must surface as a tool error")
	require.EqualValues(t, 0, calls.Load(), "must short-circuit before any Favro call")
}

func TestMCP_MoveCard_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, moveCardToolName, "card_id")
}

func TestMCP_DeleteCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_card must DELETE; got %s", r.Method)
		}
		if r.URL.Query().Get("everywhere") != "" {
			t.Errorf("everywhere=false must not appear on the URL; got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["ci-1"]`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteCardToolName,
		Arguments: map[string]any{"card_id": "ci-1"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.DeleteCardResponse]](t, res)
	require.False(t, out.DryRun)
	require.Equal(t, favro.DeleteCardResponse{"ci-1"}, *out.Result)
}

func TestMCP_DeleteCard_Everywhere(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("everywhere") != "true" {
			t.Errorf("expected everywhere=true; got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["ci-1","ci-2"]`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: deleteCardToolName,
		Arguments: map[string]any{
			"card_id":    "ci-1",
			"everywhere": true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.DeleteCardResponse]](t, res)
	require.Len(t, *out.Result, 2)
}

func TestMCP_DeleteCard_DryRun_Everywhere_StateDiff(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: deleteCardToolName,
		Arguments: map[string]any{
			"card_id":    "ci-1",
			"everywhere": true,
			"dry_run":    true,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	out := decodeStructured[writeOutput[favro.DeleteCardResponse]](t, res)
	require.True(t, out.DryRun)
	require.Contains(t, out.PredictedStateDiff, "EVERY widget",
		"everywhere=true dry-run must announce the cross-widget purge loudly")
	require.Contains(t, out.WouldCall.URL, "everywhere=true")
	require.EqualValues(t, 0, calls.Load())
}

func TestMCP_DeleteCard_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteCardToolName, "card_id")
}
