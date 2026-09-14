package favroapi

import (
	"context"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListCardActivities returns one page of a card's activity history,
// newest first. cardID is the per-widget card id, not the common id.
func (c *Client) ListCardActivities(ctx context.Context, page int, requestID, cardID string, filter favro.ListActivitiesFilter) (favro.PageEnvelope[favro.Activity], error) {
	if cardID == "" {
		return favro.PageEnvelope[favro.Activity]{}, errMissingID
	}
	path := "/cards/" + url.PathEscape(cardID) + "/activities"
	return listPageQ[favro.Activity](ctx, c, path, filter.Values(), page, requestID)
}
