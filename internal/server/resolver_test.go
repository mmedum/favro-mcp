package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// resolverFixture wires a Resolver to an httptest server backed by
// the supplied handler. Returns the resolver and a *atomic.Int32
// counter the caller can read to confirm cache hits avoided HTTP.
func resolverFixture(t *testing.T, pages [][]favro.Tag) (*Resolver, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	totalPages := len(pages)
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// page query param is 1-indexed; absent on the first request.
		// Using t.Errorf rather than require.Equal because we're inside
		// an httptest handler goroutine; require.Fail panics, which a
		// goroutine-bound panic would lose.
		page := 0
		if pageStr := r.URL.Query().Get("page"); pageStr != "" {
			if pageStr != "1" {
				t.Errorf("test fixture only supports page 0 + page 1, got page=%q", pageStr)
			}
			page = 1
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Tag]{
			Page:      page,
			Pages:     totalPages,
			RequestID: "req-tags",
			Entities:  pages[page],
		})
	}))
	return NewResolver(c), &calls
}

func TestResolveTag_CacheMissThenHit(t *testing.T) {
	t.Parallel()

	r, calls := resolverFixture(t, [][]favro.Tag{
		{
			{TagID: "t-1", Name: "frontend", Color: "blue"},
			{TagID: "t-2", Name: "Backend"},
		},
	})

	// First call hits Favro and populates the cache.
	got, cached, err := r.ResolveTag(context.Background(), "front", 0, false)
	require.NoError(t, err)
	require.False(t, cached, "first call must miss the cache")
	require.Len(t, got, 1)
	require.Equal(t, "t-1", got[0].TagID)
	require.InDelta(t, 0.7, got[0].Score, 0.001)
	require.EqualValues(t, 1, calls.Load(), "exactly one HTTP call on cold cache")

	// Second call returns from cache.
	got2, cached2, err := r.ResolveTag(context.Background(), "front", 0, false)
	require.NoError(t, err)
	require.True(t, cached2, "second call must hit the cache")
	require.Equal(t, got, got2)
	require.EqualValues(t, 1, calls.Load(), "no extra HTTP call on warm cache")
}

func TestResolveTag_ForceRefreshBypassesCache(t *testing.T) {
	t.Parallel()

	r, calls := resolverFixture(t, [][]favro.Tag{
		{{TagID: "t-1", Name: "blocker"}},
	})

	_, _, err := r.ResolveTag(context.Background(), "blocker", 0, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())

	// force_refresh=true should re-fetch even though the cache is warm.
	_, cached, err := r.ResolveTag(context.Background(), "blocker", 0, true)
	require.NoError(t, err)
	require.False(t, cached, "force_refresh must bypass cache and report uncached")
	require.EqualValues(t, 2, calls.Load(), "force_refresh must trigger a second HTTP call")
}

func TestResolveTag_InvalidateCache(t *testing.T) {
	t.Parallel()

	r, calls := resolverFixture(t, [][]favro.Tag{
		{{TagID: "t-1", Name: "frontend"}},
	})

	_, _, err := r.ResolveTag(context.Background(), "front", 0, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())

	r.invalidateTagCache()

	_, cached, err := r.ResolveTag(context.Background(), "front", 0, false)
	require.NoError(t, err)
	require.False(t, cached, "cache must miss after invalidation")
	require.EqualValues(t, 2, calls.Load())
}

func TestResolveTag_TTLExpiryRefetches(t *testing.T) {
	t.Parallel()

	r, calls := resolverFixture(t, [][]favro.Tag{
		{{TagID: "t-1", Name: "frontend"}},
	})

	// Freeze the clock so the TTL is deterministic.
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	r.tagCache.Now = func() time.Time { return now }

	_, _, err := r.ResolveTag(context.Background(), "front", 0, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())

	// Advance past the TTL.
	now = now.Add(tagCacheTTL + time.Second)

	_, cached, err := r.ResolveTag(context.Background(), "front", 0, false)
	require.NoError(t, err)
	require.False(t, cached, "expired entry must miss the cache")
	require.EqualValues(t, 2, calls.Load())
}

func TestResolveTag_FetchesAllPages(t *testing.T) {
	t.Parallel()

	r, calls := resolverFixture(t, [][]favro.Tag{
		{{TagID: "t-1", Name: "frontend"}},
		{{TagID: "t-2", Name: "frontier"}},
	})

	got, _, err := r.ResolveTag(context.Background(), "front", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 2, "both pages must be merged into the cache")
	require.EqualValues(t, 2, calls.Load(), "must fetch every page on cold cache")
}

func TestResolveTag_RankingScoreScale(t *testing.T) {
	t.Parallel()

	r, _ := resolverFixture(t, [][]favro.Tag{
		{
			{TagID: "exact", Name: "front"},
			{TagID: "prefix", Name: "frontend"},
			{TagID: "substring", Name: "the front line"},
			{TagID: "nomatch", Name: "backend"},
		},
	})

	got, _, err := r.ResolveTag(context.Background(), "front", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 3, "no-match entries must be filtered out")

	// Ordered by score descending: exact (1.0) > prefix (0.7) > substring (0.4)
	require.Equal(t, "exact", got[0].TagID)
	require.InDelta(t, 1.0, got[0].Score, 0.001)
	require.Equal(t, "prefix", got[1].TagID)
	require.InDelta(t, 0.7, got[1].Score, 0.001)
	require.Equal(t, "substring", got[2].TagID)
	require.InDelta(t, 0.4, got[2].Score, 0.001)
}

func TestResolveTag_TieBreakerByName(t *testing.T) {
	t.Parallel()

	r, _ := resolverFixture(t, [][]favro.Tag{
		{
			{TagID: "t-zeta", Name: "zeta-prefix-extra"},
			{TagID: "t-alpha", Name: "alpha-prefix-extra"},
			{TagID: "t-mid", Name: "mid-prefix-extra"},
		},
	})

	// All three contain "prefix" as a substring (score 0.4); tie-break
	// must be by name ascending so the order is deterministic.
	got, _, err := r.ResolveTag(context.Background(), "prefix", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "t-alpha", got[0].TagID)
	require.Equal(t, "t-mid", got[1].TagID)
	require.Equal(t, "t-zeta", got[2].TagID)
}

func TestResolveTag_LimitDefaultAndCap(t *testing.T) {
	t.Parallel()

	// Build 60 matching tags.
	tags := make([]favro.Tag, 60)
	for i := range tags {
		tags[i] = favro.Tag{TagID: "t-x", Name: "match-x"}
	}
	r, _ := resolverFixture(t, [][]favro.Tag{tags})

	got, _, err := r.ResolveTag(context.Background(), "match", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 10, "limit <= 0 must use default of 10")

	got, _, err = r.ResolveTag(context.Background(), "match", 9999, false)
	require.NoError(t, err)
	require.Len(t, got, 50, "limit > 50 must be capped at 50")

	got, _, err = r.ResolveTag(context.Background(), "match", 3, false)
	require.NoError(t, err)
	require.Len(t, got, 3)
}

func TestResolveTag_EmptyQueryReturnsNoMatches(t *testing.T) {
	t.Parallel()

	r, _ := resolverFixture(t, [][]favro.Tag{
		{{TagID: "t-1", Name: "anything"}},
	})

	got, _, err := r.ResolveTag(context.Background(), "", 0, false)
	require.NoError(t, err)
	require.Empty(t, got, "empty query is not a wildcard — must return no matches")
}
