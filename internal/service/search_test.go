package service

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ============================================================
// Markdown stripping
// ============================================================

func TestStripMarkdown_CornerCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "just some prose", "just some prose"},
		{"heading", "# Title\n\nbody", "Title body"},
		{"deep heading", "###### Six\n\nbody", "Six body"},
		{"blockquote", "> quoted line\n> another", "quoted line another"},
		{"unordered list", "- one\n- two\n- three", "one two three"},
		{"ordered list", "1. first\n2. second", "first second"},
		{"link kept text", "see [the docs](https://example.com)", "see the docs"},
		{"image dropped", "before ![alt](https://example.com/x.png) after", "before after"},
		{"image then link", "![logo](x.png)[click](y.html)", "click"},
		{"inline code", "use the `foo()` function", "use the function"},
		{"code fence multiline", "before\n```\nx := 1\n```\nafter", "before after"},
		{"bold double asterisk", "this is **bold** text", "this is bold text"},
		{"bold double underscore", "this is __bold__ text", "this is bold text"},
		{"italic single asterisk", "this is *italic* text", "this is italic text"},
		{"italic single underscore", "this is _italic_ text", "this is italic text"},
		{"bold italic triple", "this is ***super*** strong", "this is super strong"},
		{"bold italic triple underscore", "this is ___super___ strong", "this is super strong"},
		{"html tag stripped", "before <strong>middle</strong> after", "before middle after"},
		{"composite", "# Title\n\n[link](x) and **bold** with `code` here", "Title link and bold with here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stripMarkdown(tc.in)
			if got := got; got != tc.want {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}

// ============================================================
// Tokenizer
// ============================================================

func TestTokenize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain words", "find the printing pass", []string{"find", "the", "printing", "pass"}},
		{"mixed alphanum", "bsc-123", []string{"bsc", "123"}},
		{"unicode letters", "tøj på lager", []string{"tøj", "på", "lager"}},
		{"runs of separators", "  foo   bar  ", []string{"foo", "bar"}},
		{"empty string", "", nil},
		{"only separators", " --- ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tokenize(tc.in)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Errorf("got = %v, want empty", got)
				}
				return
			}
			if got := got; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}

// ============================================================
// Score scale
// ============================================================

// scoreCardLower is a test helper that mirrors the production
// pre-lowering contract: scoreCard takes already-lowercased name +
// body, so tests do the same once at the call site rather than
// hand-lowering each field.
func scoreCardLower(name, body, lowerQuery string, tokens []string) float64 {
	return scoreCard(strings.ToLower(name), strings.ToLower(body), lowerQuery, tokens)
}

func TestScoreCard_ScoreScale(t *testing.T) {
	t.Parallel()

	q := "printing"
	tokens := tokenize(q)

	cases := []struct {
		name      string
		cardName  string
		body      string
		wantRange [2]float64 // [min, max] inclusive
	}{
		{
			name:      "no match",
			cardName:  "Some other card",
			body:      "unrelated body",
			wantRange: [2]float64{0, 0},
		},
		{
			name:     "name phrase only",
			cardName: "Printing pass workflow",
			body:     "no body match",
			// 1.0 (name phrase) + 0.6 (1/1 token) = 1.6
			wantRange: [2]float64{1.6, 1.6},
		},
		{
			name:     "body phrase only",
			cardName: "Lookup card",
			body:     "describes the printing process",
			// 0.5 (body phrase) + 0.5 (1/1 token) = 1.0
			wantRange: [2]float64{1.0, 1.0},
		},
		{
			name:     "both name and body match",
			cardName: "Printing setup",
			body:     "talks about printing again",
			// 1.0 + 0.6 + 0.5 + 0.5 = 2.6
			wantRange: [2]float64{2.6, 2.6},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := scoreCardLower(tc.cardName, tc.body, q, tokens)
			if got < tc.wantRange[0] {
				t.Errorf("got = %v, want at least %v", got, tc.wantRange[0])
			}
			if got > tc.wantRange[1] {
				t.Errorf("got = %v, want at most %v", got, tc.wantRange[1])
			}
		})
	}
}

func TestScoreCard_NameBeatsBodyOnly(t *testing.T) {
	t.Parallel()

	q := "printing"
	tokens := tokenize(q)

	nameOnly := scoreCardLower("Printing pass", "unrelated body", q, tokens)
	bodyOnly := scoreCardLower("Lookup", "talks about printing", q, tokens)

	// Name match (1.0+0.6) must outscore body-only match (0.5+0.5).
	if nameOnly <= bodyOnly {
		t.Errorf("name-match score must beat body-only score so search results respect title authority: got %v, want greater than %v", nameOnly, bodyOnly)
	}
}

func TestScoreCard_DistinctTokenHits(t *testing.T) {
	t.Parallel()

	// Repeated occurrences of the same token still count as one.
	tokens := tokenize("printing")
	a := scoreCardLower("Lookup", "printing once", "printing", tokens)
	b := scoreCardLower("Lookup", "printing twice printing thrice printing", "printing", tokens)
	if math.Abs(b-a) > 0.0001 {
		t.Errorf("b = %v, want %v within 0.0001", b, a)
	}
}

func TestScoreCard_MultiTokenPartialOverlap(t *testing.T) {
	t.Parallel()

	q := "printing pass workflow"
	tokens := tokenize(q)
	if len(tokens) != 3 {
		t.Fatalf("len(tokens) = %d, want 3", len(tokens))
	}

	// Only "printing" hits in body — 1 of 3 tokens.
	got := scoreCardLower("Lookup", "discusses printing strategy", q, tokens)
	// Token-overlap component: 0.5 × 1/3 ≈ 0.1667
	if math.Abs(got-0.5/3) > 0.0001 {
		t.Errorf("got = %v, want %v within 0.0001", got, 0.5/3)
	}
}

// ============================================================
// Snippet extraction
// ============================================================

func TestExtractSnippet(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a ", 80) + "the printing pass is here " + strings.Repeat("b ", 80)

	cases := []struct {
		name        string
		body        string
		query       string
		maxLen      int
		wantContain string
		wantPrefix  string
		wantSuffix  string
		wantEmpty   bool
	}{
		{
			name:      "empty query",
			body:      "any body",
			query:     "",
			maxLen:    120,
			wantEmpty: true,
		},
		{
			name:      "empty body",
			body:      "",
			query:     "x",
			maxLen:    120,
			wantEmpty: true,
		},
		{
			name:      "no match",
			body:      "talks about something else",
			query:     "printing",
			maxLen:    120,
			wantEmpty: true,
		},
		{
			name:        "match at start",
			body:        "printing pass workflow notes",
			query:       "printing",
			maxLen:      120,
			wantContain: "printing pass workflow notes",
		},
		{
			name:        "long body trimmed both sides",
			body:        long,
			query:       "printing",
			maxLen:      80,
			wantContain: "the printing pass is here",
			wantPrefix:  "…",
			wantSuffix:  "…",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractSnippet(tc.body, strings.ToLower(tc.body), tc.query, tc.maxLen)
			if tc.wantEmpty {
				if len(got) != 0 {
					t.Errorf("got = %v, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantContain) {
				t.Errorf("got does not contain %q", tc.wantContain)
			}
			if tc.wantPrefix != "" {
				if !strings.HasPrefix(got, tc.wantPrefix) {
					t.Error("strings.HasPrefix(got, tc.wantPrefix) = false, want true")
				}
			}
			if tc.wantSuffix != "" {
				if !strings.HasSuffix(got, tc.wantSuffix) {
					t.Error("strings.HasSuffix(got, tc.wantSuffix) = false, want true")
				}
			}
		})
	}
}

// ============================================================
// Cache key shape
// ============================================================

func TestSearchCacheKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		scope           SearchScope
		scopeID         string
		includeArchived bool
		want            string
	}{
		{"widget", SearchScopeWidget, "w-1", false, "search:widget:w-1"},
		{"widget+archived", SearchScopeWidget, "w-1", true, "search:widget:w-1:+archived"},
		{"collection", SearchScopeCollection, "c-1", false, "search:collection:c-1"},
		{"collection+archived", SearchScopeCollection, "c-1", true, "search:collection:c-1:+archived"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := searchCacheKey(tc.scope, tc.scopeID, tc.includeArchived)
			if got := got; got != tc.want {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
			if !strings.HasPrefix(got, searchCardCacheKeyPrefix) {
				t.Error("every search cache key must start with the namespace prefix so InvalidatePrefix sweeps them all")
			}
		})
	}
}

// ============================================================
// SearchCards: cache + scope behavior
// ============================================================

// newSearchFixture builds a Resolver wired to a paginated /cards
// fixture. Returns the resolver, the call counter, and a function to
// fetch the captured query params of the most recent request.
func newSearchFixture(t *testing.T, pages [][]favro.Card) (*Resolver, *atomic.Int32, func() map[string]string) {
	t.Helper()

	var (
		calls    atomic.Int32
		captured map[string]string
		mu       sync.Mutex
	)
	totalPages := len(pages)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		latest := map[string]string{}
		for k, v := range r.URL.Query() {
			if len(v) > 0 {
				latest[k] = v[0]
			}
		}
		mu.Lock()
		captured = latest
		mu.Unlock()

		page := 0
		if pageStr := r.URL.Query().Get("page"); pageStr != "" {
			n, err := strconv.Atoi(pageStr)
			if err != nil || n < 0 {
				t.Errorf("invalid page query: %q", pageStr)
				return
			}
			page = n
		}
		if page >= totalPages {
			t.Errorf("test fixture asked for page %d but only %d pages defined", page, totalPages)
			return
		}
		// Mirror Favro's documented server-side `archived` filter:
		// when archived=true the response includes archived cards;
		// when absent (or false) archived cards are filtered out.
		entities := pages[page]
		if r.URL.Query().Get("archived") != "true" {
			filtered := entities[:0:0]
			for _, c := range entities {
				if !c.IsArchived {
					filtered = append(filtered, c)
				}
			}
			entities = filtered
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Card]{
			Page:      page,
			Pages:     totalPages,
			RequestID: "req-search",
			Entities:  entities,
		})
	})

	c := favroFixture(t, handler)
	return NewResolver(c), &calls, func() map[string]string {
		mu.Lock()
		defer mu.Unlock()
		// Copy so a caller iterating the map can't race with the next
		// request mutating `captured`.
		out := make(map[string]string, len(captured))
		for k, v := range captured {
			out[k] = v
		}
		return out
	}
}

func TestSearchCards_EmptyQueryShortCircuits(t *testing.T) {
	t.Parallel()

	r, calls, _ := newSearchFixture(t, [][]favro.Card{
		{{CardID: "card-1", CardCommonID: "cc-1", Name: "Anything"}},
	})

	got, cached, err := r.SearchCards(context.Background(), "", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %v, want empty", got)
	}
	if cached {
		t.Error("cached = true, want false")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("empty query must not touch Favro: got %v, want %v", got, 0)
	}
}

func TestSearchCards_ForcesMarkdownDescriptionFormat(t *testing.T) {
	t.Parallel()

	r, _, lastQuery := newSearchFixture(t, [][]favro.Card{
		{{CardID: "c1", CardCommonID: "cc1", WidgetCommonID: "w-1", Name: "Printing pass setup"}},
	})

	_, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	q := lastQuery()
	if got := q["descriptionFormat"]; got != "markdown" {
		t.Errorf("every search must force descriptionFormat=markdown so the markdown stripper sees the format it expects: got %v, want %v", got, "markdown")
	}
	if got := q["widgetCommonId"]; got != "w-1" {
		t.Errorf("q[\"widgetCommonId\"] = %v, want %v", got, "w-1")
	}
}

func TestSearchCards_WidgetScopePaginates(t *testing.T) {
	t.Parallel()

	pages := [][]favro.Card{
		{{CardID: "c1", CardCommonID: "cc1", WidgetCommonID: "w-1", Name: "Printing setup"}},
		{{CardID: "c2", CardCommonID: "cc2", WidgetCommonID: "w-1", Name: "Printing teardown"}},
	}
	r, calls, lastQuery := newSearchFixture(t, pages)

	got, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("widget scope must paginate fully: got %d", len(got))
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("two pages → two /cards calls: got %v, want %v", got, 2)
	}

	q := lastQuery()
	if got := q["widgetCommonId"]; got != "w-1" {
		t.Errorf("q[\"widgetCommonId\"] = %v, want %v", got, "w-1")
	}
}

func TestSearchCards_CacheMissThenHit(t *testing.T) {
	t.Parallel()

	r, calls, _ := newSearchFixture(t, [][]favro.Card{
		{{CardID: "c1", CardCommonID: "cc1", Name: "Printing pass"}},
	})

	_, cached, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if cached {
		t.Error("cached = true, want false")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls.Load() = %v, want %v", got, 1)
	}

	_, cached2, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !cached2 {
		t.Error("second identical search must hit the scoped cache")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("no extra HTTP call on warm cache: got %v, want %v", got, 1)
	}

	// Different query against same cached scope — still a cache hit at
	// the card-list layer; the search runs locally.
	_, cached3, err := r.SearchCards(context.Background(), "pass", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !cached3 {
		t.Error("different query, same scope must still be a cache hit")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls.Load() = %v, want %v", got, 1)
	}
}

func TestSearchCards_ForceRefreshBypassesCache(t *testing.T) {
	t.Parallel()

	r, calls, _ := newSearchFixture(t, [][]favro.Card{
		{{CardID: "c1", CardCommonID: "cc1", Name: "Printing pass"}},
	})

	_, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls.Load() = %v, want %v", got, 1)
	}

	_, cached, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, true)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if cached {
		t.Error("force_refresh must report uncached")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("force_refresh must trigger a second HTTP call: got %v, want %v", got, 2)
	}
}

func TestSearchCards_TTLExpiryRefetches(t *testing.T) {
	t.Parallel()

	r, calls, _ := newSearchFixture(t, [][]favro.Card{
		{{CardID: "c1", CardCommonID: "cc1", Name: "Printing"}},
	})

	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	r.searchCardCache.Now = func() time.Time { return now }

	_, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls.Load() = %v, want %v", got, 1)
	}

	now = now.Add(searchCardCacheTTL + time.Second)

	_, cached, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if cached {
		t.Error("expired entry must miss the cache")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls.Load() = %v, want %v", got, 2)
	}
}

func TestSearchCards_MinScoreFilter(t *testing.T) {
	t.Parallel()

	// Three cards with descending scores: name-phrase (1.6), body-phrase (1.0), token-only (0.6 ≈ 0.6/1).
	r, _, _ := newSearchFixture(t, [][]favro.Card{
		{
			{CardID: "name-phrase", CardCommonID: "cc1", Name: "Printing setup"},
			{CardID: "body-phrase", CardCommonID: "cc2", Name: "Lookup card", DetailedDescription: "this body talks about printing in detail"},
			{CardID: "name-token", CardCommonID: "cc3", Name: "talk about Printing strategies broadly"},
		},
	})

	// min_score=1.5 must drop the body-phrase (1.0) candidate.
	got, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 1.5, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, c := range got {
		if c.Score < 1.5 {
			t.Errorf("min_score must filter low-score hits: got %v, want at least %v", c.Score, 1.5)
		}
	}
}

func TestSearchCards_RankingOrder(t *testing.T) {
	t.Parallel()

	r, _, _ := newSearchFixture(t, [][]favro.Card{
		{
			{CardID: "body-only", CardCommonID: "cc1", Name: "Unrelated", DetailedDescription: "the body mentions printing"},
			{CardID: "name-match", CardCommonID: "cc2", Name: "Printing setup"},
			{CardID: "name+body", CardCommonID: "cc3", Name: "Printing teardown", DetailedDescription: "talks about printing again"},
		},
	})

	got, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	// name+body (2.6) > name-match (1.6) > body-only (1.0)
	if got := got[0].CardID; got != "name+body" {
		t.Errorf("got[0].CardID = %v, want %v", got, "name+body")
	}
	if got := got[1].CardID; got != "name-match" {
		t.Errorf("got[1].CardID = %v, want %v", got, "name-match")
	}
	if got := got[2].CardID; got != "body-only" {
		t.Errorf("got[2].CardID = %v, want %v", got, "body-only")
	}
}

func TestSearchCards_SnippetAndArchivedFlag(t *testing.T) {
	t.Parallel()

	r, _, _ := newSearchFixture(t, [][]favro.Card{
		{
			{CardID: "c1", CardCommonID: "cc1", Name: "Some card", DetailedDescription: "details about **printing** the badges go here"},
			{CardID: "archived-1", CardCommonID: "cc-arch", Name: "Old printing", IsArchived: true},
		},
	})

	got, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("archived card must be excluded when include_archived=false: got %d", len(got))
	}
	if got := got[0].CardID; got != "c1" {
		t.Errorf("got[0].CardID = %v, want %v", got, "c1")
	}
	if !strings.Contains(got[0].Snippet, "printing") {
		t.Errorf("snippet must contain the matched query token: %q missing", "printing")
	}

	// include_archived=true brings the archived card into the result.
	got, _, err = r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", true, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
}

func TestSearchCards_LimitDefaultAndCap(t *testing.T) {
	t.Parallel()

	cards := make([]favro.Card, 60)
	for i := range cards {
		cards[i] = favro.Card{CardID: "c", CardCommonID: "cc", Name: "Printing match"}
	}
	r, _, _ := newSearchFixture(t, [][]favro.Card{cards})

	got, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("limit <= 0 must default to 10: got %d", len(got))
	}

	got, _, err = r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 9999, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("limit > 50 must be capped at 50: got %d", len(got))
	}
}

func TestSearchCards_InvalidateCacheHook(t *testing.T) {
	t.Parallel()

	r, calls, _ := newSearchFixture(t, [][]favro.Card{
		{{CardID: "c1", CardCommonID: "cc1", Name: "Printing"}},
	})

	_, _, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls.Load() = %v, want %v", got, 1)
	}

	r.InvalidateSearchCardCache()

	_, cached, err := r.SearchCards(context.Background(), "printing", SearchScopeWidget, "w-1", false, 0, 0, false)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if cached {
		t.Error("cache must miss after invalidate")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls.Load() = %v, want %v", got, 2)
	}
}
