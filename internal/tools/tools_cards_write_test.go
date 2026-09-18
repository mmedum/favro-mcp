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
			var sent favro.UpdateCardRequest
			if err := json.Unmarshal(b, &sent); err != nil {
				t.Errorf("json.Unmarshal(PUT body, &sent): %v", err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			// Favro echoes a null parent on a structural write
			// whether or not the card is still nested, which is
			// exactly why the caller cannot tell from the answer.
			// The column it does echo, and UpdateCard refuses a
			// write whose answer carries a different one, so the
			// double has to echo the one it was sent.
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:         "ci-1",
				CardCommonID:   "cc-1",
				Name:           "nested",
				WidgetCommonID: "w-1",
				ColumnID:       sent.ColumnID,
			})
		}
	}))
}

// TestMCP_StructuralWrite_ParentGuard is the guard's truth table. Favro
// treats a write carrying widgetCommonId as structural and rebuilds the
// card's place from the body alone, so a body naming no parent detaches
// a nested card — and the 200 looks identical either way.
//
// Every row asserts all three observable effects: how many reads the
// guard cost, what parent the body ended up naming, and what the caller
// was told. The rows that cost no read matter as much as the rest: a
// guard that is wrong in the negative direction reads on every write,
// which looks exactly like one that works.
func TestMCP_StructuralWrite_ParentGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		// currentParent is what GET /cards/{id} reports the card's
		// parent to be before the write.
		currentParent string
		args          map[string]any
		wantGets      int32
		// wantBodyParent is the parentCardId the PUT body must name;
		// empty means the body must name none.
		wantBodyParent string
		// wantNote is a substring of the note the caller must get;
		// empty means the call must produce no notes at all.
		wantNote string
	}{
		{
			name: "a column nudge carries the parent through",
			tool: updateCardToolName, currentParent: "ci-parent",
			args:     map[string]any{"card_id": "ci-1", "widget_common_id": "w-1", "column_id": "col-2", "list_position": 0},
			wantGets: 1, wantBodyParent: "ci-parent", wantNote: "clear_parent",
		},
		{
			name: "an explicit detach is not second-guessed, and costs no read",
			tool: updateCardToolName, currentParent: "ci-parent",
			args:     map[string]any{"card_id": "ci-1", "widget_common_id": "w-1", "clear_parent": true},
			wantGets: 0, wantBodyParent: "", wantNote: "",
		},
		{
			name: "a top-level card has nothing to carry",
			tool: updateCardToolName, currentParent: "",
			args:     map[string]any{"card_id": "ci-1", "widget_common_id": "w-1", "name": "renamed"},
			wantGets: 1, wantBodyParent: "", wantNote: "",
		},
		{
			name: "a non-structural write does not read at all",
			tool: updateCardToolName, currentParent: "ci-parent",
			args:     map[string]any{"card_id": "ci-1", "name": "renamed"},
			wantGets: 0, wantBodyParent: "", wantNote: "",
		},
		{
			name: "clear_parent on a write Favro will not act on says so",
			tool: updateCardToolName, currentParent: "ci-parent",
			args:     map[string]any{"card_id": "ci-1", "name": "renamed", "clear_parent": true},
			wantGets: 0, wantBodyParent: "", wantNote: "clear_parent had no effect",
		},
		{
			name: "a parent does not travel to another board",
			tool: updateCardToolName, currentParent: "ci-parent",
			args:     map[string]any{"card_id": "ci-1", "widget_common_id": "w-OTHER", "column_id": "col-on-other-board", "list_position": 0},
			wantGets: 1, wantBodyParent: "", wantNote: "different board",
		},
		{
			name: "a move carries the parent too — it is the same structural body",
			tool: moveCardToolName, currentParent: "ci-parent",
			args:     map[string]any{"card_id": "ci-1", "widget_common_id": "w-1", "column_id": "col-2"},
			wantGets: 1, wantBodyParent: "ci-parent", wantNote: "clear_parent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var put string
			var gets atomic.Int32
			cs := connectInMemoryWith(t, structuralUpdateFixture(t, tt.currentParent, &put, &gets))
			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tt.tool, Arguments: tt.args})
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if res.IsError {
				t.Fatalf("res.IsError = true, want false: %v", res.Content[0])
			}
			if got := gets.Load(); got != tt.wantGets {
				t.Errorf("gets.Load() = %v, want %v", got, tt.wantGets)
			}

			var body favro.UpdateCardRequest
			if err := json.Unmarshal([]byte(put), &body); err != nil {
				t.Fatalf("json.Unmarshal(put, &body): %v", err)
			}
			if got := body.ParentCardID; got != tt.wantBodyParent {
				t.Errorf("body.ParentCardID = %q, want %q", got, tt.wantBodyParent)
			}

			out := decodeStructured[writeOutput[favro.Card]](t, res)
			notes := strings.Join(out.Notes, " ")
			switch {
			case tt.wantNote == "" && len(out.Notes) != 0:
				t.Errorf("nothing was done beyond the request, so nothing is worth reporting: out.Notes = %v", out.Notes)
			case tt.wantNote != "" && !strings.Contains(notes, tt.wantNote):
				t.Errorf("out.Notes must contain %q: %v", tt.wantNote, out.Notes)
			}
		})
	}
}

// TestMCP_UpdateCard_ClearParentWithParentID_Refuses covers the only
// input pair in the tool that asks for two opposite things. Guessing
// which half was meant is how a detach becomes a re-parent.
func TestMCP_UpdateCard_ClearParentWithParentID_Refuses(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	cs := connectInMemoryWith(t, favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
	})))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
			"clear_parent":     true,
			"parent_card_id":   "ci-other",
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Fatal("res.IsError = false, want true")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	requireDeclaredClassPrefix(t, text)
	if !strings.HasPrefix(text, "[invalid] ") {
		t.Errorf("two mutually exclusive arguments are [invalid]: got %q", text)
	}
	if got := puts.Load(); got != 0 {
		t.Errorf("a refused request must not be written: puts = %v, want %v", got, 0)
	}
}

// TestMCP_UpdateCard_FailedWrite_KeepsRequestNote pins that a note
// computed before the write survives the write failing. The note says
// the arguments cannot do what was asked; dropping it is exactly when
// the caller retries the same ineffective call.
func TestMCP_UpdateCard_FailedWrite_KeepsRequestNote(t *testing.T) {
	t.Parallel()

	cs := connectInMemoryWith(t, favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	})))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCardToolName,
		Arguments: map[string]any{
			"card_id":      "ci-1",
			"name":         "renamed",
			"clear_parent": true,
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Fatal("res.IsError = false, want true")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	requireDeclaredClassPrefix(t, text)
	if !strings.Contains(text, "clear_parent had no effect") {
		t.Errorf("the request-level note must survive a failed write: %q", text)
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

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			// The parent read settleParent does before a structural
			// write; a move is always structural.
			_, _ = w.Write([]byte(`{"cardId":"ci-1","widgetCommonId":"w-1","columnId":"col-1"}`))
			return
		}
		if r.Method != http.MethodPut {
			t.Errorf("move_card must PUT; got %s", r.Method)
		}
		puts.Add(1)
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

	var puts, gets atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			puts.Add(1)
			return
		}
		gets.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1","widgetCommonId":"w-2"}`))
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
	if got := puts.Load(); got != 0 {
		t.Errorf("dry-run must never write: puts.Load() = %v, want %v", got, 0)
	}
	// Bounded, not merely non-zero: an unbounded read is how a guard
	// that fires per retry or per field goes unnoticed.
	if got := gets.Load(); got != 1 {
		t.Errorf("dry-run reads the parent exactly once: gets.Load() = %v, want %v", got, 1)
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

// TestStructuralCardWrites_RunTheParentGuard is the rule the guard
// exists to hold, applied to every tool that can break it rather than
// to the two that were remembered.
//
// Nothing here is a list typed out by hand. The candidates come from
// the registry, and which of them is a structural card write is decided
// by what each one actually PUTs: a body carrying widgetCommonId is the
// body Favro re-seats a card from, whatever tool sent it. A third such
// tool registered later fails here on the day it is added — which is
// the failure mode the first version of this change had, guarding
// favro_update_card and leaving favro_move_card, the tool update_card's
// own description recommends, sending the same body unguarded.
//
// The floor matters as much as the assertion. A filter that stopped
// matching would pass in silence, which is the sentence a filter that
// checked everything also prints.
func TestStructuralCardWrites_RunTheParentGuard(t *testing.T) {
	t.Parallel()

	var structural []string
	for _, name := range cardAddressingTools(t) {
		body, gets := driveStructuralWrite(t, name)
		if body.WidgetCommonID == "" {
			continue // not a card write, or not a structural one
		}
		structural = append(structural, name)

		if body.ParentCardID != "ci-parent" {
			t.Errorf("%s sent a structural body naming no parent, which detaches a nested card: parentCardId = %q, want %q", name, body.ParentCardID, "ci-parent")
		}
		if gets != 1 {
			t.Errorf("%s must read the card's parent before a structural write: gets = %v, want %v", name, gets, 1)
		}
	}

	if len(structural) < 2 {
		t.Fatalf("observed %d structural card writes (%v), want at least %d — the derivation stopped finding them, which passes for the wrong reason", len(structural), structural, 2)
	}
}

// driveStructuralWrite calls one tool with the arguments that make a
// card write structural, and reports the card body it PUT and how many
// reads it cost. A tool that PUTs no card body reports a zero body,
// which is how a non-card-write drops out of the rule above.
//
// The call's own outcome is deliberately not asserted: a tool that
// errors on this fixture is one the rule does not apply to, and the
// floor is what notices if a tool the rule DOES apply to starts
// erroring out of it.
func driveStructuralWrite(t *testing.T, name string) (favro.UpdateCardRequest, int32) {
	t.Helper()

	var put string
	var gets atomic.Int32
	cs := connectInMemoryWith(t, structuralUpdateFixture(t, "ci-parent", &put, &gets))
	if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: name,
		Arguments: map[string]any{
			"card_id":          "ci-1",
			"widget_common_id": "w-1",
			"column_id":        "col-2",
		},
	}); err != nil {
		t.Fatalf("cs.CallTool(%s): %v", name, err)
	}

	var body favro.UpdateCardRequest
	if put != "" {
		if err := json.Unmarshal([]byte(put), &body); err != nil {
			t.Fatalf("json.Unmarshal(%s PUT body, &body): %v", name, err)
		}
	}
	return body, gets.Load()
}

// cardAddressingTools derives, from the live registry, every mutating
// tool that takes both a card_id to address an existing card and a
// widget_common_id to name a board. That is the widest set that could
// send a structural card write; which of them does is settled by
// driving them, not by reading their schemas.
func cardAddressingTools(t *testing.T) []string {
	t.Helper()

	cs := connectInMemoryWith(t, favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
	listed, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("cs.ListTools: %v", err)
	}

	var names []string
	for _, tool := range listed.Tools {
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("json.Marshal(%s input schema): %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("json.Unmarshal(%s input schema): %v", tool.Name, err)
		}
		_, addressesCard := schema.Properties["card_id"]
		_, namesBoard := schema.Properties["widget_common_id"]
		if addressesCard && namesBoard {
			names = append(names, tool.Name)
		}
	}
	return names
}
