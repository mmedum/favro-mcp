package favro

import (
	"context"
	"net/url"
	"strconv"
)

// Card is a Favro card. Cards have three identifier flavors that
// matter to callers:
//
//   - CardCommonID is the global card identity. A card that lives on
//     multiple widgets shares one CardCommonID across all of them.
//   - CardID is the per-widget instance id. Each appearance of the
//     same logical card on a different widget gets its own CardID.
//   - SequentialID is the human-readable per-organization counter
//     (e.g. 123). Combined with SequentialIDPrefix it forms the
//     "BSC-123" tag people use in conversation.
//
// LLM-driven workflows generally start from a name or a "BSC-123"
// reference; the resolver tools that turn those into CardCommonIDs
// land in Phase 4. Phase 3.6 just exposes raw read access keyed by
// CardCommonID.
//
// CustomFieldsValues, Tags, and Assignments are kept as raw shapes
// for now — Phase 4 (favro_get_card_full) will dereference tag IDs
// to names and resolve assignment user IDs. Fields outside this
// struct are ignored on decode (forward-compatible).
type Card struct {
	CardID              string `json:"cardId"`
	CardCommonID        string `json:"cardCommonId"`
	OrganizationID      string `json:"organizationId,omitempty"`
	WidgetCommonID      string `json:"widgetCommonId,omitempty"`
	ColumnID            string `json:"columnId,omitempty"`
	LaneID              string `json:"laneId,omitempty"`
	ParentCardID        string `json:"parentCardId,omitempty"`
	Name                string `json:"name"`
	DetailedDescription string `json:"detailedDescription,omitempty"`
	SequentialID        int    `json:"sequentialId,omitempty"`
	SequentialIDPrefix  string `json:"sequentialIdPrefix,omitempty"`
	Position            int    `json:"position,omitempty"`
	ListPosition        int    `json:"listPosition,omitempty"`
	IsArchived          bool   `json:"archived,omitempty"`
	StartDate           string `json:"startDate,omitempty"`
	DueDate             string `json:"dueDate,omitempty"`
	// Tag IDs only — resolution to tag names lives in Phase 4.
	Tags []string `json:"tags,omitempty"`
	// Assignments are {userId, completed} pairs; kept as raw maps
	// to stay forward-compatible while the phase plan defers
	// assignee resolution to Phase 4.
	Assignments []CardAssignment `json:"assignments,omitempty"`
	// CustomFieldsValues land in Phase 4 long-tail; left out of the
	// typed projection deliberately so the field set stays small
	// and obvious for LLM consumption.
	NumComments           int `json:"numComments,omitempty"`
	TotalAttachmentsCount int `json:"totalAttachmentsCount,omitempty"`
}

// CardAssignment is one entry in Card.Assignments.
type CardAssignment struct {
	UserID    string `json:"userId"`
	Completed bool   `json:"completed,omitempty"`
}

// ListCardsFilter bundles the filter knobs Favro's /cards endpoint
// accepts. All fields are optional; an empty filter requests every
// card the token can see in the organization (callers should expect
// many pages).
//
// Plan §6 calls out widget/collection/cardCommonId/sequentialId as
// the supported filters; other knobs Favro documents (todoListId,
// archived flag, etc.) can be added when a workflow tool needs them.
type ListCardsFilter struct {
	WidgetCommonID string
	CollectionID   string
	CardCommonID   string
	// SequentialID is the bare integer (e.g. 123 for "BSC-123").
	// Zero means "no filter"; Favro itself does not have card
	// sequentialId 0, so the sentinel is unambiguous.
	SequentialID int
	// Unique requests one row per CardCommonID rather than one row
	// per widget instance. Useful when the caller is searching by
	// name and doesn't care about cross-widget duplication.
	Unique bool
}

// values returns the filter as url.Values; empty fields are omitted.
func (f ListCardsFilter) values() url.Values {
	q := url.Values{}
	if f.WidgetCommonID != "" {
		q.Set("widgetCommonId", f.WidgetCommonID)
	}
	if f.CollectionID != "" {
		q.Set("collectionId", f.CollectionID)
	}
	if f.CardCommonID != "" {
		q.Set("cardCommonId", f.CardCommonID)
	}
	if f.SequentialID > 0 {
		q.Set("cardSequentialId", strconv.Itoa(f.SequentialID))
	}
	if f.Unique {
		q.Set("unique", "true")
	}
	return q
}

// ListCards returns one page of cards. Filters are optional; pass a
// zero-value ListCardsFilter for an unfiltered org-wide listing.
//
// Callers that paginate must pass the same filter on every page;
// dropping a filter mid-pagination would silently switch the result
// set to an org-wide listing.
func (c *Client) ListCards(ctx context.Context, page int, requestID string, filter ListCardsFilter) (PageEnvelope[Card], error) {
	return listPageQ[Card](ctx, c, "/cards", filter.values(), page, requestID)
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
func (c *Client) GetCard(ctx context.Context, cardID string) (Card, error) {
	return getByID[Card](ctx, c, "/cards", cardID)
}
