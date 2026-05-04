package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listOrgsToolName = "favro_list_organizations"
	getOrgToolName   = "favro_get_organization"
)

// getOrgInput is the input for favro_get_organization.
type getOrgInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"the Favro organization id (24-char hex)"`
}

func registerOrganizations(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listOrgsToolName,
		Description: "List Favro organizations the API token can see. Returns one " +
			"page; pass `page` (1-indexed) plus the `request_id` from the prior " +
			"response to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro organizations"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput[favro.Organization], error) {
		env, err := client.ListOrganizations(ctx, in.Page, in.RequestID)
		if err != nil {
			return nil, listOutput[favro.Organization]{}, err
		}
		return nil, newListOutput(env), nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        getOrgToolName,
		Description: "Get a single Favro organization by id. Read-only.",
		Annotations: readOnly("Get Favro organization"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getOrgInput) (*mcp.CallToolResult, favro.Organization, error) {
		org, err := client.GetOrganization(ctx, in.OrganizationID)
		if err != nil {
			return nil, favro.Organization{}, err
		}
		return nil, org, nil
	})
}
