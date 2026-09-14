package tools

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/favroapi"
)

// tagFixture wires a server that:
//   - GET /tags → returns the supplied tag list
//   - PUT /cards/{id} → echoes a Card back
//
// Used by every test that exercises favro_add_tag_to_card and
// favro_remove_tag_from_card.
func tagFixture(t *testing.T, tags []favro.Tag) *favroapi.Client {
	t.Helper()
	return favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: tags,
			})
		case http.MethodPut:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"x"}`))
		}
	}))
}

func TestMCP_AddTagToCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := tagFixture(t, []favro.Tag{{TagID: "t-1", Name: "Frontend"}})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontend",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
}

func TestMCP_AddTagToCard_CaseInsensitive(t *testing.T) {
	t.Parallel()

	c := tagFixture(t, []favro.Tag{{TagID: "t-1", Name: "Frontend"}})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "frontend",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
}

// TestMCP_AddTagToCard_HardFailUnknown pins the typo-prevention
// contract: an unknown name surfaces as an error pointing at
// favro_create_tag, never as an auto-create or silent success.
func TestMCP_AddTagToCard_HardFailUnknown(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: []favro.Tag{{TagID: "t-1", Name: "Frontend"}},
			})
		case http.MethodPut:
			puts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontned",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("unknown tag name must surface as a tool error")
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "favro_create_tag") {
		t.Errorf("error must point the LLM at favro_create_tag explicitly: %q missing", "favro_create_tag")
	}
	if got := puts.Load(); got != 0 {
		t.Errorf("no PUT must be issued for an unknown tag: got %v, want %v", got, 0)
	}
}

func TestMCP_AddTagToCard_AmbiguousNames(t *testing.T) {
	t.Parallel()

	c := tagFixture(t, []favro.Tag{
		{TagID: "t-1", Name: "Frontend"},
		{TagID: "t-2", Name: "Frontend"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontend",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "multiple") {
		t.Errorf("strings.ToLower(serializedResponseString(t, res)) does not contain %q", "multiple")
	}
}

func TestMCP_AddTagToCard_DryRun(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
				Pages:    1,
				Entities: []favro.Tag{{TagID: "t-1", Name: "Frontend"}},
			})
		case http.MethodPut:
			puts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: addTagToCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontend",
			"dry_run":  true,
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
		t.Errorf("puts.Load() = %v, want %v", got, 0)
	}
}

func TestMCP_RemoveTagFromCard_HappyPath(t *testing.T) {
	t.Parallel()

	c := tagFixture(t, []favro.Tag{{TagID: "t-1", Name: "Frontend"}})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: removeTagFromCardToolName,
		Arguments: map[string]any{
			"card_id":  "ci-1",
			"tag_name": "Frontend",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
}

func TestMCP_AddTagToCard_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []string{"card_id", "tag_name"}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, addTagToCardToolName, field)
		})
	}
}
