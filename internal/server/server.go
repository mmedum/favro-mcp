// Package server is the schema dump, plus the four-line constructor
// that builds an *mcp.Server ready to Run over any transport.
//
// That is an honest description of its size: New does almost nothing,
// because what the server exposes is internal/tools, what those tools
// orchestrate is internal/service, and what talks to Favro is
// internal/favroapi. What keeps the package worth having is
// DumpSchemas, which drives an in-memory client session against the
// surface — a client belongs here rather than inside the package whose
// job is to describe a server.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/tools"
)

// Options is what the process knows and the tool surface depends on.
// An alias rather than a copy, so cmd/favro-mcp keeps one name for it
// and there is no second struct to keep in step.
type Options = tools.Options

// New returns an *mcp.Server with every tool opts admits. The Favro
// client is plumbed into the handlers that need it; everything else the
// surface depends on travels in opts, which is what keeps the
// credential-source name and the version from being two adjacent
// strings nothing would notice being swapped.
func New(client *favroapi.Client, opts Options) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    tools.ServerName,
		Version: opts.Version,
	}, nil)
	tools.Register(srv, client, opts)
	return srv
}
