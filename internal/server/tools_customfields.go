package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listCustomFieldsToolName = "favro_list_custom_fields"
	getCustomFieldToolName   = "favro_get_custom_field"
)

// getCustomFieldInput is the input for favro_get_custom_field.
type getCustomFieldInput struct {
	CustomFieldID string `json:"custom_field_id" jsonschema:"the Favro customFieldId"`
}

func registerCustomFields(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listCustomFieldsToolName,
		Description: "List Favro custom fields in the active organization. Custom fields " +
			"are org-global (no widget or card scope). Each entry includes the field's " +
			"`type` (Favro's display label — e.g. \"Text\", \"Number\", \"Date\", " +
			"\"Checkbox\", \"Single select\", \"Multiple select\", \"Members\", \"Tags\", " +
			"\"Rating\", \"Link\", \"Progress\", \"Voting\", \"Relations\", \"Sequential " +
			"ID\", \"Timeline\"; Favro extends this set without notice) and, for " +
			"select-flavored types, a `customFieldItems` list of legal options. Returns " +
			"one page; pass `page` (1-indexed) plus the `request_id` from the prior " +
			"response to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro custom fields"),
	}, wrapList(client.ListCustomFields))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getCustomFieldToolName,
		Description: "Get a single Favro custom field by its customFieldId. Read-only.",
		Annotations: readOnly("Get Favro custom field"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getCustomFieldInput) (*mcp.CallToolResult, favro.CustomField, error) {
		cf, err := client.GetCustomField(ctx, in.CustomFieldID)
		if err != nil {
			return nil, favro.CustomField{}, err
		}
		return nil, cf, nil
	})
}
