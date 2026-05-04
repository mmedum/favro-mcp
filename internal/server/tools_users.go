package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listUsersToolName = "favro_list_users"
	getUserToolName   = "favro_get_user"
)

// getUserInput is the input for favro_get_user.
type getUserInput struct {
	UserID string `json:"user_id" jsonschema:"the Favro user id"`
}

func registerUsers(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listUsersToolName,
		Description: "List members of the Favro organization the API token is scoped to. " +
			"Returns one page; pass `page` (1-indexed) plus the `request_id` from the " +
			"prior response to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro users"),
	}, wrapList(client.ListUsers))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getUserToolName,
		Description: "Get a single Favro user by id. Read-only.",
		Annotations: readOnly("Get Favro user"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getUserInput) (*mcp.CallToolResult, favro.User, error) {
		u, err := client.GetUser(ctx, in.UserID)
		if err != nil {
			return nil, favro.User{}, err
		}
		return nil, u, nil
	})
}
