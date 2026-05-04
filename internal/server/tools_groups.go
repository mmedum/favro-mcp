package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listGroupsToolName = "favro_list_groups"
	getGroupToolName   = "favro_get_group"
)

// getGroupInput is the input for favro_get_group.
type getGroupInput struct {
	GroupID string `json:"group_id" jsonschema:"the Favro groupId"`
}

func registerGroups(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listGroupsToolName,
		Description: "List Favro groups (named user collections) in the active organization. " +
			"Groups are org-global (no widget or card scope). Each entry includes the " +
			"group's `members` list, where each member is a `{userId, role}` pair. " +
			"Returns one page; pass `page` (1-indexed) plus the `request_id` from the " +
			"prior response to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro groups"),
	}, wrapList(client.ListGroups))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getGroupToolName,
		Description: "Get a single Favro group by its groupId. Read-only.",
		Annotations: readOnly("Get Favro group"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getGroupInput) (*mcp.CallToolResult, favro.Group, error) {
		g, err := client.GetGroup(ctx, in.GroupID)
		if err != nil {
			return nil, favro.Group{}, err
		}
		return nil, g, nil
	})
}
