package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const addCommentToCardToolName = "favro_add_comment_to_card"

// addCommentAmbiguityMargin is how close the top search-card score
// must be to ANY other in the top-K window for the match to count
// as ambiguous (additive scale, 0..~2.0 per search.go). 0.2 means
// "another result is within ~10% of the top" on the typical
// 1.0-name-phrase + extras score curve. Tuned to forbid the obvious
// near-tie cases without rejecting "one strong hit, weak runners-up".
const addCommentAmbiguityMargin = 0.2

// addCommentAmbiguityWindow caps the number of search hits the
// ambiguity check inspects. 5 is enough to catch genuine three- /
// four-way ties without paying for a deep scan; the corpus is
// already cached so the cost is in-process ranking only.
const addCommentAmbiguityWindow = 5

// errAddCommentSearchScopeRequired is returned when search_query is
// supplied without exactly one of widget_common_id / collection_id.
// Local FT search needs a scope (Favro's /cards endpoint rejects
// unfiltered listings); the tool surfaces the requirement directly
// instead of waiting for the deeper search-cards 400.
var errAddCommentSearchScopeRequired = errors.New("favro_add_comment_to_card: search_query requires exactly one of widget_common_id or collection_id")

// errAddCommentIdentityRequired is returned when the caller supplies
// neither a card identity nor a search_query.
var errAddCommentIdentityRequired = errors.New("favro_add_comment_to_card: pass exactly one of card_common_id, card_id, sequential_id, or search_query")

// errAddCommentNoMatch is returned when search_query matches no
// cards in the supplied scope.
var errAddCommentNoMatch = errors.New("favro_add_comment_to_card: search_query matched no cards in the supplied scope")

// addCommentToCardInput is the input for favro_add_comment_to_card.
// Exactly one of the four identity flavors must be set; with
// search_query, exactly one of widget_common_id / collection_id is
// required for scope.
type addCommentToCardInput struct {
	dryRunInput
	CardCommonID   string `json:"card_common_id,omitempty" jsonschema:"the cross-widget Favro cardCommonId. Pass exactly one of card_common_id / card_id / sequential_id / search_query."`
	CardID         string `json:"card_id,omitempty" jsonschema:"the per-widget Favro cardId; the tool resolves it to the card's cardCommonId before posting"`
	SequentialID   int    `json:"sequential_id,omitempty" jsonschema:"the integer of a 'BSC-123' ref"`
	SearchQuery    string `json:"search_query,omitempty" jsonschema:"natural-language card name; resolves to the top FT-search hit. Requires widget_common_id or collection_id for scope. Refuses ambiguous matches (top-2 scores within 0.2)."`
	WidgetCommonID string `json:"widget_common_id,omitempty" jsonschema:"search scope when using search_query"`
	CollectionID   string `json:"collection_id,omitempty" jsonschema:"alternate search scope when using search_query"`
	Comment        string `json:"comment" jsonschema:"the comment text (markdown supported)"`
}

// addCommentToCardResult is the writeOutput.Result type for
// favro_add_comment_to_card. Either Comment is populated (the card
// was resolved unambiguously and the live PUT succeeded — or dry-run
// would have) or Ambiguous=true + Candidates lists the close-score
// search hits the LLM should pick from.
type addCommentToCardResult struct {
	Comment    *favro.Comment `json:"comment,omitempty" jsonschema:"the created comment (when card was resolved unambiguously and live mode)"`
	Ambiguous  bool           `json:"ambiguous,omitempty" jsonschema:"true when search_query had close-scoring matches; pick one and re-run with card_common_id"`
	Candidates []SearchedCard `json:"candidates,omitempty" jsonschema:"ranked candidate cards when ambiguous=true"`
}

func registerAddCommentToCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: addCommentToCardToolName,
		Description: "Add a comment to a Favro card identified by one of card_common_id / " +
			"card_id / sequential_id (the integer of a 'BSC-123' ref) / search_query. With " +
			"search_query, also pass widget_common_id OR collection_id for scope; the tool " +
			"runs favro_search_cards locally and refuses ambiguous matches (top-2 scores " +
			"within 0.2) by returning the candidate list — pick one and re-run with " +
			"card_common_id. Successful live writes do NOT invalidate any cache (comments " +
			"aren't cached at the resolver layer). Pass `dry_run: true` to preview.",
		Annotations: mutating("Add Favro comment to card", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addCommentToCardInput) (*mcp.CallToolResult, writeOutput[addCommentToCardResult], error) {
		ccid, ambiguous, candidates, err := resolveAddCommentTarget(ctx, r, &in)
		if err != nil {
			return nil, writeOutput[addCommentToCardResult]{}, err
		}
		if ambiguous {
			return nil, writeOutput[addCommentToCardResult]{
				Result: &addCommentToCardResult{Ambiguous: true, Candidates: candidates},
			}, nil
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (addCommentToCardResult, error) {
				cm, cerr := r.client.CreateComment(writeCtx, favro.CreateCommentRequest{
					CardCommonID: ccid,
					Comment:      in.Comment,
				})
				if cerr != nil {
					return addCommentToCardResult{}, cerr
				}
				return addCommentToCardResult{Comment: &cm}, nil
			},
			func() string {
				return fmt.Sprintf("would post a comment on card %q", ccid)
			},
		)
		if err != nil {
			return nil, writeOutput[addCommentToCardResult]{}, err
		}
		return nil, out, nil
	})
}

// resolveAddCommentTarget returns the cardCommonID to comment on. If
// search_query was used and the top-2 scores are within
// addCommentAmbiguityMargin, returns ambiguous=true with the
// candidate list — caller should NOT proceed with the PUT.
//
// Returns errAddCommentIdentityRequired if zero / multiple identity
// flavors were set, errAddCommentSearchScopeRequired if search_query
// lacks a scope, errAddCommentNoMatch if search_query found nothing.
func resolveAddCommentTarget(ctx context.Context, r *Resolver, in *addCommentToCardInput) (cardCommonID string, ambiguous bool, candidates []SearchedCard, err error) {
	idCount := 0
	if in.CardCommonID != "" {
		idCount++
	}
	if in.CardID != "" {
		idCount++
	}
	if in.SequentialID > 0 {
		idCount++
	}
	if in.SearchQuery != "" {
		idCount++
	}
	if idCount != 1 {
		return "", false, nil, errAddCommentIdentityRequired
	}
	if in.CardCommonID != "" {
		return in.CardCommonID, false, nil, nil
	}
	if in.CardID != "" || in.SequentialID > 0 {
		card, err := r.fetchCardForIdentity(ctx, FullCardIdentity{CardID: in.CardID, SequentialID: in.SequentialID})
		if err != nil {
			return "", false, nil, err
		}
		return card.CardCommonID, false, nil, nil
	}
	return resolveAddCommentBySearch(ctx, r, in)
}

// resolveAddCommentBySearch runs favro_search_cards with limit=2 in
// the supplied scope, then either returns the top hit or surfaces an
// ambiguous response with both candidates.
func resolveAddCommentBySearch(ctx context.Context, r *Resolver, in *addCommentToCardInput) (string, bool, []SearchedCard, error) {
	scope, scopeID, ok := pickSearchScope(in.WidgetCommonID, in.CollectionID)
	if !ok {
		return "", false, nil, errAddCommentSearchScopeRequired
	}
	hits, _, err := r.SearchCards(ctx, in.SearchQuery, scope, scopeID, false, addCommentAmbiguityWindow, 0, false)
	if err != nil {
		return "", false, nil, err
	}
	if len(hits) == 0 {
		return "", false, nil, errAddCommentNoMatch
	}
	// Compare across the top-K window — a 3-way tie isn't safer
	// than a 2-way tie just because the third hit is in slot 2.
	for i := 1; i < len(hits); i++ {
		if (hits[0].Score - hits[i].Score) < addCommentAmbiguityMargin {
			return "", true, hits[:i+1], nil
		}
	}
	return hits[0].CardCommonID, false, nil, nil
}

// pickSearchScope encodes the "exactly one of widget_common_id /
// collection_id" requirement. Returns ok=false when zero or both are
// set so the caller can surface errAddCommentSearchScopeRequired.
func pickSearchScope(widgetCommonID, collectionID string) (SearchScope, string, bool) {
	switch {
	case widgetCommonID != "" && collectionID == "":
		return SearchScopeWidget, widgetCommonID, true
	case widgetCommonID == "" && collectionID != "":
		return SearchScopeCollection, collectionID, true
	default:
		return 0, "", false
	}
}
