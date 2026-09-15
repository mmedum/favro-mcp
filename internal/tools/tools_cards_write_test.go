package tools

import (
	"encoding/json"
	"io"
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

// structuralUpdateFixture answers a GET /cards/{id} with a card that
// has parent, and records the PUT body. It is the shape every
// parent-preservation test needs: Favro re-seats a card on a write
// carrying widgetCommonId, and what the body says about parentCardId
// is the whole subject.
func structuralUpdateFixture(t *testing.T, parentCardID string, putBody *string, gets *atomic.Int32) *favroapi.Client {
	t.Helper()
	return favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if gets != nil {
				gets.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:         "ci-1",
				CardCommonID:   "cc-1",
				Name:           "nested",
				WidgetCommonID: "w-1",
				ParentCardID:   parentCardID,
			})
		case http.MethodPut:
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read PUT body: %v", err)
				return
			}
			if putBody != nil {
				*putBody = string(b)
			}
			w.Header().Set("Content-Type", "application/json")
			// Favro echoes a null parent on a structural write
			// whether or not the card is still nested, which is
			// exactly why the caller cannot tell from the answer.
			_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"nested","widgetCommonId":"w-1"}`))
		}
	}))
}

// TestMCP_UpdateCard_Structural_CarriesExistingParent is the
// regression for the orphaning in hard rule 2's territory: a
// column nudge on a nested card is a structural write, and a
// structural write naming no parent leaves the card at top level.
func TestMCP_UpdateCard_Structural_CarriesExistingParent(t *testing.T) {
	t.Parallel()

	var put string
	var gets atomic.Int32
	cs := connectInMemoryWith(t, structuralUpdateFixture(t, "ci-parent", &put, &gets))

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
			"column_id":        "col-2",
			"list_position":    0,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
	if got := gets.Load(); got != 1 {
		t.Errorf("a structural write must read the card back for its parent: gets = %v, want %v", got, 1)
	}

	var body favro.UpdateCardRequest
	if err := json.Unmarshal([]byte(put), &body); err != nil {
		t.Fatalf("json.Unmarshal(put, &body): %v", err)
	}
	if got := body.ParentCardID; got != "ci-parent" {
		t.Errorf("the structural write must carry the existing parent: body.ParentCardID = %q, want %q", got, "ci-parent")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if len(out.Notes) == 0 {
		t.Fatal("carrying the parent through is something the caller did not ask for; it must be reported in notes")
	}
	if !strings.Contains(strings.Join(out.Notes, " "), "clear_parent") {
		t.Errorf("the note must name the way to detach on purpose: %q missing", "clear_parent")
	}
}

// TestMCP_UpdateCard_Structural_ClearParentDetaches is the opposite
// direction: asking for the detach explicitly must not be second-
// guessed, and must not cost a read.
func TestMCP_UpdateCard_Structural_ClearParentDetaches(t *testing.T) {
	t.Parallel()

	var put string
	var gets atomic.Int32
	cs := connectInMemoryWith(t, structuralUpdateFixture(t, "ci-parent", &put, &gets))

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
			"clear_parent":     true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
	if got := gets.Load(); got != 0 {
		t.Errorf("an explicit detach needs no read: gets = %v, want %v", got, 0)
	}
	if strings.Contains(put, "parentCardId") {
		t.Errorf("clear_parent must leave parentCardId out of the body: %q present in %q", "parentCardId", put)
	}
}

// TestMCP_UpdateCard_Structural_TopLevelCardNeedsNoNote is the no-op
// case: a card with no parent has nothing to preserve, so the read
// happens and nothing else does.
func TestMCP_UpdateCard_Structural_TopLevelCardNeedsNoNote(t *testing.T) {
	t.Parallel()

	var put string
	cs := connectInMemoryWith(t, structuralUpdateFixture(t, "", &put, nil))

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
			"name":             "renamed",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
	if strings.Contains(put, "parentCardId") {
		t.Errorf("a top-level card has no parent to carry: %q present in %q", "parentCardId", put)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if len(out.Notes) != 0 {
		t.Errorf("nothing was preserved, so nothing is worth reporting: out.Notes = %v, want empty", out.Notes)
	}
}

// TestMCP_UpdateCard_NonStructural_DoesNotRead pins the other half of
// the guard: a write Favro does not treat as structural cannot orphan
// anything, so it must not pay for a read.
func TestMCP_UpdateCard_NonStructural_DoesNotRead(t *testing.T) {
	t.Parallel()

	var gets atomic.Int32
	cs := connectInMemoryWith(t, structuralUpdateFixture(t, "ci-parent", nil, &gets))

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
		t.Errorf("a non-structural write must not read the card: gets = %v, want %v", got, 0)
	}
}

// TestMCP_UpdateCard_ClearParentWithoutStructural_Reports covers the
// input that asks for something Favro will not do: clear_parent on a
// write carrying no widget_common_id changes nothing, and silence
// would read as "detached".
func TestMCP_UpdateCard_ClearParentWithoutStructural_Reports(t *testing.T) {
	t.Parallel()

	cs := connectInMemoryWith(t, structuralUpdateFixture(t, "ci-parent", nil, nil))

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":      "ci-1",
			"name":         "renamed",
			"clear_parent": true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !strings.Contains(strings.Join(out.Notes, " "), "clear_parent had no effect") {
		t.Errorf("out.Notes must say the flag did nothing: %q missing from %v", "clear_parent had no effect", out.Notes)
	}
}

// TestMCP_UpdateCard_Structural_ReadFailureRefuses: the read is what
// makes the write safe, so a write that cannot be made safe does not
// happen. Reporting beats refusing everywhere the tool knows what it
// is doing; here it does not.
func TestMCP_UpdateCard_Structural_ReadFailureRefuses(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
			"column_id":        "col-2",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	if got := puts.Load(); got != 0 {
		t.Errorf("the write must not happen when the parent cannot be read: puts = %v, want %v", got, 0)
	}
	if !strings.Contains(serializedResponseString(t, res), "parent") {
		t.Errorf("the LLM-visible error must name the problem: %q missing", "parent")
	}
}

// TestMCP_UpdateCard_Structural_DryRunPreviewsPreservedParent: a
// preview that omits the parent describes a body the tool would not
// send, so the read runs under dry-run too — the same choice the
// description-editor tools make.
func TestMCP_UpdateCard_Structural_DryRunPreviewsPreservedParent(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.Card{CardID: "ci-1", ParentCardID: "ci-parent"})
		case http.MethodPut:
			puts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
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
	if got := puts.Load(); got != 0 {
		t.Errorf("dry-run must not PUT: puts = %v, want %v", got, 0)
	}
	if !strings.Contains(out.PredictedStateDiff, "ci-parent") {
		t.Errorf("the preview must show the parent the body would carry: %q missing from %q", "ci-parent", out.PredictedStateDiff)
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
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
			"column_id":        "col-2",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if got := out.Result.ColumnID; got != "col-2" {
		t.Errorf("out.Result.ColumnID = %v, want %v", got, "col-2")
	}
}

// TestMCP_MoveCard_ColumnWithoutWidget is the eval's failure as a unit
// test: the model asked for a column move with the two ids the task
// gave it, and got a success. It is an error now, and it names the
// argument to add.
func TestMCP_MoveCard_ColumnWithoutWidget(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
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
	if !res.IsError {
		t.Fatal("res.IsError = false, want true")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	requireDeclaredClassPrefix(t, text)
	if !strings.HasPrefix(text, "[invalid] ") || !strings.Contains(text, "widgetCommonId") {
		t.Errorf("the LLM-visible error must be [invalid] and name widgetCommonId: got %q", text)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
}

// TestMCP_MoveCard_FavroIgnoredTheMove pins that a move Favro accepts
// and does not perform reaches the caller as an error. The body is the
// stub Favro answers with, recorded live 2026-09-15.
func TestMCP_MoveCard_FavroIgnoredTheMove(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"a card"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: moveCardToolName,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
			"column_id":        "col-2",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Fatal("res.IsError = false, want true")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	requireDeclaredClassPrefix(t, text)
	if !strings.HasPrefix(text, "[unavailable] ") {
		t.Errorf("the LLM-visible error must be [unavailable]: got %q", text)
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
