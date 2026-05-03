// Package server wires the auth subsystem and the MCP SDK into a
// configured *mcp.Server, ready to Run over any transport.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/auth"
)

// serverName is the MCP Implementation name advertised on the protocol
// handshake. Hosts use it for de-duplication and display, so it stays
// stable across versions.
const serverName = "favro-mcp"

// New returns an *mcp.Server with every registered tool. The resolved
// token is plumbed into handlers; version is embedded as the MCP
// Implementation.Version (set at link time via -ldflags).
func New(rt auth.ResolvedToken, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: version,
	}, nil)

	registerPing(srv, rt, version)

	return srv
}
