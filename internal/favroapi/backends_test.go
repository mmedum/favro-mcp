package favroapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
	"github.com/mmedum/favro-mcp/v4/internal/render"
)

// The cache holds the newest listings and forgets the oldest first, so a
// long session cannot grow it without bound.
func TestBackendIDsForgetsTheOldestListing(t *testing.T) {
	t.Parallel()
	var b backendIDs
	for i := range backendIDsCap + 1 {
		b.put(fmt.Sprintf("req-%d", i), fmt.Sprintf("backend-%d", i))
	}
	if got := b.get("req-0"); got != "" {
		t.Errorf("the oldest listing is still held: %q", got)
	}
	newest := fmt.Sprintf("req-%d", backendIDsCap)
	if got := b.get(newest); got != fmt.Sprintf("backend-%d", backendIDsCap) {
		t.Errorf("get(%s) = %q, want the newest backend", newest, got)
	}
	// A listing read again is not counted twice, and is kept over newer
	// ones: it is still being paged.
	b.put("req-1", "backend-again")
	b.put("req-new", "backend-new")
	if len(b.order) != backendIDsCap || b.get("req-1") != "backend-again" || b.get("req-2") != "" {
		t.Errorf("after a repeat put: %d held, req-1 = %q, req-2 = %q", len(b.order), b.get("req-1"), b.get("req-2"))
	}
}

// A page size outside 1 to 100 is refused before anything is sent:
// Favro answers a negative one with 100 and caps a larger one, neither of
// which is what was asked.
func TestFetchOneRefusesALimitOutOfRange(t *testing.T) {
	t.Parallel()
	c := &Client{BaseURL: "http://127.0.0.1:1"}
	for _, limit := range []int{-1, 101} {
		_, err := c.ListUsers(context.Background(), favro.Page{Limit: limit})
		var classed interface{ ErrorClass() render.Class }
		if !errors.As(err, &classed) || classed.ErrorClass() != render.ClassInvalid {
			t.Errorf("limit %d: err = %v, want an [invalid] refusal", limit, err)
		}
	}
}
