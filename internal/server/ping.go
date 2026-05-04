package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// pingToolName is the tool name advertised on the MCP protocol and
// asserted by tests; one source of truth so a rename can't drift.
const pingToolName = "favro_ping"

// pingInput is intentionally empty — favro_ping takes no parameters.
// A named type (rather than struct{}) keeps the JSON Schema generation
// in the SDK predictable and leaves a place to extend later if a scope
// hint becomes useful.
type pingInput struct{}

// PingOutput is the structured response from favro_ping.
//
// IMPORTANT: this struct must NEVER carry the user's email, the API
// token, the Authorization header, or any URL containing them. Tests
// pin this down (TestMCP_FavroPing_OutputContainsNoSecrets).
type PingOutput struct {
	Server           string `json:"server" jsonschema:"the server identifier"`
	Version          string `json:"version" jsonschema:"the build version of the server"`
	OrganizationID   string `json:"organization_id" jsonschema:"the Favro organization id the server is bound to"`
	CredentialSource string `json:"credential_source" jsonschema:"where the active credentials came from: env or keyring"`
}

// registerPing wires the favro_ping tool into srv. The output is
// captured at registration time because nothing in it changes during
// the server's lifetime — credentials are resolved once at startup.
func registerPing(srv *mcp.Server, client *favro.Client, source, version string) {
	out := PingOutput{
		Server:           serverName,
		Version:          version,
		OrganizationID:   client.Token.OrganizationID,
		CredentialSource: source,
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name: pingToolName,
		Description: "Liveness check. Returns the server version, the bound Favro " +
			"organization id, and which credential source is active. Does NOT " +
			"contact Favro — this is a local diagnostic, not a connectivity test.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint: true,
			Title:        "Server liveness check",
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ pingInput) (*mcp.CallToolResult, PingOutput, error) {
		return nil, out, nil
	})
}
