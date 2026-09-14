package tools

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/service"
)

func TestMCP_GetCardFull_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:         "c-1",
				CardCommonID:   "cc-1",
				Name:           "Print visitor passes",
				WidgetCommonID: "w-1",
				ColumnID:       "col-2",
				Tags:           []string{"tag-1"},
			})
		case r.URL.Path == "/widgets":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{
				Pages: 1,
				Entities: []favro.Widget{
					{WidgetCommonID: "w-1", Name: "Sprint Board", CollectionIDs: []string{"col-A"}},
				},
			})
		case r.URL.Path == "/columns":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
				Pages: 1,
				Entities: []favro.Column{
					{ColumnID: "col-2", WidgetCommonID: "w-1", Name: "Done"},
				},
			})
		case r.URL.Path == "/collections":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Collection]{
				Pages:    1,
				Entities: []favro.Collection{{CollectionID: "col-A", Name: "Engineering"}},
			})
		case r.URL.Path == "/tags":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: []favro.Tag{{TagID: "tag-1", Name: "frontend", Color: "blue"}},
			})
		default:
			t.Errorf("unexpected fixture path: %s", r.URL.Path)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      getCardFullToolName,
		Arguments: map[string]any{"card_id": "c-1"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool must succeed; got %s", serializedResponseString(t, res))
	}

	out := decodeStructured[service.FullCard](t, res)
	if got := out.Name; got != "Print visitor passes" {
		t.Errorf("out.Name = %v, want %v", got, "Print visitor passes")
	}
	if got := out.WidgetName; got != "Sprint Board" {
		t.Errorf("out.WidgetName = %v, want %v", got, "Sprint Board")
	}
	if got := out.ColumnName; got != "Done" {
		t.Errorf("out.ColumnName = %v, want %v", got, "Done")
	}
	if got := out.CollectionNames; !reflect.DeepEqual(got, ([]string{"Engineering"})) {
		t.Errorf("out.CollectionNames = %v, want %v", got, []string{"Engineering"})
	}
	if len(out.ResolvedTags) != 1 {
		t.Fatalf("len(out.ResolvedTags) = %d, want 1", len(out.ResolvedTags))
	}
	if got := out.ResolvedTags[0].Name; got != "frontend" {
		t.Errorf("out.ResolvedTags[0].Name = %v, want %v", got, "frontend")
	}
}

// TestMCP_GetCardFull_IdentityRequired pins that the SDK-layer
// rejects calls with no identity field via the typed error from
// the resolver — distinct from a missing-field schema error
// because the schema treats every identity field as optional
// (the "exactly one of N" rule lives in the handler).
func TestMCP_GetCardFull_IdentityRequired(t *testing.T) {
	t.Parallel()

	calls := 0
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      getCardFullToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("missing identity must surface as a tool error")
	}
	if got := calls; got != 0 {
		t.Errorf("missing identity must short-circuit before any Favro call: got %v, want %v", got, 0)
	}

	full := strings.ToLower(serializedResponseString(t, res))
	if !strings.Contains(full, "card_id") {
		t.Errorf("full does not contain %q", "card_id")
	}
	if !strings.Contains(full, "card_common_id") {
		t.Errorf("full does not contain %q", "card_common_id")
	}
	if !strings.Contains(full, "sequential_id") {
		t.Errorf("full does not contain %q", "sequential_id")
	}
}
