package server

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const searchCardsToolName = "favro_search_cards"

// errSearchCardsScopeConflict is returned when the caller supplies
// both widget_common_id and collection_id. The two are mutually
// exclusive — Favro's /cards endpoint accepts either filter, not
// both, and conflating them would silently widen the search beyond
// the caller's intent.
var errSearchCardsScopeConflict = errors.New("favro_search_cards: pass widget_common_id OR collection_id, not both")

// errSearchCardsScopeRequired is returned when neither widget_common_id
// nor collection_id is supplied. Favro's /cards endpoint rejects
// unfiltered listings (HTTP 400) so org-wide search isn't directly
// supported; the caller should resolve a widget or collection first.
var errSearchCardsScopeRequired = errors.New("favro_search_cards: widget_common_id or collection_id is required (Favro's /cards endpoint rejects unfiltered listings); call favro_resolve_widget or favro_resolve_collection first if you don't know one")

// searchCardsInput is the input for favro_search_cards.
//
// Exactly one of widget_common_id / collection_id must be set:
//   - widget_common_id set → widget scope.
//   - collection_id set → collection scope.
type searchCardsInput struct {
	Query           string  `json:"query" jsonschema:"natural-language search fragment; matched case-insensitively against card name and description (markdown is stripped before matching)"`
	WidgetCommonID  string  `json:"widget_common_id,omitempty" jsonschema:"Favro widgetCommonId; mutually exclusive with collection_id. Exactly one of widget_common_id / collection_id is required."`
	CollectionID    string  `json:"collection_id,omitempty" jsonschema:"Favro collectionId; mutually exclusive with widget_common_id. Exactly one of widget_common_id / collection_id is required."`
	IncludeArchived bool    `json:"include_archived,omitempty" jsonschema:"include archived cards in the corpus (default false)"`
	Limit           int     `json:"limit,omitempty" jsonschema:"max hits to return (default 10, max 50)"`
	MinScore        float64 `json:"min_score,omitempty" jsonschema:"optional score floor [0, ~2]; results below this are dropped before the limit cap. Score scale: name phrase +1.0, name token overlap up to +0.6, body phrase +0.5, body token overlap up to +0.5"`
	ForceRefresh    bool    `json:"force_refresh,omitempty" jsonschema:"bypass the 60s scoped card-list cache and re-fetch from Favro before searching"`
}

// searchCardsOutput is the output for favro_search_cards.
type searchCardsOutput struct {
	Results []SearchedCard `json:"results" jsonschema:"ranked search hits; empty when nothing matches"`
	Cached  bool           `json:"cached" jsonschema:"true when the underlying card list came from the in-memory scoped cache rather than a fresh Favro fetch"`
}

func registerSearchCards(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: searchCardsToolName,
		Description: "Search Favro cards by name and description, ranked locally because " +
			"the Favro API has no full-text search. Exactly one of `widget_common_id` " +
			"(single board) or `collection_id` (single collection) is required — Favro's " +
			"/cards endpoint rejects unfiltered listings, so org-wide search is not " +
			"supported; call favro_resolve_widget or favro_resolve_collection first if " +
			"you don't know the scope. Markdown in descriptions is stripped before " +
			"matching. `column_name` is intentionally omitted from each result (resolving " +
			"columns per-hit would burn the rate-limit budget the cache exists to " +
			"protect); chain to favro_get_card_full for the cards you want metadata on. " +
			"The card list is cached for 60 seconds per (scope, include_archived); pass " +
			"`force_refresh: true` to bypass the cache. Score scale: name phrase +1.0, " +
			"name token overlap up to +0.6, body phrase +0.5, body token overlap up to " +
			"+0.5 (additive). Read-only.",
		Annotations: readOnly("Search Favro cards"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchCardsInput) (*mcp.CallToolResult, searchCardsOutput, error) {
		if in.WidgetCommonID != "" && in.CollectionID != "" {
			return nil, searchCardsOutput{}, errSearchCardsScopeConflict
		}
		var scope SearchScope
		var scopeID string
		switch {
		case in.WidgetCommonID != "":
			scope = SearchScopeWidget
			scopeID = in.WidgetCommonID
		case in.CollectionID != "":
			scope = SearchScopeCollection
			scopeID = in.CollectionID
		default:
			return nil, searchCardsOutput{}, errSearchCardsScopeRequired
		}
		results, cached, err := r.SearchCards(ctx, in.Query, scope, scopeID, in.IncludeArchived, in.Limit, in.MinScore, in.ForceRefresh)
		if err != nil {
			return nil, searchCardsOutput{}, err
		}
		return nil, searchCardsOutput{Results: results, Cached: cached}, nil
	})
}
