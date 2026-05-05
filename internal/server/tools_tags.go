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

// listTagsInput extends the standard pagination shape with the
// optional name filter (Favro's exact-match server-side filter).
type listTagsInput struct {
	listInput
	Name string `json:"name,omitempty" jsonschema:"optional exact-match tag name filter applied server-side; useful when you already know the tag name and just need its id"`
}

// getTagInput is the input for favro_get_tag.
type getTagInput struct {
	TagID string `json:"tag_id" jsonschema:"the Favro tagId"`
}

func registerTags(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listTagsToolName,
		Description: "List Favro tags in the active organization. Tags are org-global " +
			"(no widget or card scope). Pass `name` for an exact-match server-side " +
			"filter. Returns one page; pass `page` (1-indexed) plus the `request_id` " +
			"from the prior response to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro tags"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listTagsInput) (*mcp.CallToolResult, listOutput[favro.Tag], error) {
		env, err := client.ListTags(ctx, in.Page, in.RequestID, favro.ListTagsFilter{Name: in.Name})
		if err != nil {
			return nil, listOutput[favro.Tag]{}, err
		}
		return nil, newListOutput(env), nil
	})

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
