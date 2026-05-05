package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listCollectionsToolName = "favro_list_collections"
	getCollectionToolName   = "favro_get_collection"
)

// listCollectionsInput extends the standard pagination shape with
// the optional archived flag.
type listCollectionsInput struct {
	listInput
	Archived bool `json:"archived,omitempty" jsonschema:"if true, include archived collections in the result; default (false) hides them server-side"`
}

// getCollectionInput is the input for favro_get_collection.
type getCollectionInput struct {
	CollectionID string `json:"collection_id" jsonschema:"the Favro collection id"`
}

func registerCollections(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listCollectionsToolName,
		Description: "List Favro collections in the organization the API token is scoped to. " +
			"Pass `archived: true` to include archived collections. Returns one page; " +
			"pass `page` (1-indexed) plus the `request_id` from the prior response to " +
			"retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro collections"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listCollectionsInput) (*mcp.CallToolResult, listOutput[favro.Collection], error) {
		env, err := client.ListCollections(ctx, in.Page, in.RequestID, favro.ListCollectionsFilter{
			Archived: in.Archived,
		})
		if err != nil {
			return nil, listOutput[favro.Collection]{}, err
		}
		return nil, newListOutput(env), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getCollectionToolName,
		Description: "Get a single Favro collection by id. Read-only.",
		Annotations: readOnly("Get Favro collection"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getCollectionInput) (*mcp.CallToolResult, favro.Collection, error) {
		col, err := client.GetCollection(ctx, in.CollectionID)
		if err != nil {
			return nil, favro.Collection{}, err
		}
		return nil, col, nil
	})
}
