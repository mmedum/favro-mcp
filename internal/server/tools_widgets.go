package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listWidgetsToolName = "favro_list_widgets"
	getWidgetToolName   = "favro_get_widget"
)

// listWidgetsInput extends the standard pagination shape with an
// optional collection filter. listInput is embedded so the Page /
// RequestID field tags are defined in one place; the MCP SDK's
// jsonschema-go reflection flattens embedded struct fields by
// default.
type listWidgetsInput struct {
	listInput
	CollectionID string `json:"collection_id,omitempty" jsonschema:"optional Favro collection id to scope the listing to widgets in that collection; pass it on EVERY page when paginating, not just the first"`
	Archived     bool   `json:"archived,omitempty" jsonschema:"if true, include archived widgets in the result; default (false) hides them server-side"`
}

// getWidgetInput is the input for favro_get_widget.
type getWidgetInput struct {
	WidgetCommonID string `json:"widget_common_id" jsonschema:"the Favro widget id (the cross-widget widgetCommonId)"`
}

func registerWidgets(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listWidgetsToolName,
		Description: "List Favro widgets (boards) in the organization the API token is scoped to. " +
			"Pass `collection_id` to scope the result to a single collection. Pass " +
			"`archived: true` to include archived widgets. Returns one page; pass `page` " +
			"(1-indexed) plus the `request_id` from the prior response to retrieve " +
			"subsequent pages. Read-only.",
		Annotations: readOnly("List Favro widgets"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listWidgetsInput) (*mcp.CallToolResult, listOutput[favro.Widget], error) {
		env, err := client.ListWidgets(ctx, in.Page, in.RequestID, favro.ListWidgetsFilter{
			CollectionID: in.CollectionID,
			Archived:     in.Archived,
		})
		if err != nil {
			return nil, listOutput[favro.Widget]{}, err
		}
		return nil, newListOutput(env), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getWidgetToolName,
		Description: "Get a single Favro widget by its widgetCommonId. Read-only.",
		Annotations: readOnly("Get Favro widget"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getWidgetInput) (*mcp.CallToolResult, favro.Widget, error) {
		w, err := client.GetWidget(ctx, in.WidgetCommonID)
		if err != nil {
			return nil, favro.Widget{}, err
		}
		return nil, w, nil
	})
}
