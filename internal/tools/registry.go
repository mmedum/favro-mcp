package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/render"
)

// schemaOptions fixes what type inference gets wrong about
// json.RawMessage.
//
// A RawMessage is []byte in Go, so the inferrer describes it as an
// array of integers between 0 and 255 — and the SDK then validates
// every tool result against that schema. Favro puts real JSON in those
// fields: a custom field of type Vote, Members, Tags, Status or
// Multiple select carries an array of ids, so `favro_get_card` and
// `favro_list_cards` failed outright on any card using one, with a
// protocol error rather than a result. It was invisible to every
// fixture, because a fixture that sends what the schema claims is a
// fixture that agrees with the bug; a live read is what found it.
//
// An empty schema accepts anything, which is the honest description:
// the shape of a custom field's value depends on its type, and §7.4 is
// the documentation for that.
var schemaOptions = &jsonschema.ForOptions{
	TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[json.RawMessage](): {},
	},
}

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

	// uploadDir is FAVRO_UPLOAD_DIR, the one directory the upload tools
	// read from. Empty, and they are not registered.
	uploadDir string
}

// destructive reports whether t is annotated destructive.
func destructive(t *mcp.Tool) bool {
	return t.Annotations != nil && t.Annotations.DestructiveHint != nil && *t.Annotations.DestructiveHint
}

// promptMeta is Claude Code's mark for a tool whose permission prompt is
// itself the point: Claude Code prompts on every call of it, whatever the
// operator's rules say, and denies the call where no prompt can be drawn.
const promptMeta = "anthropic/requiresUserInteraction"

// promptEveryCall is the _meta of a write that cannot be undone and
// reaches past the one thing named: a delete that takes something every
// card or share depends on, or a collection opened to the whole
// organization or to anyone with its link (§9.2). Only a destructive
// tool may carry it; addTool refuses it on any other.
func promptEveryCall() mcp.Meta { return mcp.Meta{promptMeta: true} }

// confirmInput is embedded in every destructive tool's input. It is a
// witness that the caller meant this call, never approval: the model
// sets it, so it stops a call made by mistake and nothing more. The
// destructive flag is the control (§9.2).
type confirmInput struct {
	Confirm bool `json:"confirm,omitempty" jsonschema:"must be true for the call to go ahead: pass it only when the person asked for this call. Not needed with dry_run."`
}

func (c confirmInput) confirmed() bool { return c.Confirm }

// confirmer is an input that embeds confirmInput.
type confirmer interface{ confirmed() bool }

// dryRunner is an input that embeds dryRunInput.
type dryRunner interface{ dryRun() bool }

// refuseUnconfirmed refuses a destructive call that is not a dry run and
// does not carry confirm: true, before its handler reads or writes
// anything.
func refuseUnconfirmed(tool string, in any) error {
	if d, ok := in.(dryRunner); ok && d.dryRun() {
		return nil
	}
	if c, ok := in.(confirmer); ok && c.confirmed() {
		return nil
	}
	return render.Errorf(render.ClassBlocked, "%s is refused without confirm: true; nothing was changed. "+
		"Pass confirm: true only when the person asked for this call, or dry_run: true to preview it", tool)
}

// requireRules panics on a tool that breaks a rule of §9.2: a
// destructive tool whose input does not embed confirmInput, or a tool
// that carries the mark and is not destructive.
func requireRules[In any](t *mcp.Tool, gated bool) {
	if _, ok := any(*new(In)).(confirmer); gated && !ok {
		panic(fmt.Sprintf("tool %q is destructive and its input does not embed confirmInput", t.Name))
	}
	if _, marked := t.Meta[promptMeta]; marked && !gated {
		panic(fmt.Sprintf("tool %q carries %s and is not destructive", t.Name, promptMeta))
	}
}

// addTool registers one tool, and is the single place five things
// happen that would otherwise have to happen in every handler.
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
// **confirm: true.** A destructive tool's input embeds confirmInput, or
// registration panics, and a call without it is refused [blocked]
// unless it is a dry run. The rule lives here so no delete can forget
// it.
//
// **The mark.** Only a destructive tool may carry promptMeta, or
// registration panics: the mark goes on the few writes whose prompt is
// the point, never on an ordinary one.
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
	gated := destructive(t)
	if gated && !reg.destructive {
		return
	}
	requireRules[In](t, gated)

	// The output schema is generated here rather than left to the SDK,
	// so schemaOptions applies. Registration panics on a bad schema,
	// which is the right time to find out: it happens on every start,
	// including the smoke gate's.
	if t.OutputSchema == nil {
		schema, err := jsonschema.For[Out](schemaOptions)
		if err != nil {
			panic(fmt.Sprintf("tool %q: output schema: %v", t.Name, err))
		}
		t.OutputSchema = schema
	}

	mcp.AddTool(reg.srv, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		if gated {
			if err := refuseUnconfirmed(t.Name, in); err != nil {
				return nil, zero, render.Error(err)
			}
		}
		res, out, err := h(ctx, req, in)
		if err != nil {
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
