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

// scoreEpsilon is the comparison tolerance for resolver score
// assertions. Scores are produced by literal returns in
// scoreLowered (1.0 / 0.7 / 0.4) so they are exact in practice;
// the InDelta wrapping guards against future changes to the score
// scale that introduce arithmetic, and silences the testifylint
// `float-compare` rule on require.Equal.
const scoreEpsilon = 0.001

// resolverFixture wires a Resolver to an httptest server backed by
// a paginated handler over T. Returns the resolver and an
// *atomic.Int32 counter the caller can read to confirm cache hits
// avoided HTTP. Tests that only care about a single page can pass
// `[][]T{{...one page of items...}}`.
func resolverFixture[T any](t *testing.T, pages [][]T) (*Resolver, *atomic.Int32) {
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
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[T]{
			Page:      page,
			Pages:     totalPages,
			RequestID: "req-resolver",
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
	require.InDelta(t, 0.7, got[0].Score, scoreEpsilon)
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
	require.InDelta(t, 1.0, got[0].Score, scoreEpsilon)
	require.Equal(t, "prefix", got[1].TagID)
	require.InDelta(t, 0.7, got[1].Score, scoreEpsilon)
	require.Equal(t, "substring", got[2].TagID)
	require.InDelta(t, 0.4, got[2].Score, scoreEpsilon)
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

// ============================================================
// User resolver
// ============================================================

func TestResolveUser_HappyPath(t *testing.T) {
	t.Parallel()

	r, calls := resolverFixture(t, [][]favro.User{
		{
			{UserID: "u-1", Name: "Alice", Email: "alice@example.invalid"},
			{UserID: "u-2", Name: "Bob", Email: "bob@example.invalid"},
		},
	})

	got, cached, err := r.ResolveUser(context.Background(), "alic", 0, false)
	require.NoError(t, err)
	require.False(t, cached)
	require.Len(t, got, 1)
	require.Equal(t, "u-1", got[0].UserID)
	require.Equal(t, "alice@example.invalid", got[0].Email)
	require.InDelta(t, 0.7, got[0].Score, scoreEpsilon)
	require.EqualValues(t, 1, calls.Load())
}

// TestResolveUser_MatchesAgainstEmail pins the contract that an
// LLM asking for a user by email gets a hit even when the display
// name doesn't match. Email is the disambiguator humans actually
// use.
func TestResolveUser_MatchesAgainstEmail(t *testing.T) {
	t.Parallel()

	r, _ := resolverFixture(t, [][]favro.User{
		// u-1 only matches via email substring; the leading "j-" so
		// "engin" is a substring of the email rather than a prefix.
		{
			{UserID: "u-1", Name: "Display Only", Email: "j-engineering@example.invalid"},
			{UserID: "u-2", Name: "engineer", Email: "other@example.invalid"},
		},
	})

	// "engin" is a prefix of u-2's name "engineer" (0.7) and a
	// substring (NOT prefix) of u-1's email "j-engineering@..." (0.4).
	got, _, err := r.ResolveUser(context.Background(), "engin", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 2)
	// u-2 wins because its name has prefix match (0.7); u-1 only
	// substring-matches via email (0.4).
	require.Equal(t, "u-2", got[0].UserID)
	require.InDelta(t, 0.7, got[0].Score, scoreEpsilon)
	require.Equal(t, "u-1", got[1].UserID)
	require.InDelta(t, 0.4, got[1].Score, scoreEpsilon)
}

// ============================================================
// Collection resolver
// ============================================================

func TestResolveCollection_HappyPath(t *testing.T) {
	t.Parallel()

	r, calls := resolverFixture(t, [][]favro.Collection{
		{
			{CollectionID: "c-1", Name: "Documentation"},
			{CollectionID: "c-2", Name: "Design"},
		},
	})

	got, cached, err := r.ResolveCollection(context.Background(), "doc", 0, false)
	require.NoError(t, err)
	require.False(t, cached)
	require.Len(t, got, 1)
	require.Equal(t, "c-1", got[0].CollectionID)
	require.InDelta(t, 0.7, got[0].Score, scoreEpsilon)
	require.EqualValues(t, 1, calls.Load())
}

// ============================================================
// Widget resolver
// ============================================================

func TestResolveWidget_HappyPath(t *testing.T) {
	t.Parallel()

	r, _ := resolverFixture(t, [][]favro.Widget{
		{
			{WidgetCommonID: "w-1", Name: "Sprint Board", Type: "board", CollectionIDs: []string{"c-1"}},
			{WidgetCommonID: "w-2", Name: "Roadmap", Type: "backlog", CollectionIDs: []string{"c-1", "c-2"}},
		},
	})

	got, _, err := r.ResolveWidget(context.Background(), "sprint", "", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "w-1", got[0].WidgetCommonID)
	require.Equal(t, "board", got[0].Type)
	require.Equal(t, []string{"c-1"}, got[0].CollectionIDs)
}

// TestResolveWidget_FiltersByCollection pins the optional
// client-side collection filter — a widget that doesn't include
// the requested collectionId must drop out even if its name
// matches.
func TestResolveWidget_FiltersByCollection(t *testing.T) {
	t.Parallel()

	r, _ := resolverFixture(t, [][]favro.Widget{
		{
			{WidgetCommonID: "w-only-c1", Name: "Roadmap", CollectionIDs: []string{"c-1"}},
			{WidgetCommonID: "w-only-c2", Name: "Roadmap", CollectionIDs: []string{"c-2"}},
			{WidgetCommonID: "w-both", Name: "Roadmap", CollectionIDs: []string{"c-1", "c-2"}},
		},
	})

	got, _, err := r.ResolveWidget(context.Background(), "roadmap", "c-1", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 2, "only widgets containing c-1 in their CollectionIDs must match")
	ids := []string{got[0].WidgetCommonID, got[1].WidgetCommonID}
	require.ElementsMatch(t, []string{"w-only-c1", "w-both"}, ids)
}

// ============================================================
// Column resolver
// ============================================================

func TestResolveColumn_HappyPath(t *testing.T) {
	t.Parallel()

	r, calls := resolverFixture(t, [][]favro.Column{
		{
			{ColumnID: "col-1", WidgetCommonID: "w-1", Name: "To do", Position: 0},
			{ColumnID: "col-2", WidgetCommonID: "w-1", Name: "Doing", Position: 1},
			{ColumnID: "col-3", WidgetCommonID: "w-1", Name: "Done", Position: 2},
		},
	})

	got, _, err := r.ResolveColumn(context.Background(), "w-1", "do", 0, false)
	require.NoError(t, err)
	// "do" prefix-matches "Doing" (0.7) and "Done" (0.7); substring of
	// "To do" (0.4). Three candidates, prefix matches first.
	require.Len(t, got, 3)
	require.InDelta(t, 0.7, got[0].Score, scoreEpsilon)
	require.EqualValues(t, 1, calls.Load())
}

// TestResolveColumn_RequiresWidgetCommonID pins the contract that
// column resolution refuses an org-wide search — column names
// repeat across widgets and an unscoped resolver would always
// return ambiguous garbage.
func TestResolveColumn_RequiresWidgetCommonID(t *testing.T) {
	t.Parallel()

	r := NewResolver(favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("missing widget_common_id must short-circuit before any HTTP call")
	})))

	_, _, err := r.ResolveColumn(context.Background(), "", "doing", 0, false)
	require.Error(t, err)
	require.ErrorIs(t, err, errMissingResolveWidgetCommonID)
}

// TestResolveColumn_CacheKeyedPerWidget pins the contract that
// column caches are scoped per widget — switching widgetCommonID
// must NOT see the previous widget's columns.
func TestResolveColumn_CacheKeyedPerWidget(t *testing.T) {
	t.Parallel()

	// Hand-rolled fixture: respond differently based on the
	// widgetCommonId query param so we can verify the cache
	// doesn't bleed between widgets.
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		widget := r.URL.Query().Get("widgetCommonId")
		w.Header().Set("Content-Type", "application/json")
		switch widget {
		case "w-A":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
				Page: 0, Pages: 1,
				Entities: []favro.Column{
					{ColumnID: "col-A1", WidgetCommonID: "w-A", Name: "Match"},
				},
			})
		case "w-B":
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
				Page: 0, Pages: 1,
				Entities: []favro.Column{
					{ColumnID: "col-B1", WidgetCommonID: "w-B", Name: "Match"},
				},
			})
		default:
			t.Errorf("unexpected widget filter %q", widget)
		}
	}))
	r := NewResolver(c)

	gotA, _, err := r.ResolveColumn(context.Background(), "w-A", "match", 0, false)
	require.NoError(t, err)
	require.Len(t, gotA, 1)
	require.Equal(t, "col-A1", gotA[0].ColumnID)

	gotB, _, err := r.ResolveColumn(context.Background(), "w-B", "match", 0, false)
	require.NoError(t, err)
	require.Len(t, gotB, 1)
	require.Equal(t, "col-B1", gotB[0].ColumnID,
		"different widget must surface its own columns, not a previously-cached widget's")
}

// ============================================================
// Custom field resolver
// ============================================================

func TestResolveCustomField_HappyPath(t *testing.T) {
	t.Parallel()

	r, _ := resolverFixture(t, [][]favro.CustomField{
		{
			{CustomFieldID: "cf-1", Name: "Priority", Type: "Single select"},
			{CustomFieldID: "cf-2", Name: "Estimation", Type: "Rating"},
		},
	})

	got, _, err := r.ResolveCustomField(context.Background(), "prior", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "cf-1", got[0].CustomFieldID)
	require.Equal(t, "Single select", got[0].Type)
	require.InDelta(t, 0.7, got[0].Score, scoreEpsilon)
}

// ============================================================
// Group resolver
// ============================================================

func TestResolveGroup_HappyPath(t *testing.T) {
	t.Parallel()

	r, _ := resolverFixture(t, [][]favro.Group{
		{
			{GroupID: "g-1", Name: "Engineering"},
			{GroupID: "g-2", Name: "Sales"},
		},
	})

	got, _, err := r.ResolveGroup(context.Background(), "eng", 0, false)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "g-1", got[0].GroupID)
	require.InDelta(t, 0.7, got[0].Score, scoreEpsilon)
}

// TestInvalidateCacheHooks pins the contract for the 6 non-tag
// invalidate*Cache hooks. Each is reserved for Phase 6 mutating
// tools (favro_create_<x>, favro_update_<x>, favro_delete_<x>) to
// call after a successful write so the next resolve sees fresh
// data. They have no callers in this PR; the table here both
// satisfies the `unused` linter and pins the contract: after
// invalidate, the next call misses the cache and re-fetches.
//
// The tag invalidate is exercised by TestResolveTag_InvalidateCache
// (the original Phase 4.1 test).
func TestInvalidateCacheHooks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// run does the full miss → invalidate → re-miss dance and
		// returns the call count after the second resolve. Wrapping
		// each resource's quirks (different fixture types, different
		// resolver-method signatures) in a closure lets us share the
		// outer assertion shape.
		run func(t *testing.T) (callsAfterFirst, callsAfterSecond int)
	}{
		{
			name: "user",
			run: func(t *testing.T) (int, int) {
				r, calls := resolverFixture(t, [][]favro.User{{{UserID: "u-1", Name: "Alice"}}})
				_, _, err := r.ResolveUser(context.Background(), "alice", 0, false)
				require.NoError(t, err)
				first := int(calls.Load())
				r.invalidateUserCache()
				_, cached, err := r.ResolveUser(context.Background(), "alice", 0, false)
				require.NoError(t, err)
				require.False(t, cached)
				return first, int(calls.Load())
			},
		},
		{
			name: "collection",
			run: func(t *testing.T) (int, int) {
				r, calls := resolverFixture(t, [][]favro.Collection{{{CollectionID: "c-1", Name: "Docs"}}})
				_, _, err := r.ResolveCollection(context.Background(), "docs", 0, false)
				require.NoError(t, err)
				first := int(calls.Load())
				r.invalidateCollectionCache()
				_, cached, err := r.ResolveCollection(context.Background(), "docs", 0, false)
				require.NoError(t, err)
				require.False(t, cached)
				return first, int(calls.Load())
			},
		},
		{
			name: "widget",
			run: func(t *testing.T) (int, int) {
				r, calls := resolverFixture(t, [][]favro.Widget{{{WidgetCommonID: "w-1", Name: "Board"}}})
				_, _, err := r.ResolveWidget(context.Background(), "board", "", 0, false)
				require.NoError(t, err)
				first := int(calls.Load())
				r.invalidateWidgetCache()
				_, cached, err := r.ResolveWidget(context.Background(), "board", "", 0, false)
				require.NoError(t, err)
				require.False(t, cached)
				return first, int(calls.Load())
			},
		},
		{
			name: "column",
			run: func(t *testing.T) (int, int) {
				r, calls := resolverFixture(t, [][]favro.Column{{{ColumnID: "col-1", WidgetCommonID: "w-1", Name: "Doing"}}})
				_, _, err := r.ResolveColumn(context.Background(), "w-1", "doing", 0, false)
				require.NoError(t, err)
				first := int(calls.Load())
				r.invalidateColumnCache("w-1")
				_, cached, err := r.ResolveColumn(context.Background(), "w-1", "doing", 0, false)
				require.NoError(t, err)
				require.False(t, cached, "column cache must miss after invalidate for the same widget")
				return first, int(calls.Load())
			},
		},
		{
			name: "custom_field",
			run: func(t *testing.T) (int, int) {
				r, calls := resolverFixture(t, [][]favro.CustomField{{{CustomFieldID: "cf-1", Name: "Priority", Type: "Single select"}}})
				_, _, err := r.ResolveCustomField(context.Background(), "prior", 0, false)
				require.NoError(t, err)
				first := int(calls.Load())
				r.invalidateCustomFieldCache()
				_, cached, err := r.ResolveCustomField(context.Background(), "prior", 0, false)
				require.NoError(t, err)
				require.False(t, cached)
				return first, int(calls.Load())
			},
		},
		{
			name: "group",
			run: func(t *testing.T) (int, int) {
				r, calls := resolverFixture(t, [][]favro.Group{{{GroupID: "g-1", Name: "Engineering"}}})
				_, _, err := r.ResolveGroup(context.Background(), "eng", 0, false)
				require.NoError(t, err)
				first := int(calls.Load())
				r.invalidateGroupCache()
				_, cached, err := r.ResolveGroup(context.Background(), "eng", 0, false)
				require.NoError(t, err)
				require.False(t, cached)
				return first, int(calls.Load())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first, second := tc.run(t)
			require.Equal(t, 1, first, "exactly one HTTP call before invalidate")
			require.Equal(t, 2, second, "invalidate must force a re-fetch")
		})
	}
}
