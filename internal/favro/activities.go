package favro

import (
	"context"
	"net/url"
)

// Activity is one entry in a card's audit trail. Which fields are
// populated depends on Type: a column move fills ColumnID /
// ColumnName, a custom-field edit fills CustomFieldName /
// CustomFieldValue, a comment fills CommentID, and so on. Fields
// outside this struct are ignored on decode (forward-compatible).
type Activity struct {
	// Type is the kind of change (e.g. "assigned"). Left as a plain
	// string — Favro does not document the closed set, and a typed
	// enum would silently mask new values.
	Type string `json:"type,omitempty"`
	// Source is the notification category: "news", "follow", or
	// both.
	Source string `json:"source,omitempty"`

	CardID       string `json:"cardId,omitempty"`
	CardCommonID string `json:"cardCommonId,omitempty"`
	// CardCommonKey is the key Favro's example payloads use where the
	// documented field table says cardCommonId. Both are decoded
	// because the docs contradict themselves and no live payload has
	// been captured to settle it; prefer CommonID.
	CardCommonKey string `json:"cardCommonKey,omitempty"`
	CardName      string `json:"cardName,omitempty"`

	OrganizationID string `json:"organizationId,omitempty"`
	WidgetCommonID string `json:"widgetCommonId,omitempty"`
	WidgetName     string `json:"widgetName,omitempty"`
	ColumnID       string `json:"columnId,omitempty"`
	ColumnName     string `json:"columnName,omitempty"`

	CustomFieldName  string `json:"customFieldName,omitempty"`
	CustomFieldValue string `json:"customFieldValue,omitempty"`
	TaskName         string `json:"taskName,omitempty"`
	CommentID        string `json:"commentId,omitempty"`

	// Time is when the change happened, and ByUserID who made it.
	// Time is a string for the same reason as Card.CreatedAt: Favro's
	// timestamp serialization is not uniformly RFC3339 and a typed
	// time.Time would fail the whole decode on an odd row.
	Time     string `json:"time,omitempty"`
	ByUserID string `json:"byUserId,omitempty"`
}

// CommonID returns the card's common id, reconciling the two wire
// keys Favro's docs disagree on.
func (a Activity) CommonID() string {
	if a.CardCommonID != "" {
		return a.CardCommonID
	}
	return a.CardCommonKey
}

// ListActivitiesFilter bundles the optional time-window parameters
// for /cards/{cardId}/activities. Both bounds are ISO-8601 strings;
// empty means unbounded.
type ListActivitiesFilter struct {
	Since string
	Until string
}

// Values returns the filter as url.Values; empty fields are omitted.
func (f ListActivitiesFilter) Values() url.Values {
	q := url.Values{}
	if f.Since != "" {
		q.Set("since", f.Since)
	}
	if f.Until != "" {
		q.Set("until", f.Until)
	}
	return q
}

// ListCardActivities returns one page of a card's activity history,
// newest first. cardID is the per-widget card id, not the common id.
func (c *Client) ListCardActivities(ctx context.Context, page int, requestID, cardID string, filter ListActivitiesFilter) (PageEnvelope[Activity], error) {
	if cardID == "" {
		return PageEnvelope[Activity]{}, errMissingID
	}
	path := "/cards/" + url.PathEscape(cardID) + "/activities"
	return listPageQ[Activity](ctx, c, path, filter.Values(), page, requestID)
}
