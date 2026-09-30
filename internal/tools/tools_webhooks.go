package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v3/internal/favro"
	"github.com/mmedum/favro-mcp/v3/internal/favroapi"
	"github.com/mmedum/favro-mcp/v3/internal/render"
)

const (
	listWebhooksToolName  = "favro_list_webhooks"
	deleteWebhookToolName = "favro_delete_webhook"
)

// listWebhooksInput narrows the listing to one widget.
//
// No page or request_id: /webhooks is the one Favro collection that
// answers with a bare array rather than a paginated envelope, so there
// is nothing to page through and offering the knob would be a lie.
type listWebhooksInput struct {
	WidgetCommonID string `json:"widget_common_id,omitempty" jsonschema:"only the webhooks of this widget; omit for every webhook in the organization"`
}

// listWebhooksOutput is the whole answer, because Favro returns the
// whole answer.
type listWebhooksOutput struct {
	Webhooks []favro.Webhook `json:"webhooks" jsonschema:"every outgoing webhook, or those of one widget"`
	Count    int             `json:"count" jsonschema:"how many came back; Favro does not paginate this endpoint, so this is all of them"`
}

// Summary renders the readable half.
func (o listWebhooksOutput) Summary() string {
	return fmt.Sprintf("%d %s (Favro returns them all; this endpoint does not paginate)%s",
		o.Count, render.Plural(o.Count, "webhook", "webhooks"), render.InlineList(o.Webhooks))
}

// deleteWebhookInput identifies one webhook.
type deleteWebhookInput struct {
	dryRunInput
	WebhookID string `json:"webhook_id" jsonschema:"the webhookId to delete, from favro_list_webhooks"`
}

func registerWebhooks(reg *registry, client *favroapi.Client) {
	addTool(reg, &mcp.Tool{
		Name: listWebhooksToolName,
		Description: "List the outgoing webhooks configured in the organization — the " +
			"addresses Favro posts card events to. Optional `widget_common_id` narrows " +
			"to one board. Favro returns every webhook at once — this is the one " +
			"collection it does not paginate — so there is no `page`. The signing " +
			"secret is deliberately NOT returned: it is what " +
			"authenticates deliveries to whoever consumes them. There is no tool to " +
			"create a webhook, because a webhook needs a receiver that outlives this " +
			"process and a stdio server is not one; use this to see what exists and " +
			"`favro_delete_webhook` to remove it. Read-only.",
		Annotations: readOnly("List Favro outgoing webhooks"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listWebhooksInput) (*mcp.CallToolResult, listWebhooksOutput, error) {
		hooks, err := client.ListWebhooks(ctx, favro.ListWebhooksFilter{WidgetCommonID: in.WidgetCommonID})
		if err != nil {
			return nil, listWebhooksOutput{}, err
		}
		return nil, listWebhooksOutput{Webhooks: hooks, Count: len(hooks)}, nil
	})

	addAsking(reg, &mcp.Tool{
		Name: deleteWebhookToolName,
		Description: "Delete an outgoing webhook by `webhook_id`. Favro stops posting card " +
			"events to that address immediately, and whatever was consuming them stops " +
			"receiving them — this server cannot tell you what that is, so confirm with " +
			"a human before removing a webhook you did not create. Resolve the id with " +
			"`favro_list_webhooks`. Pass `dry_run: true` to preview." + asksFirst,
		Annotations: mutating("Delete Favro webhook", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteWebhookInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		if err := confirmFirst(ctx, client.DryRun(writeCtx), func() (render.Question, error) {
			hook, err := findWebhook(ctx, client, in.WebhookID)
			return render.AskDeleteWebhook(in.WebhookID, hook.Name, hook.PostToURL), err
		}); err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, client.DeleteWebhook(writeCtx, in.WebhookID)
			},
			func() string {
				return fmt.Sprintf("would delete webhook %q; Favro would stop posting card events to its address", in.WebhookID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		return nil, out, nil
	})
}

// errWebhookNotFound is a webhook id the organization's list does not
// hold. Favro has no read for one webhook, so the question that names
// it reads them all.
var errWebhookNotFound = render.Sentinel(render.ClassNotFound,
	"favro_delete_webhook: no webhook in this organization has that webhook_id; list them with favro_list_webhooks")

// findWebhook is one webhook, found in the organization's list.
func findWebhook(ctx context.Context, client *favroapi.Client, id string) (favro.Webhook, error) {
	hooks, err := client.ListWebhooks(ctx, favro.ListWebhooksFilter{})
	if err != nil {
		return favro.Webhook{}, err
	}
	for _, h := range hooks {
		if h.WebhookID == id {
			return h, nil
		}
	}
	return favro.Webhook{}, errWebhookNotFound
}
