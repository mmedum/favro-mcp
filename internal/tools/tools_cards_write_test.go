package tools

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/favroapi"
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	if got := out.Result.CardID; got != "ci-new" {
		t.Errorf("out.Result.CardID = %v, want %v", got, "ci-new")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if got := out.WouldCall.Method; got != http.MethodPost {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodPost)
	}
	if !strings.Contains(out.WouldCall.URL, "/cards") {
		t.Errorf("out.WouldCall.URL does not contain %q", "/cards")
	}
	if !strings.Contains(out.PredictedStateDiff, "preview") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "preview")
	}
	if !strings.Contains(out.PredictedStateDiff, "w-1") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "w-1")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 1 {
		t.Errorf("listCalls.Load() = %v, want %v", got, 1)
	}

	// Re-search — must hit the cache.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "alpha",
			"widget_common_id": "w-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 1 {
		t.Errorf("second search must hit the cache: got %v, want %v", got, 1)
	}

	// Dry-run create — must NOT invalidate.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCardToolName,
		Arguments: map[string]any{
			"name":             "preview",
			"widget_common_id": "w-1",
			"dry_run":          true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "alpha",
			"widget_common_id": "w-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 1 {
		t.Errorf("dry_run create_card must NOT invalidate the search-cards cache: got %v, want %v", got, 1)
	}

	// Live create — must invalidate.
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCardToolName,
		Arguments: map[string]any{
			"name":             "actual",
			"widget_common_id": "w-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	_, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: searchCardsToolName,
		Arguments: map[string]any{
			"query":            "alpha",
			"widget_common_id": "w-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := listCalls.Load(); got != 2 {
		t.Errorf("live create_card must invalidate the search-cards cache so the next search re-fetches: got %v, want %v", got, 2)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if got := out.Result.Name; got != "renamed" {
		t.Errorf("out.Result.Name = %v, want %v", got, "renamed")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if got := out.WouldCall.Method; got != http.MethodPut {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodPut)
	}
	if !strings.Contains(out.WouldCall.URL, "/cards/ci-1") {
		t.Errorf("out.WouldCall.URL does not contain %q", "/cards/ci-1")
	}
	if !strings.Contains(out.PredictedStateDiff, "renamed") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "renamed")
	}
	if !strings.Contains(out.PredictedStateDiff, "+2 tag") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "+2 tag")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
}

func TestMCP_UpdateCard_NoChanges_DryRun(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateCardToolName,
		Arguments: map[string]any{"card_id": "ci-1", "dry_run": true},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !strings.Contains(out.PredictedStateDiff, "no-op") {
		t.Errorf("a dry-run with no fields set must report a no-op: %q missing", "no-op")
	}
}

func TestMCP_UpdateCard_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, updateCardToolName, "card_id")
}

// placementFixture answers a PUT with a 200 and a GET with a card
// sitting at readBackColumn. Favro answers 200 for a body it ignored,
// so the PUT tells a caller nothing; what the GET says is the whole
// subject of these tests.
func placementFixture(t *testing.T, readBackColumn string, gets *atomic.Int32) *favroapi.Client {
	t.Helper()
	return favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && gets != nil {
			gets.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:         "ci-1",
				CardCommonID:   "cc-1",
				WidgetCommonID: "w-1",
				ColumnID:       readBackColumn,
			})
			return
		}
		// What Favro echoes to a write it discarded and to one it
		// applied is the same 200, which is the problem.
		_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","columnId":"col-2"}`))
	}))
}

// TestMCP_UpdateCard_ColumnMove_ReportsDiscardedWrite is the case
// `list_position` has documented in prose: a column change carrying
// no position is accepted and discarded, and the result looks like a
// success.
func TestMCP_UpdateCard_ColumnMove_ReportsDiscardedWrite(t *testing.T) {
	t.Parallel()

	cs := connectInMemoryWith(t, placementFixture(t, "col-1", nil))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"column_id": "col-2",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("a discarded write is information, not a failed call: res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	notes := strings.Join(out.Notes, " ")
	if !strings.Contains(notes, "column_id did not change") {
		t.Errorf("out.Notes must say the column did not move: %q missing from %v", "column_id did not change", out.Notes)
	}
	if !strings.Contains(notes, "list_position") {
		t.Errorf("out.Notes must name the fix: %q missing from %v", "list_position", out.Notes)
	}
	if !strings.Contains(serializedResponseString(t, res), "column_id did not change") {
		t.Error("the verdict must reach the readable half of the result too")
	}
}

// TestMCP_UpdateCard_ColumnMove_ReportsVerified is the other outcome:
// silence would be indistinguishable from "not checked".
func TestMCP_UpdateCard_ColumnMove_ReportsVerified(t *testing.T) {
	t.Parallel()

	cs := connectInMemoryWith(t, placementFixture(t, "col-2", nil))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":       "ci-1",
			"column_id":     "col-2",
			"list_position": 0,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !strings.Contains(strings.Join(out.Notes, " "), "verified by reading the card back") {
		t.Errorf("out.Notes must record that the read happened: %v", out.Notes)
	}
}

// TestMCP_UpdateCard_NoPlacementChange_DoesNotRead is the no-op case.
// A guard that is wrong in the negative direction is invisible: the
// read would happen on every write, which is what a working
// verification also looks like.
func TestMCP_UpdateCard_NoPlacementChange_DoesNotRead(t *testing.T) {
	t.Parallel()

	var gets atomic.Int32
	cs := connectInMemoryWith(t, placementFixture(t, "col-1", &gets))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateCardToolName,
		Arguments: map[string]any{"card_id": "ci-1", "name": "renamed"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
	if got := gets.Load(); got != 0 {
		t.Errorf("a write that changes no placement has nothing to verify: gets = %v, want %v", got, 0)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if len(out.Notes) != 0 {
		t.Errorf("out.Notes = %v, want empty", out.Notes)
	}
}

func TestMCP_UpdateCard_SkipVerify_DoesNotRead(t *testing.T) {
	t.Parallel()

	var gets atomic.Int32
	cs := connectInMemoryWith(t, placementFixture(t, "col-1", &gets))
	if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":     "ci-1",
			"column_id":   "col-2",
			"skip_verify": true,
		},
	}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := gets.Load(); got != 0 {
		t.Errorf("skip_verify must drop the read: gets = %v, want %v", got, 0)
	}
}

// TestMCP_UpdateCard_VerifyReadFails_ReportsRatherThanErrors: the
// write already happened, so turning a successful write into a failed
// call would be a worse answer than saying it could not be checked.
func TestMCP_UpdateCard_VerifyReadFails_ReportsRatherThanErrors(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","columnId":"col-2"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateCardToolName,
		Arguments: map[string]any{"card_id": "ci-1", "column_id": "col-2"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !strings.Contains(strings.Join(out.Notes, " "), "unconfirmed") {
		t.Errorf("out.Notes must say the write is unconfirmed: %v", out.Notes)
	}
}

// TestMCP_MoveCard_DiscardedColumnMove_Reports: favro_move_card runs
// the same PUT, and carries the same caution in its schema.
func TestMCP_MoveCard_DiscardedColumnMove_Reports(t *testing.T) {
	t.Parallel()

	cs := connectInMemoryWith(t, placementFixture(t, "col-1", nil))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      moveCardToolName,
		Arguments: map[string]any{"card_id": "ci-1", "column_id": "col-2"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !strings.Contains(strings.Join(out.Notes, " "), "column_id did not change") {
		t.Errorf("out.Notes must say the column did not move: %v", out.Notes)
	}
}

// TestMCP_UpdateCard_DryRun_DoesNotVerify: nothing was written, so
// there is nothing to read back.
func TestMCP_UpdateCard_DryRun_DoesNotVerify(t *testing.T) {
	t.Parallel()

	var gets atomic.Int32
	cs := connectInMemoryWith(t, placementFixture(t, "col-1", &gets))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"column_id": "col-2",
			"dry_run":   true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if got := gets.Load(); got != 0 {
		t.Errorf("dry-run wrote nothing, so it verifies nothing: gets = %v, want %v", got, 0)
	}
	if len(out.Notes) != 0 {
		t.Errorf("out.Notes = %v, want empty", out.Notes)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !out.Result.IsArchived {
		t.Error("out.Result.IsArchived = false, want true")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if out.Result.IsArchived {
		t.Error("out.Result.IsArchived = true, want false")
	}
}

func TestMCP_UnarchiveCard_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, unarchiveCardToolName, "card_id")
}

func TestMCP_MoveCard_HappyPath(t *testing.T) {
	t.Parallel()

	// The move is a PUT and the verification that follows it is a GET;
	// anything else is the tool doing something this test did not ask
	// for.
	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			puts.Add(1)
		case http.MethodGet:
		default:
			t.Errorf("move_card must PUT, then GET to verify; got %s", r.Method)
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	if got := puts.Load(); got != 1 {
		t.Errorf("puts.Load() = %v, want %v", got, 1)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if got := out.Result.ColumnID; got != "col-2" {
		t.Errorf("out.Result.ColumnID = %v, want %v", got, "col-2")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if !strings.Contains(out.PredictedStateDiff, "w-2") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "w-2")
	}
	if !strings.Contains(out.PredictedStateDiff, "col-2") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "col-2")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("fully-empty move must surface as a tool error")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("must short-circuit before any Favro call: got %v, want %v", got, 0)
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.DeleteCardResponse]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	if got := *out.Result; !reflect.DeepEqual(got, (favro.DeleteCardResponse{"ci-1"})) {
		t.Errorf("*out.Result = %v, want %v", got, favro.DeleteCardResponse{"ci-1"})
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.DeleteCardResponse]](t, res)
	if len(*out.Result) != 2 {
		t.Fatalf("len(*out.Result) = %d, want 2", len(*out.Result))
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.DeleteCardResponse]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if !strings.Contains(out.PredictedStateDiff, "EVERY widget") {
		t.Errorf("everywhere=true dry-run must announce the cross-widget purge loudly: %q missing", "EVERY widget")
	}
	if !strings.Contains(out.WouldCall.URL, "everywhere=true") {
		t.Errorf("out.WouldCall.URL does not contain %q", "everywhere=true")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
}

func TestMCP_DeleteCard_MissingCardID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteCardToolName, "card_id")
}
