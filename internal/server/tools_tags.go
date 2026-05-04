package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listTagsToolName = "favro_list_tags"
	getTagToolName   = "favro_get_tag"
)

// getTagInput is the input for favro_get_tag.
type getTagInput struct {
	TagID string `json:"tag_id" jsonschema:"the Favro tagId"`
}

func registerTags(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listTagsToolName,
		Description: "List Favro tags in the active organization. Tags are org-global " +
			"(no widget or card scope). Returns one page; pass `page` (1-indexed) plus " +
			"the `request_id` from the prior response to retrieve subsequent pages. " +
			"Read-only.",
		Annotations: readOnly("List Favro tags"),
	}, wrapList(client.ListTags))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getTagToolName,
		Description: "Get a single Favro tag by its tagId. Read-only.",
		Annotations: readOnly("Get Favro tag"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getTagInput) (*mcp.CallToolResult, favro.Tag, error) {
		tag, err := client.GetTag(ctx, in.TagID)
		if err != nil {
			return nil, favro.Tag{}, err
		}
		return nil, tag, nil
	})
}
