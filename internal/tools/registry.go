package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/favroapi"
	"github.com/mmedum/favro-mcp/v4/internal/render"
)

// schemaOptions fixes what type inference gets wrong about
// json.RawMessage.
//
// A RawMessage is []byte in Go, so the inferrer describes it as an
// array of integers between 0 and 255 — and the SDK then validates
// every tool result against that schema. Favro puts real JSON in those
// fields: a custom field of type Vote, Members, Tags, Status or
// Multiple select carries an array of ids, so under that schema
// `favro_get_card` and `favro_list_cards` fail on any card using one,
// with a protocol error rather than a result. A fixture that sends what
// the schema claims agrees with the bug, so only a real card shows it,
// and the live reads catch it only when the cards they read carry such
// a field. Checked by the live steps "read a card as markdown" and
// "list cards by widget and collection, page 1 with a request id".
//
// An empty schema accepts anything, which is the honest description:
// the shape of a custom field's value depends on its type.
// https://favro.com/developer/#card-custom-fields
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
	// whose input takes confirm is not registered — not hidden, not
	// guarded, not present. See addTool.
	destructive bool

	// uploadDir is FAVRO_UPLOAD_DIR, the one directory the upload tools
	// read from. Empty, and they are not registered.
	uploadDir string

	// client is the Favro client the tools write through, read here
	// only for --dry-run, which makes every call a dry run.
	client *favroapi.Client
}

// annotatedDestructive reports whether t says destructiveHint: true.
func annotatedDestructive(t *mcp.Tool) bool {
	return t.Annotations != nil && t.Annotations.DestructiveHint != nil && *t.Annotations.DestructiveHint
}

// promptMeta is Claude Code's mark for a tool whose permission prompt is
// itself the point. From v2.1.199 Claude Code prompts on every call of
// it, allow rules and hooks included, with no "don't ask again" from
// v2.1.246, and denies the call where no prompt can be drawn. No live
// step reads _meta.
// https://code.claude.com/docs/en/mcp
// https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md
const promptMeta = "anthropic/requiresUserInteraction"

// promptEveryCall is the _meta of a write that cannot be undone and
// reaches past the one thing named: a delete that takes something every
// card or share depends on, or a collection opened to the whole
// organization or to anyone with its link. Only a destructive tool may
// carry it; addTool refuses it on any other.
func promptEveryCall() mcp.Meta { return mcp.Meta{promptMeta: true} }

// confirmInput is embedded in every destructive tool's input. It is a
// witness that the caller meant this call, never approval: the model
// sets it, so it stops a call made by mistake and nothing more. The
// destructive flag is the control.
type confirmInput struct {
	Confirm bool `json:"confirm,omitempty" jsonschema:"must be true for the call to go ahead: pass it only when the person asked for this call. Not needed with dry_run."`
}

func (c confirmInput) confirmed() bool { return c.Confirm }

// confirmer is an input that embeds confirmInput.
type confirmer interface{ confirmed() bool }

// dryRunner is an input that embeds dryRunInput.
type dryRunner interface{ dryRun() bool }

// refuseUnconfirmed refuses a destructive call that does not carry
// confirm: true, before its handler reads or writes anything. addTool
// asks it only of a call that is not a dry run.
func refuseUnconfirmed(tool string, in any) error {
	if c, ok := in.(confirmer); ok && c.confirmed() {
		return nil
	}
	return render.Errorf(render.ClassBlocked, "%s is refused without confirm: true; nothing was changed. "+
		"Pass confirm: true only when the person asked for this call, or dry_run: true to preview it", tool)
}

// before is what addTool does ahead of every handler. A call that asks
// for a dry run is one for every write its handler makes, whether or not
// the handler puts the dry run on its own context, and a dry run needs
// no confirm on that promise. A destructive call that is not one, by
// its arguments or by --dry-run, needs confirm: true.
func (reg *registry) before(ctx context.Context, tool string, gated bool, in any) (context.Context, error) {
	if d, ok := in.(dryRunner); ok && d.dryRun() {
		ctx = favroapi.WithDryRun(ctx)
	}
	if gated && !reg.client.DryRun(ctx) {
		return ctx, refuseUnconfirmed(tool, in)
	}
	return ctx, nil
}

// requireRules panics on a tool that breaks a registration rule: a
// write whose input takes no dry_run (D-28), a destructive tool not
// annotated destructiveHint: true, or a tool that carries the mark and
// is not destructive (D-34).
func requireRules(t *mcp.Tool, gated, takesDryRun bool) {
	if readOnly := t.Annotations != nil && t.Annotations.ReadOnlyHint; !readOnly && !takesDryRun {
		panic(fmt.Sprintf("tool %q is not marked read-only and takes no dry_run; embed dryRunInput in its input", t.Name))
	}
	if gated && !annotatedDestructive(t) {
		panic(fmt.Sprintf("tool %q takes confirm and is not annotated destructive; use changing(title)", t.Name))
	}
	if _, marked := t.Meta[promptMeta]; marked && !gated {
		panic(fmt.Sprintf("tool %q carries %s and is not destructive", t.Name, promptMeta))
	}
}

// addTool registers one tool, and is the single place the things below
// happen that would otherwise have to happen in every handler.
//
// **The destructive gate.** A destructive tool — a delete, a whole-list
// replace or a collection opened wide — is one whose input embeds
// confirmInput, and it is registered only when the flag is set (D-29).
// The check reads the input type rather than a list of names, so a
// destructive tool added later is gated by taking confirm, which it
// needs anyway to be refused without one, instead of by somebody
// remembering to add it somewhere else. It does not read
// destructiveHint, which is true on every write that changes something
// (changing) and false only on one that adds (adding). This is
// registration and not a runtime guard because a host in an
// auto-approve permission mode runs an annotated tool without
// prompting, and the MCP schema calls annotations hints that a client
// should never base a decision on when the server is untrusted. Assume
// every registered tool can run unattended.
// https://modelcontextprotocol.io/specification/2025-11-25/schema#toolannotations
//
// **confirm: true.** A call of a destructive tool without it is
// refused [blocked] unless it is a dry run, and registration panics on
// a destructive tool not annotated destructiveHint: true. The rule
// lives here so no delete can forget it.
//
// **dry_run.** Registration panics on a tool not marked read-only whose
// input does not embed dryRunInput, so no write can leave it out (D-28).
//
// **The mark.** Only a destructive tool may carry promptMeta, or
// registration panics: the mark goes on the few writes whose prompt is
// the point, never on an ordinary one.
//
// **Both halves of the result.** The SDK marshals the typed output into
// StructuredContent and, finding Content unset, puts the identical
// bytes in a TextContent block. Setting Content here to a rendered
// summary is what makes the two halves differ (D-61); the SDK leaves a
// Content that is already set alone when the output is a JSON object,
// as every output here is.
// https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/mcp/server.go#L468-L485
//
// **The error class.** Errors leave as "[class] actionable message",
// classified from the error's own type rather than from its text.
func addTool[In, Out any](reg *registry, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	_, gated := any(*new(In)).(confirmer)
	if gated && !reg.destructive {
		return
	}
	_, takesDryRun := any(*new(In)).(dryRunner)
	requireRules(t, gated, takesDryRun)

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
		ctx, err := reg.before(ctx, t.Name, gated, in)
		if err != nil {
			return nil, zero, render.Error(err)
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
