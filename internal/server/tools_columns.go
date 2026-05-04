package server

import (
	"cmp"
	"context"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listColumnsToolName = "favro_list_columns"
	getColumnToolName   = "favro_get_column"
)

// listColumnsInput extends the standard pagination shape with the
// required widget filter. Unlike widgets (where `collection_id` is
// optional), Favro's /columns endpoint rejects unfiltered listings
// with HTTP 400, so the field is mandatory.
type listColumnsInput struct {
	listInput
	WidgetCommonID string `json:"widget_common_id" jsonschema:"the Favro widget id (widgetCommonId) whose columns to list; required by Favro on every page (must be passed on follow-up pages too, not just the first)"`
}

// getColumnInput is the input for favro_get_column.
type getColumnInput struct {
	ColumnID string `json:"column_id" jsonschema:"the Favro column id (columnId)"`
}

func registerColumns(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listColumnsToolName,
		Description: "List Favro columns (status lanes) on a single widget. " +
			"`widget_common_id` is REQUIRED — Favro rejects unfiltered listings with 400. " +
			"Items are sorted by `position` ascending (left-to-right board order); Favro " +
			"itself returns them in columnId order, which is meaningless to humans. " +
			"Returns one page; pass `page` (1-indexed) plus the `request_id` from the prior " +
			"response (and `widget_common_id` again) to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro columns"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listColumnsInput) (*mcp.CallToolResult, listOutput[favro.Column], error) {
		env, err := client.ListColumns(ctx, in.Page, in.RequestID, in.WidgetCommonID)
		if err != nil {
			return nil, listOutput[favro.Column]{}, err
		}
		slices.SortStableFunc(env.Entities, func(a, b favro.Column) int {
			return cmp.Compare(a.Position, b.Position)
		})
		return nil, newListOutput(env), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getColumnToolName,
		Description: "Get a single Favro column by its columnId. Read-only.",
		Annotations: readOnly("Get Favro column"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getColumnInput) (*mcp.CallToolResult, favro.Column, error) {
		col, err := client.GetColumn(ctx, in.ColumnID)
		if err != nil {
			return nil, favro.Column{}, err
		}
		return nil, col, nil
	})
}
