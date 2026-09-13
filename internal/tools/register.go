package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/service"
)

// ServerName is the MCP Implementation name advertised on the protocol
// handshake. Hosts use it for de-duplication and display, so it stays
// stable across versions; favro_ping returns it too, which is why it
// lives here rather than beside the handshake that sends it.
const ServerName = "favro-mcp"

// Options is what the process knows and the tool surface depends on.
type Options struct {
	// Destructive registers the delete-style tools. It comes from
	// FAVRO_ENABLE_DESTRUCTIVE, and the default — off — means those
	// tools are not in tools/list at all.
	//
	// This is registration rather than a runtime guard on purpose.
	// Standard §3: a host in an auto-approve permission mode runs an
	// annotated tool without prompting, and the spec says clients
	// treat tool annotations as untrusted, so `destructiveHint` is a
	// signal a client may act on and not a control. The only tool that
	// cannot be run unattended is one that is not there.
	Destructive bool

	// CredentialSource is "env" or "keyring", surfaced by favro_ping so
	// a caller can tell which credentials are live without being shown
	// any of them.
	CredentialSource string

	// Version is the build stamp, surfaced by favro_ping and sent on
	// the MCP handshake.
	Version string
}

// Register adds every tool opts admits to srv.
//
// This is the whole MCP surface in one list, and it is deliberately a
// list: the alternative — each file registering itself from an init()
// — hides the surface from the one place somebody looks for it, and
// makes the order the linker's business rather than a decision.
//
// Every call below is unconditional. Whether a destructive tool is
// admitted is decided in addTool, from the tool's own annotation;
// gating here instead would mean a second list of which tools are
// destructive, and a new one would be missed by exactly the person who
// forgot the first list existed.
//
// A single Resolver is constructed here and shared across every
// resolver tool so the cache state is process-wide; ad-hoc resolvers
// per-tool would each maintain their own cache and burn the rate-limit
// budget on parallel cold-start fetches.
func Register(srv *mcp.Server, client *favroapi.Client, opts Options) {
	reg := &registry{srv: srv, destructive: opts.Destructive}
	resolver := service.NewResolver(client)

	registerPing(reg, client, opts.CredentialSource, opts.Version)
	registerRateLimitStatus(reg, client)
	registerOrganizations(reg, client)
	registerUsers(reg, client)
	registerCollections(reg, client)
	registerWidgets(reg, client)
	registerColumns(reg, client)
	registerCards(reg, client)
	registerComments(reg, client)
	registerTags(reg, client)
	registerCustomFields(reg, client)
	registerGroups(reg, client)
	registerResolveTag(reg, resolver)
	registerResolveUser(reg, resolver)
	registerResolveCollection(reg, resolver)
	registerResolveWidget(reg, resolver)
	registerResolveColumn(reg, resolver)
	registerResolveCustomField(reg, resolver)
	registerResolveGroup(reg, resolver)
	registerSearchCards(reg, resolver)
	registerGetCardFull(reg, resolver)
	registerCreateTag(reg, resolver)
	registerDeleteTag(reg, resolver)
	registerUpdateTag(reg, resolver)
	registerUpdateTags(reg, resolver)
	registerCreateComment(reg, resolver)
	registerUpdateComment(reg, resolver)
	registerDeleteComment(reg, resolver)
	registerCreateCard(reg, resolver)
	registerUpdateCard(reg, resolver)
	registerArchiveCard(reg, resolver)
	registerUnarchiveCard(reg, resolver)
	registerMoveCard(reg, resolver)
	registerDeleteCard(reg, resolver)
	registerCreateCollection(reg, resolver)
	registerUpdateCollection(reg, resolver)
	registerDeleteCollection(reg, resolver)
	registerCreateWidget(reg, resolver)
	registerUpdateWidget(reg, resolver)
	registerDeleteWidget(reg, resolver)
	registerCreateColumn(reg, resolver)
	registerUpdateColumn(reg, resolver)
	registerDeleteColumn(reg, resolver)
	registerCreateGroup(reg, resolver)
	registerUpdateGroup(reg, resolver)
	registerDeleteGroup(reg, resolver)
	registerSetCardCustomField(reg, resolver)
	registerAppendCardDescription(reg, resolver)
	registerPrependCardDescription(reg, resolver)
	registerReplaceInCardDescription(reg, resolver)
	registerAddCommentToCard(reg, resolver)
	registerAddTagToCard(reg, resolver)
	registerRemoveTagFromCard(reg, resolver)
	registerUploadAttachment(reg, resolver)
	registerUploadCommentAttachment(reg, resolver)
	registerRemoveAttachment(reg, resolver)
	registerTasks(reg, client)
	registerTasklists(reg, client)
	registerDependencies(reg, client)
	registerActivities(reg, client)
}
