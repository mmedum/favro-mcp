package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/render"
)

// registry is what every register* function adds to. It carries the
// *mcp.Server plus the one policy decision that has to be visible at
// the moment a tool is registered rather than at the moment it is
// called: whether destructive tools exist at all.
//
// It is a struct rather than two arguments because Go does not allow
// type parameters on methods, so the destructive flag cannot ride on a
// method of *mcp.Server and addTool has to be a function that receives
// it.
type registry struct {
	srv *mcp.Server
	// destructive is FAVRO_ENABLE_DESTRUCTIVE. When false, a tool
	// whose annotations set DestructiveHint is not registered — not
	// hidden, not guarded, not present. See addTool.
	destructive bool
}

// addTool registers one tool, and is the single place three things
// happen that would otherwise have to happen in all 83 handlers.
//
// **The destructive gate.** A tool annotated destructive is registered
// only when the flag is set. The check reads the annotation rather than
// a list of names, so a destructive tool added later is gated by having
// been annotated — which the author has to do anyway — instead of by
// somebody remembering to add it somewhere else. Standard §3 is blunt
// about why this is registration and not a runtime guard: a host in an
// auto-approve permission mode runs an annotated tool without
// prompting, and the spec says clients treat tool annotations as
// untrusted. Assume every registered tool can run unattended.
//
// **Both halves of the result.** The SDK marshals the typed output into
// StructuredContent and, finding Content unset, puts the identical
// bytes in a TextContent block. Setting Content here to a rendered
// summary is what makes the readable half readable; the SDK leaves a
// Content that is already set alone.
//
// **The error class.** Errors leave as "[class] actionable message",
// classified from the error's own type rather than from its text.
func addTool[In, Out any](reg *registry, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	if t.Annotations != nil && t.Annotations.DestructiveHint != nil &&
		*t.Annotations.DestructiveHint && !reg.destructive {
		return
	}

	mcp.AddTool(reg.srv, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if err != nil {
			var zero Out
			return nil, zero, render.Error(err)
		}
		if res == nil {
			res = &mcp.CallToolResult{}
		}
		if res.Content == nil {
			res.Content = []mcp.Content{&mcp.TextContent{Text: render.Summary(out)}}
		}
		return res, out, nil
	})
}
