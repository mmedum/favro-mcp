package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/favroapi"
)

const (
	listOrgsToolName = "favro_list_organizations"
	getOrgToolName   = "favro_get_organization"
)

// getOrgInput is the input for favro_get_organization: nothing.
//
// It used to take an `organization_id`, which Favro documents as "the id
// of the organization to be retrieved. Required." The live API does not
// honour it — it routes by the `organizationId` HEADER, which Favro
// documents separately as required "to ensure the request is routed to
// the correct server" — so any value, a malformed one included, returned
// the organization the header names. §2.1 again, and this server makes
// it structural rather than merely surprising: it binds one organization
// at startup and sets that header on every request, so the path segment
// could never select anything else here.
//
// A required input that cannot affect the result is worse than no input.
// It reads as a choice, and a model asking for one organization was
// silently handed another.
//
// A named type rather than struct{} keeps the JSON Schema generation
// consistent with every other tool.
type getOrgInput struct{}

func registerOrganizations(reg *registry, client *favroapi.Client) {
	addTool(reg, &mcp.Tool{
		Name: listOrgsToolName,
		Description: "List Favro organizations the API token can see. Returns one " +
			"page; pass `page` (1-indexed) plus the `request_id` from the prior " +
			"response to retrieve subsequent pages. Read-only.",
		Annotations: readOnly("List Favro organizations"),
	}, wrapList(client.ListOrganizations))

	addTool(reg, &mcp.Tool{
		Name: getOrgToolName,
		Description: "Get the Favro organization this server is bound to — its name, id " +
			"and membership. Takes no input: the server is single-org, binds one " +
			"organization at startup, and Favro routes this call by a header rather than " +
			"by the id in the path. Use `favro_list_organizations` to see every " +
			"organization the token can reach. Read-only.",
		Annotations: readOnly("Get Favro organization"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ getOrgInput) (*mcp.CallToolResult, favro.Organization, error) {
		org, err := client.GetOrganization(ctx, client.Token.OrganizationID)
		if err != nil {
			return nil, favro.Organization{}, err
		}
		return nil, org, nil
	})
}
