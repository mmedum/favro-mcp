package favroapi

import (
	"context"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// ListWebhooks returns the outgoing webhooks configured in the
// organization, optionally narrowed to one widget.
//
// **It does not paginate, and that is not an oversight.** Every other
// Favro collection answers with the `{entities, page, pages, requestId}`
// envelope; /webhooks answers with a bare JSON array. The reference
// shows it — "The response will be an array of configured webhooks" —
// and a live call against an organization with none returns `[]`, which
// is what caught this: the first version of this method went through
// the shared paginated helper and failed to decode against the real
// API while passing against a fixture written to match the assumption.
// §2.1, again.
func (c *Client) ListWebhooks(ctx context.Context, filter favro.ListWebhooksFilter) ([]favro.Webhook, error) {
	var out []favro.Webhook
	if err := c.GetJSON(ctx, "/webhooks", filter.Values(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteWebhook removes one outgoing webhook by id.
//
// The counterpart POST is deliberately absent: registering a webhook
// points Favro at a URL that has to outlive this process, and a stdio
// server started and stopped by a host can never be that URL. Removing
// one somebody else registered needs no receiver, which is why this
// half exists — see testdata/api-coverage.tsv.
func (c *Client) DeleteWebhook(ctx context.Context, webhookID string) error {
	return deleteByID(ctx, c, "/webhooks", webhookID)
}
