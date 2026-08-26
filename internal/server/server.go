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
//
// A single Resolver is constructed here and shared across every
// resolver tool so the cache state is process-wide; ad-hoc
// resolvers per-tool would each maintain their own cache and burn
// the rate-limit budget on parallel cold-start fetches.
func New(client *favro.Client, source, version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: version,
	}, nil)

	resolver := NewResolver(client)

	registerPing(srv, client, source, version)
	registerRateLimitStatus(srv, client)
	registerOrganizations(srv, client)
	registerUsers(srv, client)
	registerCollections(srv, client)
	registerWidgets(srv, client)
	registerColumns(srv, client)
	registerCards(srv, client)
	registerComments(srv, client)
	registerTags(srv, client)
	registerCustomFields(srv, client)
	registerGroups(srv, client)
	registerResolveTag(srv, resolver)
	registerResolveUser(srv, resolver)
	registerResolveCollection(srv, resolver)
	registerResolveWidget(srv, resolver)
	registerResolveColumn(srv, resolver)
	registerResolveCustomField(srv, resolver)
	registerResolveGroup(srv, resolver)
	registerSearchCards(srv, resolver)
	registerGetCardFull(srv, resolver)
	registerCreateTag(srv, resolver)
	registerDeleteTag(srv, resolver)
	registerUpdateTag(srv, resolver)
	registerUpdateTags(srv, resolver)
	registerCreateComment(srv, resolver)
	registerUpdateComment(srv, resolver)
	registerDeleteComment(srv, resolver)
	registerCreateCard(srv, resolver)
	registerUpdateCard(srv, resolver)
	registerArchiveCard(srv, resolver)
	registerUnarchiveCard(srv, resolver)
	registerMoveCard(srv, resolver)
	registerDeleteCard(srv, resolver)
	registerCreateCollection(srv, resolver)
	registerUpdateCollection(srv, resolver)
	registerDeleteCollection(srv, resolver)
	registerCreateWidget(srv, resolver)
	registerUpdateWidget(srv, resolver)
	registerDeleteWidget(srv, resolver)
	registerCreateColumn(srv, resolver)
	registerUpdateColumn(srv, resolver)
	registerDeleteColumn(srv, resolver)
	registerCreateGroup(srv, resolver)
	registerUpdateGroup(srv, resolver)
	registerDeleteGroup(srv, resolver)
	registerSetCardCustomField(srv, resolver)
	registerAppendCardDescription(srv, resolver)
	registerPrependCardDescription(srv, resolver)
	registerReplaceInCardDescription(srv, resolver)
	registerAddCommentToCard(srv, resolver)
	registerAddTagToCard(srv, resolver)
	registerRemoveTagFromCard(srv, resolver)
	registerUploadAttachment(srv, resolver)
	registerUploadCommentAttachment(srv, resolver)
	registerRemoveAttachment(srv, resolver)
	registerTasks(srv, client)
	registerTasklists(srv, client)
	registerDependencies(srv, client)
	registerActivities(srv, client)

	return srv
}
