package favroapi

import (
	"context"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
)

// ListWebhooks returns the outgoing webhooks configured in the
// organization, optionally narrowed to one widget.
//
// **It does not paginate.** Every other Favro collection answers with
// the `{entities, page, pages, requestId}` envelope; /webhooks answers
// with a bare JSON array, as the reference says: "The response will be
// an array of configured webhooks". Checked by the live step
// "list a widget's webhooks", which has only ever read `[]`, so a
// populated row's shape rests on fixtures.
// https://favro.com/developer/#get-all-webhooks
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
// half exists — see testdata/api-coverage.tsv. Only a dry run has
// reached it live, in the live step "delete a webhook, dry run", since
// no tool creates a webhook to delete.
// https://favro.com/developer/#delete-a-webhook
func (c *Client) DeleteWebhook(ctx context.Context, webhookID string) error {
	return deleteByID(ctx, c, "/webhooks", webhookID)
}
