// Package server wires the auth subsystem and the MCP SDK into a
// configured *mcp.Server, ready to Run over any transport.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// serverName is the MCP Implementation name advertised on the protocol
// handshake. Hosts use it for de-duplication and display, so it stays
// stable across versions.
const serverName = "favro-mcp"

// New returns an *mcp.Server with every registered tool. The Favro
// client is plumbed into handlers that need it; source is the
// credential-source name ("env" / "keyring") surfaced by favro_ping;
// version is embedded as MCP Implementation.Version.
func New(client *favro.Client, source, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: version,
	}, nil)

	registerPing(srv, client, source, version)
	registerRateLimitStatus(srv, client)
	registerOrganizations(srv, client)
	registerUsers(srv, client)
	registerCollections(srv, client)

	return srv
}
