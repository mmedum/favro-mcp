package favro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// GetCardWithDescriptionFormat fetches one card with the response's
// `detailedDescription` rendered in the requested format. Favro's
// default is "plaintext"; pass "markdown" for the form Phase 6's
// description editor tools require so the markdown stays
// edit-correct round-trip. Empty cardID short-circuits with
// errMissingID; *NotFoundError on 404. Empty format falls through
// to Favro's default (no `descriptionFormat` query param sent).
func (c *Client) GetCardWithDescriptionFormat(ctx context.Context, cardID, format string) (Card, error) {
	if cardID == "" {
		return Card{}, errMissingID
	}
	q := url.Values{}
	if format != "" {
		q.Set("descriptionFormat", format)
	}
	var out Card
	if err := c.GetJSON(ctx, "/cards/"+url.PathEscape(cardID), q, &out); err != nil {
		return Card{}, err
	}
	return out, nil
}

// CreateCardRequest is the body for POST /cards. Name is the only
// required field; widgetCommonId pins the card to a board (omit and
// the card lands on the authenticated user's todo list). The full
// API surface includes tasklists / customFields / dependencies /
// favroAttachments — Phase 5.3 omits them; Phase 5.5 adds simple
// custom fields, Phase 7 covers tasklists + attachments.
//
// Tags and TagIDs are both accepted by Favro: Tags are tag *names*
// (Favro auto-creates missing ones) and TagIDs are existing tagIds.
// The MCP create_card tool exposes only tag_ids — auto-create from
// name is the kind of typo amplifier the plan §6 design avoids; the
// "add tag by name with hard-fail on unknown" UX lives in Phase 6's
// favro_add_tag_to_card.
// ListPosition / SheetPosition (on this and UpdateCardRequest) are
// JSON numbers — verified live Phase 5.3: string values, even numeric
// ones, return HTTP 400 "Unexpected value of listPosition". 0 is the
// top of the column; a value larger than the current max bumps to
// the bottom; a fractional value (e.g. 3.5) slots between two
// siblings without renumbering. Pointer typing distinguishes
// "absent" from "0".
type CreateCardRequest struct {
	Name                string   `json:"name"`
	WidgetCommonID      string   `json:"widgetCommonId,omitempty"`
	ColumnID            string   `json:"columnId,omitempty"`
	LaneID              string   `json:"laneId,omitempty"`
	ParentCardID        string   `json:"parentCardId,omitempty"`
	DetailedDescription string   `json:"detailedDescription,omitempty"`
	ListPosition        *float64 `json:"listPosition,omitempty"`
	SheetPosition       *float64 `json:"sheetPosition,omitempty"`
	AssignmentIDs       []string `json:"assignmentIds,omitempty"`
	Tags                []string `json:"tags,omitempty"`
	TagIDs              []string `json:"tagIds,omitempty"`
	StartDate           string   `json:"startDate,omitempty"`
	DueDate             string   `json:"dueDate,omitempty"`
}

// CreateCard creates a new card. Returns the created Card (Favro
// echoes the row back with cardId / cardCommonId / sequentialId /
// position assigned). Honors per-context WithDryRun and process-wide
// ForceDryRun via the wrapped PostJSON; in either case the call
// returns *DryRunRecord wrapped in ErrDryRun without touching the
// network.
func (c *Client) CreateCard(ctx context.Context, req CreateCardRequest) (Card, error) {
	if req.Name == "" {
		return Card{}, fmt.Errorf("favro: card name is required")
	}
	var out Card
	if err := c.PostJSON(ctx, "/cards", req, &out); err != nil {
		return Card{}, err
	}
	return out, nil
}

// UpdateCardRequest is the body for PUT /cards/{cardId}. Every field
// is optional; absent ones are left untouched. Pointer types appear
// where "absent" must be distinguishable from a zero value:
//
//   - Archive: nil = don't touch, &true = archive, &false = unarchive.
//   - CompleteAssignments: nil = don't touch, &true/&false = mark
//     all assignments accordingly.
//
// Tag mutations come in two flavors: AddTags / RemoveTags (by name,
// auto-create on add) and AddTagIDs / RemoveTagIDs (by tagId,
// strict). The MCP update_card tool exposes only the *_tag_ids
// flavors — Phase 6's favro_add_tag_to_card handles the by-name path
// with hard-fail-on-unknown semantics.
//
// DragMode is "commit" (default — Favro re-positions cards around
// the moved one and updates listPosition) or "move" (no
// repositioning). Only relevant when ListPosition / ColumnID is set.
//
// ListPosition / SheetPosition are JSON numbers — see CreateCardRequest
// for the wire contract. A column move that omits listPosition is a
// silent 200-empty-body no-op (verified live Phase 5.3); MoveCard
// surfaces this in its method docs.
//
// CustomFields carries per-card custom-field updates (Phase 5.5
// added). The MCP `favro_set_card_custom_field` convenience tool
// builds a single-element slice for simple-type fields; long-tail
// types (Members, Status, Multi-select, Rating, Link) ship in
// Phase 7. Tasklists / attachment removal stay deferred.
type UpdateCardRequest struct {
	Name                string                  `json:"name,omitempty"`
	DetailedDescription string                  `json:"detailedDescription,omitempty"`
	WidgetCommonID      string                  `json:"widgetCommonId,omitempty"`
	ColumnID            string                  `json:"columnId,omitempty"`
	LaneID              string                  `json:"laneId,omitempty"`
	ParentCardID        string                  `json:"parentCardId,omitempty"`
	DragMode            string                  `json:"dragMode,omitempty"`
	ListPosition        *float64                `json:"listPosition,omitempty"`
	SheetPosition       *float64                `json:"sheetPosition,omitempty"`
	AddAssignmentIDs    []string                `json:"addAssignmentIds,omitempty"`
	RemoveAssignmentIDs []string                `json:"removeAssignmentIds,omitempty"`
	CompleteAssignments *bool                   `json:"completeAssignments,omitempty"`
	AddTags             []string                `json:"addTags,omitempty"`
	RemoveTags          []string                `json:"removeTags,omitempty"`
	AddTagIDs           []string                `json:"addTagIds,omitempty"`
	RemoveTagIDs        []string                `json:"removeTagIds,omitempty"`
	StartDate           string                  `json:"startDate,omitempty"`
	DueDate             string                  `json:"dueDate,omitempty"`
	Archive             *bool                   `json:"archive,omitempty"`
	CustomFields        []CardCustomFieldUpdate `json:"customFields,omitempty"`
}

// CardCustomFieldUpdate is one entry in UpdateCardRequest.CustomFields.
// Mirrors the read-shape CardCustomFieldValue but for writes. Value
// is `any` because the Favro wire shape is type-discriminated:
//
//   - Text / Link: JSON string.
//   - Number / Rating: JSON number.
//   - Date: ISO-8601 string.
//   - Checkbox / Voting: JSON bool.
//   - Single select / Multiple select: omit Value; set
//     CustomFieldItemIDs (a single-element slice for single-select).
//
// The MCP `favro_set_card_custom_field` convenience tool resolves
// the field type via the resolver cache and builds the right
// entry; direct callers can also build it manually.
type CardCustomFieldUpdate struct {
	CustomFieldID      string   `json:"customFieldId"`
	Value              any      `json:"value,omitempty"`
	CustomFieldItemIDs []string `json:"customFieldItemIds,omitempty"`
}

// UpdateCard updates a card by its per-widget cardId. Returns the
// updated Card. Empty cardID short-circuits with errMissingID;
// Favro errors propagate via the wrapped PutJSON, including
// *DryRunRecord-wrapping-ErrDryRun under dry-run.
func (c *Client) UpdateCard(ctx context.Context, cardID string, req UpdateCardRequest) (Card, error) {
	if cardID == "" {
		return Card{}, errMissingID
	}
	var out Card
	if err := c.PutJSON(ctx, "/cards/"+url.PathEscape(cardID), req, &out); err != nil {
		return Card{}, err
	}
	return out, nil
}

// ArchiveCard is a thin wrapper over UpdateCard that flips the
// archive flag on. Surfaces a dedicated favro_archive_card MCP
// tool — common LLM workflow worth its own one-shot entry point.
func (c *Client) ArchiveCard(ctx context.Context, cardID string) (Card, error) {
	t := true
	return c.UpdateCard(ctx, cardID, UpdateCardRequest{Archive: &t})
}

// UnarchiveCard is the symmetric wrapper that restores an archived
// card. Sends `archive: false` explicitly (not omitempty-elided)
// because Archive is a *bool so &false survives JSON encoding.
func (c *Client) UnarchiveCard(ctx context.Context, cardID string) (Card, error) {
	f := false
	return c.UpdateCard(ctx, cardID, UpdateCardRequest{Archive: &f})
}

// MoveCardRequest captures the move-card knobs. At least one of
// WidgetCommonID / ColumnID / LaneID must be set; an empty request
// would PUT a no-op and silently succeed.
//
// **Wire-contract gotcha (verified live in Phase 5.3):** a column
// move (ColumnID set) that omits ListPosition silently no-ops —
// Favro returns HTTP 200 with an empty body and the card stays put.
// Callers that move between columns must set ListPosition. 0 is the
// top; a number larger than the column's current max sends the card
// to the bottom; fractional values slot between siblings.
//
// DragMode defaults to Favro's "commit" when empty (cards around
// the destination position re-shuffle); pass "move" to leave
// surrounding listPositions untouched.
type MoveCardRequest struct {
	WidgetCommonID string
	ColumnID       string
	LaneID         string
	ListPosition   *float64
	SheetPosition  *float64
	DragMode       string
}

// MoveCard relocates a card via UpdateCard. Empty cardID
// short-circuits with errMissingID; an empty MoveCardRequest
// short-circuits with a typed error (silently succeeding on a
// nothing-to-do request would mask a caller bug).
func (c *Client) MoveCard(ctx context.Context, cardID string, req MoveCardRequest) (Card, error) {
	if cardID == "" {
		return Card{}, errMissingID
	}
	if req.WidgetCommonID == "" && req.ColumnID == "" && req.LaneID == "" {
		return Card{}, fmt.Errorf("favro: move requires at least one of widget_common_id, column_id, or lane_id")
	}
	return c.UpdateCard(ctx, cardID, UpdateCardRequest{
		WidgetCommonID: req.WidgetCommonID,
		ColumnID:       req.ColumnID,
		LaneID:         req.LaneID,
		ListPosition:   req.ListPosition,
		SheetPosition:  req.SheetPosition,
		DragMode:       req.DragMode,
	})
}

// DeleteCardResponse is the body Favro returns from DELETE /cards/{id}:
// a bare JSON array of the per-widget cardIds that were removed
// (verified live in Phase 5.3 — the docs hint at `{"cardIds":[...]}`
// but the wire shape is the bare array). With everywhere=false this
// is a single-element slice (the targeted instance); with
// everywhere=true it carries every widget instance the cardCommonId
// had.
type DeleteCardResponse []string

// DeleteCard deletes a card. With everywhere=false (the common case)
// only the per-widget instance referenced by cardID is removed;
// other widgets sharing the same cardCommonId keep their copies.
// With everywhere=true the cardCommonId is purged across every
// widget — an irreversible op.
//
// Empty cardID short-circuits with errMissingID; *NotFoundError /
// other typed errors propagate via doJSON; *DryRunRecord wraps
// ErrDryRun under dry-run.
func (c *Client) DeleteCard(ctx context.Context, cardID string, everywhere bool) (DeleteCardResponse, error) {
	if cardID == "" {
		return nil, errMissingID
	}
	q := url.Values{}
	if everywhere {
		q.Set("everywhere", "true")
	}
	var out DeleteCardResponse
	if err := c.doJSON(ctx, http.MethodDelete, "/cards/"+url.PathEscape(cardID), q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
