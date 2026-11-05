package favro

import (
	"encoding/json"
	"net/url"
)

// Webhook is one outgoing webhook Favro has registered: an address it
// posts card events to.
//
// **The signing secret is deliberately not modeled.** Favro returns a
// `secret` on every read, and it is what signs the X-Favro-Webhook
// header on every delivery — anyone holding it can forge an event to
// whoever is consuming that webhook. The strongest way not to leak a
// value to a model is not to parse it, so this type has no field for
// it and no tool can surface what no type holds (D-52). Decoding is
// tolerant, so the field is simply dropped. Only the reference's
// example shows the field: no live read has returned a webhook.
// https://favro.com/developer/#get-all-webhooks
type Webhook struct {
	WebhookID string `json:"webhookId"`
	// WidgetCommonID is the widget whose events are posted.
	WidgetCommonID string `json:"widgetCommonId,omitempty"`
	Name           string `json:"name,omitempty"`
	// PostToURL is the address Favro delivers to. Not a secret — it is
	// what identifies the webhook to a person deciding whether to
	// remove it.
	PostToURL string         `json:"postToUrl,omitempty"`
	Options   WebhookOptions `json:"options,omitempty"`
}

// WebhookOptions narrows what a webhook fires on.
//
// The reference disagrees with itself on the column scope: the field
// table documents `columnIds` as an array, and every example response
// shows a single `columnId` string. Both are accepted on read — see
// ColumnIDs — and no live read has settled which one Favro sends.
// https://favro.com/developer/#webhook-options
type WebhookOptions struct {
	// RawColumnIDs holds whichever of the two shapes arrived. Use
	// ColumnIDs to read it.
	RawColumnIDs json.RawMessage `json:"columnIds,omitempty"`
	// RawColumnID holds the singular spelling the examples show.
	RawColumnID string `json:"columnId,omitempty"`
	// Notifications are the event names, e.g. "Card created". Empty
	// means every notification.
	Notifications []string `json:"notifications,omitempty"`
}

// ColumnIDs returns the columns this webhook is scoped to, reading
// whichever spelling Favro sent. The documented plural is preferred;
// the singular the examples show is accepted after it.
func (o WebhookOptions) ColumnIDs() []string {
	if len(o.RawColumnIDs) > 0 {
		var many []string
		if err := json.Unmarshal(o.RawColumnIDs, &many); err == nil {
			return many
		}
		// A single string under the plural key is taken too, rather than
		// dropping the scope.
		var one string
		if err := json.Unmarshal(o.RawColumnIDs, &one); err == nil && one != "" {
			return []string{one}
		}
	}
	if o.RawColumnID != "" {
		return []string{o.RawColumnID}
	}
	return nil
}

// ListWebhooksFilter narrows GET /webhooks.
type ListWebhooksFilter struct {
	// WidgetCommonID restricts to the webhooks of one widget. Optional;
	// empty lists every webhook in the organization.
	WidgetCommonID string
}

// Values renders the filter as query parameters.
func (f ListWebhooksFilter) Values() url.Values {
	q := url.Values{}
	if f.WidgetCommonID != "" {
		q.Set("widgetCommonId", f.WidgetCommonID)
	}
	return q
}
