package favro

import (
	"context"
	"encoding/json"
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
// Tag IDs / assignment user IDs / custom-field IDs are kept as raw
// IDs here; favro_get_card_full composes resolver caches to
// dereference them into human names. Fields outside this struct
// are ignored on decode (forward-compatible).
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
	// Position / ListPosition are fractional — Favro uses
	// subdivisions like 3.125 to slot a card between two siblings
	// without renumbering everything around it. Decoding as int 400s
	// the JSON unmarshal on any non-integer value.
	Position     float64 `json:"position,omitempty"`
	ListPosition float64 `json:"listPosition,omitempty"`
	IsArchived   bool    `json:"archived,omitempty"`
	// IsLane signals that this "card" is the meta-row representing
	// a Favro lane (a horizontal grouping inside a board); regular
	// cards have IsLane=false.
	IsLane    bool   `json:"isLane,omitempty"`
	StartDate string `json:"startDate,omitempty"`
	DueDate   string `json:"dueDate,omitempty"`
	// CreatedByUserID + CreatedAt are the audit-trail fields Favro
	// surfaces on every card payload. Kept as a string for CreatedAt
	// because Favro's timestamp serialization isn't strictly RFC3339
	// across all responses (verified live: occasional unfortunate
	// formats appear); a typed time.Time would silently 400 the
	// decode on those rows.
	CreatedByUserID string `json:"createdByUserId,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
	// Tag IDs only — see favro_get_card_full for name resolution.
	Tags []string `json:"tags,omitempty"`
	// Assignments are {userId, completed} pairs.
	Assignments []CardAssignment `json:"assignments,omitempty"`
	// CustomFieldsValues holds the per-card values for the org's
	// custom fields. Each entry references the field by ID; the
	// caller dereferences against the org-global custom-field list to
	// get name + type + (for select-flavored fields) the option name.
	// Value is kept as json.RawMessage because the shape varies by
	// field type; CustomFieldItemIDs / Total carry the typed flavors
	// callers commonly need without requiring a parse.
	CustomFieldsValues []CardCustomFieldValue `json:"customFieldsValues,omitempty"`
	// TasksTotal / TasksDone are the on-card checklist counts. Lets
	// the LLM see "3 of 7 tasks remaining" without an extra /tasks
	// fetch. The full /tasks + /tasklists resources are not yet
	// modeled.
	TasksTotal int `json:"tasksTotal,omitempty"`
	TasksDone  int `json:"tasksDone,omitempty"`
	// Attachments is the list of file attachments on the card.
	// FavroAttachments is the list of *linked* favro objects (other
	// cards / widgets — Favro's intra-product cross-link feature).
	Attachments      []CardAttachment      `json:"attachments,omitempty"`
	FavroAttachments []CardFavroAttachment `json:"favroAttachments,omitempty"`
	// TimeOnBoard is the running time-since-creation aggregate Favro
	// surfaces, with an isStopped flag (true on archived cards).
	// TimeOnColumns is keyed by columnId and holds the per-column
	// cumulative milliseconds as a bare number (verified live —
	// Favro returns scalars here, not the same object shape as
	// timeOnBoard). Both are useful for "how long has this been
	// blocked" queries.
	TimeOnBoard   CardTimeOnBoard  `json:"timeOnBoard,omitempty"`
	TimeOnColumns map[string]int64 `json:"timeOnColumns,omitempty"`

	NumComments           int `json:"numComments,omitempty"`
	TotalAttachmentsCount int `json:"totalAttachmentsCount,omitempty"`
}

// CardAttachment is one file attachment on a Favro card. Fields are
// kept minimal — Favro extends the shape with metadata (size,
// content-type) but only the name + URL are stable across responses.
type CardAttachment struct {
	Name    string `json:"name"`
	FileURL string `json:"fileURL,omitempty"`
}

// CardFavroAttachment is one intra-Favro link from this card to
// another Favro object (another card, a widget, etc.). Type
// distinguishes the kind; itemCommonID points at the linked object.
type CardFavroAttachment struct {
	Type         string `json:"type,omitempty"`
	ItemCommonID string `json:"itemCommonId,omitempty"`
}

// CardTimeOnBoard is a time aggregate Favro returns in two places
// on a Card: as the top-level TimeOnBoard (time-since-creation), and
// as the value in TimeOnColumns (time spent in the keyed column).
// IsStopped is true on archived cards (the timer is paused).
type CardTimeOnBoard struct {
	Time      int64 `json:"time,omitempty"`
	IsStopped bool  `json:"isStopped,omitempty"`
}

// CardCustomFieldValue is one per-card custom-field value entry.
// Favro returns different shapes per field type; the type-discriminated
// fields below cover the common kinds:
//
//   - Text/Link: Value is a JSON string.
//   - Number/Rating: Value is a JSON number.
//   - Date/Date created: Value is an ISO-8601 string.
//   - Checkbox/Voting: Value is a JSON bool.
//   - Single select: CustomFieldItemIDs holds one ID referencing
//     CustomField.CustomFieldItems.
//   - Multiple select: CustomFieldItemIDs holds N IDs.
//   - Members: Value or a separate field carries user IDs.
//   - Timeline: Value carries {startDate, dueDate}.
//
// Total is the running sum surfaced for Rating-style fields. Fields
// outside this struct are ignored on decode (forward-compatible).
type CardCustomFieldValue struct {
	CustomFieldID      string          `json:"customFieldId"`
	Value              json.RawMessage `json:"value,omitempty"`
	CustomFieldItemIDs []string        `json:"customFieldItemIds,omitempty"`
	Total              float64         `json:"total,omitempty"`
}

// CardAssignment is one entry in Card.Assignments.
type CardAssignment struct {
	UserID    string `json:"userId"`
	Completed bool   `json:"completed,omitempty"`
}

// ListCardsFilter bundles the filter knobs Favro's /cards endpoint
// accepts. Favro requires at least one of widgetCommonId,
// collectionId, cardCommonId, cardSequentialId, or todoList — an
// otherwise-empty filter returns HTTP 400 (verified live in Phase
// 4.3). All fields are optional individually; the caller is
// responsible for setting at least one of the five required ones.
type ListCardsFilter struct {
	WidgetCommonID string
	CollectionID   string
	CardCommonID   string
	// SequentialID is the bare integer (e.g. 123 for "BSC-123").
	// Zero means "no filter"; Favro itself does not have card
	// sequentialId 0, so the sentinel is unambiguous.
	SequentialID int
	// ColumnID restricts to one column inside the scoped widget /
	// collection.
	ColumnID string
	// TodoList, when true, scopes to the authenticated user's
	// personal todo list (Favro's user-private tasklist).
	TodoList bool
	// Archived, when true, includes archived cards in the result;
	// when false (default) Favro filters them server-side. Replaces
	// Phase 4.3 search.go's client-side filter.
	Archived bool
	// Unique requests one row per CardCommonID rather than one row
	// per widget instance. Useful when the caller is searching by
	// name and doesn't care about cross-widget duplication.
	Unique bool
	// DescriptionFormat selects "plaintext" (default) or "markdown"
	// for Card.DetailedDescription. Empty means Favro's default.
	// Phase 4.3 search.go and Phase 6 description editors require
	// "markdown" so the markdown stripper / surgical-edit logic
	// sees the format it expects.
	DescriptionFormat string
}

// Values returns the filter as url.Values; empty fields are
// omitted. Exported so callers that paginate via favro.Paginate
// (e.g. internal/server/search.go) can re-use the filter encoding.
func (f ListCardsFilter) Values() url.Values {
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
	if f.ColumnID != "" {
		q.Set("columnId", f.ColumnID)
	}
	if f.TodoList {
		q.Set("todoList", "true")
	}
	if f.Archived {
		q.Set("archived", "true")
	}
	if f.Unique {
		q.Set("unique", "true")
	}
	if f.DescriptionFormat != "" {
		q.Set("descriptionFormat", f.DescriptionFormat)
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
	return listPageQ[Card](ctx, c, "/cards", filter.Values(), page, requestID)
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
