package favroapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListCards returns one page of cards. Filters are optional; pass a
// zero-value favro.ListCardsFilter for an unfiltered org-wide listing.
//
// Callers that paginate must pass the same filter on every page;
// dropping a filter mid-pagination would silently switch the result
// set to an org-wide listing.
func (c *Client) ListCards(ctx context.Context, page int, requestID string, filter favro.ListCardsFilter) (favro.PageEnvelope[favro.Card], error) {
	return listPageQ[favro.Card](ctx, c, "/cards", filter.Values(), page, requestID)
}

// GetCard returns a single card by its per-widget CardID. Note this
// is the per-widget instance id, NOT the cross-widget CardCommonID:
// Favro's GET /cards/{id} endpoint 403s on a CardCommonID. To fetch
// a card known only by CardCommonID, call ListCards with the filter
// CardCommonID set (and Unique=true if cross-widget duplication is
// noise).
//
// Returns *NotFoundError if no such card exists in the active
// organization.
func (c *Client) GetCard(ctx context.Context, cardID string) (favro.Card, error) {
	return getByID[favro.Card](ctx, c, "/cards", cardID)
}

// GetCardWithDescriptionFormat fetches one card with the response's
// `detailedDescription` rendered in the requested format. Favro's
// default is "plaintext"; pass "markdown" for the form Phase 6's
// description editor tools require so the markdown stays
// edit-correct round-trip. Empty cardID short-circuits with
// errMissingID; *NotFoundError on 404. Empty format falls through
// to Favro's default (no `descriptionFormat` query param sent).
func (c *Client) GetCardWithDescriptionFormat(ctx context.Context, cardID, format string) (favro.Card, error) {
	if cardID == "" {
		return favro.Card{}, errMissingID
	}
	q := url.Values{}
	if format != "" {
		q.Set("descriptionFormat", format)
	}
	var out favro.Card
	if err := c.GetJSON(ctx, "/cards/"+url.PathEscape(cardID), q, &out); err != nil {
		return favro.Card{}, err
	}
	return out, nil
}

// CreateCard creates a new card. Returns the created favro.Card (Favro
// echoes the row back with cardId / cardCommonId / sequentialId /
// position assigned). Honors per-context WithDryRun and process-wide
// ForceDryRun via the wrapped PostJSON; in either case the call
// returns *DryRunRecord wrapped in ErrDryRun without touching the
// network.
func (c *Client) CreateCard(ctx context.Context, req favro.CreateCardRequest) (favro.Card, error) {
	if req.Name == "" {
		return favro.Card{}, fmt.Errorf("favro: card name is required")
	}
	var out favro.Card
	if err := c.doJSON(ctx, http.MethodPost, "/cards", descriptionFormatQuery(req.DescriptionFormat), req, &out); err != nil {
		return favro.Card{}, err
	}
	return out, nil
}

// descriptionFormatQuery renders the optional descriptionFormat
// query parameter shared by the card create / update / read paths.
// Returns nil when unset so callers send no query string at all.
func descriptionFormatQuery(format string) url.Values {
	if format == "" {
		return nil
	}
	return url.Values{"descriptionFormat": []string{format}}
}

// UpdateCard updates a card by its per-widget cardId. Returns the
// updated favro.Card. Empty cardID short-circuits with errMissingID;
// Favro errors propagate via the wrapped PutJSON, including
// *DryRunRecord-wrapping-ErrDryRun under dry-run.
func (c *Client) UpdateCard(ctx context.Context, cardID string, req favro.UpdateCardRequest) (favro.Card, error) {
	if cardID == "" {
		return favro.Card{}, errMissingID
	}
	var out favro.Card
	path := "/cards/" + url.PathEscape(cardID)
	if err := c.doJSON(ctx, http.MethodPut, path, descriptionFormatQuery(req.DescriptionFormat), req, &out); err != nil {
		return favro.Card{}, err
	}
	return out, nil
}

// ArchiveCard is a thin wrapper over UpdateCard that flips the
// archive flag on. Surfaces a dedicated favro_archive_card MCP
// tool — common LLM workflow worth its own one-shot entry point.
func (c *Client) ArchiveCard(ctx context.Context, cardID string) (favro.Card, error) {
	t := true
	return c.UpdateCard(ctx, cardID, favro.UpdateCardRequest{Archive: &t})
}

// UnarchiveCard is the symmetric wrapper that restores an archived
// card. Sends `archive: false` explicitly (not omitempty-elided)
// because Archive is a *bool so &false survives JSON encoding.
func (c *Client) UnarchiveCard(ctx context.Context, cardID string) (favro.Card, error) {
	f := false
	return c.UpdateCard(ctx, cardID, favro.UpdateCardRequest{Archive: &f})
}

// MoveCard relocates a card via UpdateCard. Empty cardID
// short-circuits with errMissingID; an empty favro.MoveCardRequest
// short-circuits with a typed error (silently succeeding on a
// nothing-to-do request would mask a caller bug).
func (c *Client) MoveCard(ctx context.Context, cardID string, req favro.MoveCardRequest) (favro.Card, error) {
	if cardID == "" {
		return favro.Card{}, errMissingID
	}
	if req.WidgetCommonID == "" && req.ColumnID == "" && req.LaneID == "" {
		return favro.Card{}, fmt.Errorf("favro: move requires at least one of widget_common_id, column_id, or lane_id")
	}
	return c.UpdateCard(ctx, cardID, favro.UpdateCardRequest{
		WidgetCommonID: req.WidgetCommonID,
		ColumnID:       req.ColumnID,
		LaneID:         req.LaneID,
		ListPosition:   req.ListPosition,
		SheetPosition:  req.SheetPosition,
		DragMode:       req.DragMode,
	})
}

// DeleteCard deletes a card. With everywhere=false (the common case)
// only the per-widget instance referenced by cardID is removed;
// other widgets sharing the same cardCommonId keep their copies.
// With everywhere=true the cardCommonId is purged across every
// widget — an irreversible op.
//
// Empty cardID short-circuits with errMissingID; *NotFoundError /
// other typed errors propagate via doJSON; *DryRunRecord wraps
// ErrDryRun under dry-run.
func (c *Client) DeleteCard(ctx context.Context, cardID string, everywhere bool) (favro.DeleteCardResponse, error) {
	if cardID == "" {
		return nil, errMissingID
	}
	q := url.Values{}
	if everywhere {
		q.Set("everywhere", "true")
	}
	var out favro.DeleteCardResponse
	if err := c.doJSON(ctx, http.MethodDelete, "/cards/"+url.PathEscape(cardID), q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
