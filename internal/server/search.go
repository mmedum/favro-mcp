package server

import (
	"cmp"
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// SearchScope picks the corpus the search runs over. Favro's /cards
// endpoint rejects unfiltered listings (HTTP 400, "One of todoList,
// cardCommonId, cardSequentialId, widgetCommonId, collectionId is a
// required parameter"), so an "org-wide" scope is not directly
// supported — the caller must pass widget_common_id or collection_id.
type SearchScope int

// Search scope flavors. Both require the matching ID; an unset scope
// is rejected at the tool layer with a typed error guiding the
// caller to favro_resolve_widget / favro_resolve_collection first.
const (
	SearchScopeWidget SearchScope = iota
	SearchScopeCollection
)

// searchCardCacheTTL is plan §7's "card-list-by-search-scope 60s"
// budget — short enough that mid-session card creation is visible
// quickly, long enough that a chat with a few back-to-back search
// queries shares one fetch.
const searchCardCacheTTL = 60 * time.Second

// searchCardCacheKeyPrefix is the namespace under which every search
// cache key lives. Phase 5 mutating card tools will call
// invalidateSearchCardCache, which uses InvalidatePrefix to drop
// every entry in one call.
const searchCardCacheKeyPrefix = "search:"

// SearchedCard is one ranked search hit. Output deliberately omits
// `column_name`: resolving the column name for every hit would mean
// a per-widget /columns fetch for up to 50 results, which burns the
// same rate-limit budget the search cache exists to protect.
// Callers should chain to favro_get_card_full for the cards they
// want metadata on.
type SearchedCard struct {
	CardCommonID       string  `json:"card_common_id"`
	CardID             string  `json:"card_id"`
	SequentialID       int     `json:"sequential_id,omitempty"`
	SequentialIDPrefix string  `json:"sequential_id_prefix,omitempty"`
	Name               string  `json:"name"`
	Snippet            string  `json:"snippet,omitempty"`
	WidgetCommonID     string  `json:"widget_common_id,omitempty"`
	ColumnID           string  `json:"column_id,omitempty"`
	IsArchived         bool    `json:"is_archived,omitempty"`
	Score              float64 `json:"score"`
}

// scopedCorpus is the cached fetch+strip result for one (scope,
// scopeID, includeArchived) tuple. Pre-stripping and pre-lowering
// at fetch time means warm-cache repeat queries skip both the
// per-card markdown sweep and the per-card ToLower (run by ranker
// and snippet extractor combined ~3× otherwise). cards / lowerNames
// / bodies / lowerBodies are parallel slices indexed by card.
type scopedCorpus struct {
	cards       []favro.Card
	lowerNames  []string
	bodies      []string
	lowerBodies []string
}

// scoredCard is one card that survived the score+filter pass, kept
// alongside its already-stripped body so the snippet projection
// doesn't re-strip and doesn't re-lowercase.
type scoredCard struct {
	card      favro.Card
	body      string
	lowerBody string
	score     float64
}

// SearchCards is the entry point Phase 4.3 exposes. It runs a local
// full-text search over the cards in scope and returns up to limit
// hits ranked by name/body relevance. minScore (when > 0) drops
// candidates below the threshold post-rank, pre-cap.
//
// Empty query short-circuits to an empty result without touching
// Favro — empty queries are not wildcards.
//
// `cached` is true when the underlying card list came from the 60s
// scoped cache rather than a fresh Favro fetch.
func (r *Resolver) SearchCards(
	ctx context.Context,
	query string,
	scope SearchScope,
	scopeID string,
	includeArchived bool,
	limit int,
	minScore float64,
	forceRefresh bool,
) (results []SearchedCard, cached bool, err error) {
	limit = clampLimit(limit)
	if query == "" {
		return nil, false, nil
	}

	corpus, cached, err := r.fetchScopedCards(ctx, scope, scopeID, includeArchived, forceRefresh)
	if err != nil {
		return nil, cached, err
	}

	lq := strings.ToLower(query)
	tokens := tokenize(lq)
	picks := scoreAndRank(corpus, lq, tokens, includeArchived, minScore, limit)
	return projectSearchedCards(picks, lq), cached, nil
}

// scoreAndRank filters cards by archived-flag, scores each survivor,
// drops zero / sub-minScore hits, sorts (score desc, name asc), and
// caps at limit. Pulled out of SearchCards to keep the entry point
// under the cyclomatic-complexity budget.
func scoreAndRank(corpus scopedCorpus, lowerQuery string, tokens []string, includeArchived bool, minScore float64, limit int) []scoredCard {
	picks := make([]scoredCard, 0, len(corpus.cards))
	for i, c := range corpus.cards {
		if c.IsArchived && !includeArchived {
			continue
		}
		s := scoreCard(corpus.lowerNames[i], corpus.lowerBodies[i], lowerQuery, tokens)
		if s == 0 {
			continue
		}
		if minScore > 0 && s < minScore {
			continue
		}
		picks = append(picks, scoredCard{
			card:      c,
			body:      corpus.bodies[i],
			lowerBody: corpus.lowerBodies[i],
			score:     s,
		})
	}
	slices.SortStableFunc(picks, func(a, b scoredCard) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		return cmp.Compare(a.card.Name, b.card.Name)
	})
	if len(picks) > limit {
		picks = picks[:limit]
	}
	return picks
}

// projectSearchedCards converts ranked picks into the public
// SearchedCard shape, computing each hit's snippet from the
// already-stripped body.
func projectSearchedCards(picks []scoredCard, lowerQuery string) []SearchedCard {
	out := make([]SearchedCard, 0, len(picks))
	for _, p := range picks {
		out = append(out, SearchedCard{
			CardCommonID:       p.card.CardCommonID,
			CardID:             p.card.CardID,
			SequentialID:       p.card.SequentialID,
			SequentialIDPrefix: p.card.SequentialIDPrefix,
			Name:               p.card.Name,
			Snippet:            extractSnippet(p.body, p.lowerBody, lowerQuery, 120),
			WidgetCommonID:     p.card.WidgetCommonID,
			ColumnID:           p.card.ColumnID,
			IsArchived:         p.card.IsArchived,
			Score:              p.score,
		})
	}
	return out
}

// fetchScopedCards returns the cached corpus for (scope, scopeID,
// includeArchived); on a miss, fetches it from Favro, strips and
// lowercases the per-card name + body once, and caches the result.
// Both widget and collection scopes paginate fully — the scope's
// natural size bounds the cost.
func (r *Resolver) fetchScopedCards(
	ctx context.Context,
	scope SearchScope,
	scopeID string,
	includeArchived bool,
	forceRefresh bool,
) (scopedCorpus, bool, error) {
	key := searchCacheKey(scope, scopeID, includeArchived)
	if !forceRefresh {
		if hit, ok := r.searchCardCache.Get(key); ok {
			return hit, true, nil
		}
	}

	q := url.Values{}
	// descriptionFormat=markdown keeps the body in the format the
	// markdown stripper expects; without it Favro defaults are
	// HTML-flavored which would defeat the strip rules below.
	q.Set("descriptionFormat", "markdown")
	switch scope {
	case SearchScopeWidget:
		q.Set("widgetCommonId", scopeID)
	case SearchScopeCollection:
		q.Set("collectionId", scopeID)
	}

	var all []favro.Card
	visit := func(env favro.PageEnvelope[favro.Card]) error {
		all = append(all, env.Entities...)
		return nil
	}
	if err := favro.Paginate(ctx, r.client, "/cards", q, visit); err != nil {
		return scopedCorpus{}, false, err
	}
	corpus := buildCorpus(all)
	r.searchCardCache.Set(key, corpus, searchCardCacheTTL)
	return corpus, false, nil
}

// buildCorpus pre-strips each card's description and pre-lowercases
// the name + stripped body so the ranker and snippet extractor can
// reuse the work across multiple queries against the same scope.
func buildCorpus(cards []favro.Card) scopedCorpus {
	corpus := scopedCorpus{
		cards:       cards,
		lowerNames:  make([]string, len(cards)),
		bodies:      make([]string, len(cards)),
		lowerBodies: make([]string, len(cards)),
	}
	for i, c := range cards {
		corpus.lowerNames[i] = strings.ToLower(c.Name)
		corpus.bodies[i] = stripMarkdown(c.DetailedDescription)
		corpus.lowerBodies[i] = strings.ToLower(corpus.bodies[i])
	}
	return corpus
}

// searchCacheKey composes the cache key for (scope, scopeID,
// includeArchived). Tested directly so the format is locked in for
// the invalidateSearchCardCache prefix sweep.
func searchCacheKey(scope SearchScope, scopeID string, includeArchived bool) string {
	var b strings.Builder
	b.WriteString(searchCardCacheKeyPrefix)
	switch scope {
	case SearchScopeWidget:
		b.WriteString("widget:")
		b.WriteString(scopeID)
	case SearchScopeCollection:
		b.WriteString("collection:")
		b.WriteString(scopeID)
	}
	if includeArchived {
		b.WriteString(":+archived")
	}
	return b.String()
}

// invalidateSearchCardCache drops every cached search corpus so the
// next favro_search_cards sees fresh data. One InvalidatePrefix call
// drops org/widget/collection × archived variants in one shot;
// mutating card tools call this after a successful write.
func (r *Resolver) invalidateSearchCardCache() {
	r.searchCardCache.InvalidatePrefix(searchCardCacheKeyPrefix)
}

// ============================================================
// Ranking
// ============================================================

// tokenSplitRE splits a query on Unicode-aware non-alphanumeric
// boundaries so "BSC-123" tokenizes as ["bsc", "123"] and
// "frontend-board" as ["frontend", "board"]. Empty results are
// dropped by the caller.
var tokenSplitRE = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// tokenize splits an already-lowercased query into search tokens. It
// is the only tokenizer the ranker uses; the haystack is matched via
// strings.Contains rather than tokenized, so "the front line" is
// still a hit for the token "front".
func tokenize(lowerQuery string) []string {
	parts := tokenSplitRE.Split(lowerQuery, -1)
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// scoreCard returns a score in roughly [0, 2] given pre-lowercased
// name, body (markdown-stripped), and query, plus the query's
// tokens. Empty query / no tokens yield 0.
//
// Pre-lowercasing is the caller's responsibility because the same
// name+body pair is scored across many queries against the same
// scope cache, so doing it once at fetch time saves redundant work.
//
// The score scale is additive so an LLM reading a 1.0 + 0.5 = 1.5
// card knows two pieces of evidence (name phrase and body phrase)
// matched. Body matches alone (max 1.0) lose to name matches alone
// (max 1.6) — name is more authoritative than body.
//
// Components:
//   - name phrase: +1.0 when the query is a substring of the name.
//   - name token overlap: +0.6 × (distinct hits / token count).
//   - body phrase: +0.5 when the query is a substring of the body.
//   - body token overlap: +0.5 × (distinct hits / token count).
func scoreCard(lowerName, lowerBody, lowerQuery string, tokens []string) float64 {
	if lowerQuery == "" || len(tokens) == 0 {
		return 0
	}
	score := 0.0
	if strings.Contains(lowerName, lowerQuery) {
		score += 1.0
	}
	if hits := countTokenHits(lowerName, tokens); hits > 0 {
		score += 0.6 * float64(hits) / float64(len(tokens))
	}
	if strings.Contains(lowerBody, lowerQuery) {
		score += 0.5
	}
	if hits := countTokenHits(lowerBody, tokens); hits > 0 {
		score += 0.5 * float64(hits) / float64(len(tokens))
	}
	return score
}

// countTokenHits counts how many distinct tokens appear at least
// once in haystack. Repeated occurrences still count as one — the
// signal we care about is "did the haystack mention this token",
// not "how often" (which heavily favors long bodies and biases the
// ranker toward verbose cards).
func countTokenHits(haystack string, tokens []string) int {
	n := 0
	for _, t := range tokens {
		if t != "" && strings.Contains(haystack, t) {
			n++
		}
	}
	return n
}

// extractSnippet returns a short, ~maxLen-char window of body
// centered on the first match of lowerQuery, with ellipses on either
// side when truncated. Empty when no match — the caller can choose
// to fall back to a name-only display.
//
// body is the original-case stripped body (used for the visible
// snippet); lowerBody is its already-lowercased twin (used for the
// case-insensitive match). The two must have the same length —
// pre-lowering the body separately avoids redoing strings.ToLower
// on every snippet call.
//
// The window is collapsed via strings.Fields/Join so newlines and
// runs of whitespace from the original markdown become single spaces.
func extractSnippet(body, lowerBody, lowerQuery string, maxLen int) string {
	if lowerQuery == "" || body == "" || maxLen <= 0 {
		return ""
	}
	idx := strings.Index(lowerBody, lowerQuery)
	if idx < 0 {
		return ""
	}
	half := maxLen / 2
	start := idx - half
	end := idx + len(lowerQuery) + half
	if start < 0 {
		start = 0
	}
	if end > len(body) {
		end = len(body)
	}
	collapsed := strings.Join(strings.Fields(body[start:end]), " ")
	prefix := ""
	suffix := ""
	if start > 0 {
		prefix = "…"
	}
	if end < len(body) {
		suffix = "…"
	}
	return prefix + collapsed + suffix
}

// ============================================================
// Markdown stripping
// ============================================================
//
// Not a full markdown parser — covers the cases that affect ranking
// and snippet readability: code spans, links/images, headings, list
// markers, blockquotes, HTML tags, emphasis. Rules apply in order;
// images run before links because the image syntax `![alt](url)` is
// a superset of the link syntax.

var (
	mdCodeFence  = regexp.MustCompile("(?s)```.*?```")
	mdInlineCode = regexp.MustCompile("`[^`]*`")
	mdImage      = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink       = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdHeading    = regexp.MustCompile(`(?m)^\s*#{1,6}\s+`)
	mdBlockquote = regexp.MustCompile(`(?m)^\s*>+\s?`)
	mdListMarker = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+\.)\s+`)
	mdHTMLTag    = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)

	// Emphasis: separate non-backref patterns per marker form. Go's
	// RE2 doesn't support backreferences (the original
	// `([*_]{1,3})(...)\1` pattern was flagged by staticcheck SA1000),
	// so we walk longer markers first to avoid the shorter pattern
	// chewing the outer marker of a longer one (e.g. `*` matching
	// the inner asterisks of `***bold***`).
	mdEmphasisPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\*\*\*([^*]+?)\*\*\*`),
		regexp.MustCompile(`___([^_]+?)___`),
		regexp.MustCompile(`\*\*([^*]+?)\*\*`),
		regexp.MustCompile(`__([^_]+?)__`),
		regexp.MustCompile(`\*([^*]+?)\*`),
		regexp.MustCompile(`_([^_]+?)_`),
	}
)

// stripMarkdown removes the markdown noise from body so the search
// ranker sees the human-meaningful prose. A `[text](url)` link
// becomes `text`; an `![alt](url)` image is dropped entirely. The
// goal is search-quality, not faithful rendering — pathological
// inputs (mismatched markers, nested emphasis) survive in degraded
// form rather than corrupting the rest of the body.
func stripMarkdown(body string) string {
	if body == "" {
		return ""
	}
	s := body
	s = mdCodeFence.ReplaceAllString(s, " ")
	s = mdInlineCode.ReplaceAllString(s, " ")
	s = mdImage.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdHeading.ReplaceAllString(s, "")
	s = mdBlockquote.ReplaceAllString(s, "")
	s = mdListMarker.ReplaceAllString(s, "")
	s = mdHTMLTag.ReplaceAllString(s, " ")
	for _, re := range mdEmphasisPatterns {
		s = re.ReplaceAllString(s, "$1")
	}
	return strings.Join(strings.Fields(s), " ")
}
