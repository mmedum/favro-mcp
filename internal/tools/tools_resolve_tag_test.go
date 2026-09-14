package tools

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/service"
)

// scoreEpsilon is the comparison tolerance for resolver scores, which
// are floats. internal/service has its own copy for its own
// assertions; a shared one would mean a test-only export.
const scoreEpsilon = 0.001

func TestMCP_ResolveTag_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
			Page:  0,
			Pages: 1,
			Entities: []favro.Tag{
				{TagID: "t-1", Name: "frontend", Color: "blue"},
				{TagID: "t-2", Name: "backend"},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: resolveTagToolName,
		Arguments: map[string]any{
			"name": "front",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[resolveOutput[service.ResolvedTag]](t, res)
	if len(out.Candidates) != 1 {
		t.Fatalf("len(out.Candidates) = %d, want 1", len(out.Candidates))
	}
	if got := out.Candidates[0].TagID; got != "t-1" {
		t.Errorf("out.Candidates[0].TagID = %v, want %v", got, "t-1")
	}
	if got := out.Candidates[0].Name; got != "frontend" {
		t.Errorf("out.Candidates[0].Name = %v, want %v", got, "frontend")
	}
	if got := out.Candidates[0].Color; got != "blue" {
		t.Errorf("out.Candidates[0].Color = %v, want %v", got, "blue")
	}
	if math.Abs(out.Candidates[0].Score-0.7) > scoreEpsilon {
		t.Errorf("out.Candidates[0].Score = %v, want %v within scoreEpsilon", out.Candidates[0].Score, 0.7)
	}
	if out.Cached {
		t.Error("first call must report uncached")
	}
}

func TestMCP_ResolveTag_NoMatchReturnsEmptyList(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
			Page:     0,
			Pages:    1,
			Entities: []favro.Tag{{TagID: "t-1", Name: "frontend"}},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: resolveTagToolName,
		Arguments: map[string]any{
			"name": "nomatch",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("no match must NOT surface as a tool error — empty candidate list is the contract")
	}

	out := decodeStructured[resolveOutput[service.ResolvedTag]](t, res)
	if len(out.Candidates) != 0 {
		t.Errorf("out.Candidates = %v, want empty", out.Candidates)
	}
}
