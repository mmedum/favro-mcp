package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const listCardActivitiesToolName = "favro_list_card_activities"

// listCardActivitiesInput scopes to one card and, optionally, a time
// window. Both bounds are ISO-8601.
type listCardActivitiesInput struct {
	listInput
	CardID string `json:"card_id" jsonschema:"the per-widget cardId whose history to read (not the cardCommonId); required on every page"`
	Since  string `json:"since,omitempty" jsonschema:"only activities after this ISO 8601 timestamp"`
	Until  string `json:"until,omitempty" jsonschema:"only activities before this ISO 8601 timestamp"`
}

func registerActivities(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listCardActivitiesToolName,
		Description: "Read a Favro card's activity history — who changed what, and when. " +
			"Answers \"when did this move to Done\", \"who reassigned this\", \"what changed " +
			"since Friday\". `card_id` is the per-widget cardId, NOT the cardCommonId. " +
			"Narrow with `since` / `until` (ISO 8601) rather than paging through everything. " +
			"Which fields an entry carries depends on its `type`: a column move fills " +
			"column_id / column_name, a custom-field edit fills custom_field_name / " +
			"custom_field_value, a comment fills comment_id. `by_user_id` resolves to a " +
			"name via favro_get_user. Returns one page; pass `page` plus the prior " +
			"`request_id` (and `card_id` again) for later pages. Read-only.",
		Annotations: readOnly("List Favro card activities"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCardActivitiesInput) (*mcp.CallToolResult, listOutput[favro.Activity], error) {
		env, err := client.ListCardActivities(ctx, in.Page, in.RequestID, in.CardID, favro.ListActivitiesFilter{
			Since: in.Since,
			Until: in.Until,
		})
		if err != nil {
			return nil, listOutput[favro.Activity]{}, err
		}
		return nil, newListOutput(env), nil
	})
}
