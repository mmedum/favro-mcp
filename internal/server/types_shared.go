package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/render"
)

// listInput is the shared input shape for every list-tool that maps
// to a Favro paginated endpoint. The `request_id` round-trip is
// required because Favro's pagination is stateful: page N+1 is only
// reachable from the backend that produced page N's cursor.
type listInput struct {
	// Page is the 1-indexed page number. Omit (or 0) for the first
	// page. Pass values from a prior call's `next_page`.
	//
	// 1-indexed here and 0-indexed on the wire: see favroPage.
	Page int `json:"page,omitempty" jsonschema:"1-indexed page; omit for the first page. Pass the next_page value from a prior response rather than counting."`
	// RequestID is the prior call's `request_id`. Required when Page
	// > 0; ignored otherwise.
	RequestID string `json:"request_id,omitempty" jsonschema:"request_id from a prior page; required when page > 0 (Favro routes via X-Favro-Backend-Identifier)"`
}

// favroPage converts this tool's 1-indexed page number into the
// 0-indexed one Favro's `page` parameter takes.
//
// The two numberings used to be the same number. The schema has said
// "1-indexed" since these tools shipped, and the value went to Favro
// untouched — so a caller that believed the schema and asked for page 1
// was served Favro's page 1, the *second* page, and never saw the
// first. Nothing failed: it returned a valid page of real results, and
// the missing rows were simply absent.
//
// The conversion lives here rather than in internal/favro because that
// package is a faithful client of Favro's REST API, and Favro's page is
// 0-indexed. This is the MCP layer keeping the promise its own schema
// makes.
func (in listInput) favroPage() int {
	if in.Page <= 0 {
		return 0 // omitted, or an explicit first page
	}
	return in.Page - 1
}

// listOutput is the shared output shape for every list-tool. Items
// is the page's resources; the rest is pagination metadata so the
// caller can decide whether to fetch more.
type listOutput[T any] struct {
	Items      []T    `json:"items" jsonschema:"the resources on this page"`
	Page       int    `json:"page" jsonschema:"the page number this response represents"`
	TotalPages int    `json:"total_pages" jsonschema:"total page count Favro reported"`
	NextPage   *int   `json:"next_page,omitempty" jsonschema:"next page number, or null if this is the last page"`
	RequestID  string `json:"request_id,omitempty" jsonschema:"forward as request_id on the next page"`
}

// newListOutput projects a Favro PageEnvelope into the MCP-shaped
// listOutput.
func newListOutput[T any](env favro.PageEnvelope[T]) listOutput[T] {
	// Favro counts pages from zero; this surface counts from one, so
	// what comes back reads the way the input schema promises and
	// `next_page` can be passed straight back in.
	out := listOutput[T]{
		Items:      env.Entities,
		Page:       env.Page + 1,
		TotalPages: env.Pages,
		RequestID:  env.RequestID,
	}
	if env.HasNextPage() {
		next := env.Page + 2
		out.NextPage = &next
	}
	return out
}

// resolveOutput is the shared output shape for every favro_resolve_*
// tool. T is the per-resource Resolved<X> projection. Cached signals
// whether the underlying resource list came from the in-memory
// cache so callers know whether a `force_refresh` follow-up might
// surface newly-created items.
type resolveOutput[T any] struct {
	Candidates []T  `json:"candidates" jsonschema:"ranked candidates; empty when nothing matches"`
	Cached     bool `json:"cached" jsonschema:"true when results came from the in-memory cache rather than a fresh Favro fetch"`
}

// resolveScoreScaleDoc is the canonical score-scale sentence
// embedded in every favro_resolve_* tool description. Centralized
// so the four magic numbers (1.0 / 0.7 / 0.4 / 0) live in exactly
// one place that's reachable from every tool description and from
// the Resolver layer's scoreLowered helper docstring. Update both
// together if the score scale ever changes.
const resolveScoreScaleDoc = "Score scale: exact match 1.0, prefix 0.7, substring 0.4. "

// readOnly returns the canonical ToolAnnotations for a read-only tool
// with the given title. Centralizing the ReadOnlyHint policy here
// lets a future audit confirm "every read tool actually sets this".
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint: true,
		Title:        title,
	}
}

// mutating returns the canonical ToolAnnotations for a non-read-only
// (mutating) tool. destructive controls Favro's destructiveHint —
// true for delete-style tools so MCP hosts can warn users before
// auto-confirming the call.
//
// The MCP SDK's `DestructiveHint` defaults to true when nil, so we
// always pass an explicit pointer (even for non-destructive tools)
// — letting a future refactor drop the pointer would silently flip
// every `mutating(..., false)` tool into "destructive" semantics.
func mutating(title string, destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: &destructive,
		Title:           title,
	}
}

// dryRunInput is embedded in every mutating tool's input so the
// caller can opt into dry-run on a per-call basis. The binary's
// --dry-run flag forces dry-run process-wide (independent of this
// field).
type dryRunInput struct {
	DryRun bool `json:"dry_run,omitempty" jsonschema:"if true, return a description of the request that would be sent (method + URL + body + predicted state change) without actually contacting Favro. Useful for previewing destructive operations before committing."`
}

// writeOutput is the standard output shape for every mutating tool.
// Either Result (live mode) OR WouldCall+RequestBody+StateDiff
// (dry-run) is populated; DryRun is true exactly when the latter
// branch fires.
//
// T is the resource type returned by the underlying favro write
// (favro.Tag for create_tag, favro.Card for create/update/delete,
// etc.). For deletes that return no payload, callers can use
// writeOutput[struct{}].
type writeOutput[T any] struct {
	DryRun bool `json:"dry_run" jsonschema:"true when dry_run was requested (or forced process-wide). When true, Result is omitted and WouldCall + RequestBody + PredictedStateDiff describe the would-be request."`

	Result *T `json:"result,omitempty" jsonschema:"the resource returned by Favro (populated when dry_run is false)"`

	WouldCall          *DryRunCall `json:"would_call,omitempty" jsonschema:"the HTTP request that would have been sent (populated when dry_run is true)"`
	RequestBody        any         `json:"request_body,omitempty" jsonschema:"the JSON body that would have been sent, decoded into a structured object (populated when dry_run is true)"`
	PredictedStateDiff string      `json:"predicted_state_diff,omitempty" jsonschema:"a human-readable description of the change that would happen (populated when dry_run is true)"`
}

// DryRunCall describes the request a mutating tool would have sent.
// The Authorization header is redacted upstream in
// favro.DryRunRecord; this projection only carries the method and
// URL the LLM needs to reason about scope.
type DryRunCall struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

// runWrite executes a favro write `run` and projects the result
// into the unified writeOutput shape:
//   - On success → {DryRun:false, Result: &result}.
//   - On *favro.DryRunRecord (the dry-run gate fired) →
//     {DryRun:true, WouldCall, RequestBody, PredictedStateDiff}.
//   - On any other error → propagate.
//
// stateDiff is provided by the caller because the natural-language
// "what would happen" phrasing is per-tool ("would create tag X",
// "would archive card Y", etc).
func runWrite[T any](
	run func() (T, error),
	stateDiff func() string,
) (writeOutput[T], error) {
	result, err := run()
	if err == nil {
		return writeOutput[T]{Result: &result}, nil
	}
	var rec *favro.DryRunRecord
	if errors.As(err, &rec) {
		// Decode the captured body into a structured Go value so the
		// MCP output carries a typed JSON object, not opaque bytes.
		// Empty body → nil (DELETEs and similar carry no payload).
		var body any
		if len(rec.Body) > 0 {
			if jerr := json.Unmarshal(rec.Body, &body); jerr != nil {
				// Fall back to the raw string — Favro requests are
				// always JSON in this codebase, but propagating an
				// unmarshal error here would mask the dry-run signal.
				body = string(rec.Body)
			}
		}
		return writeOutput[T]{
			DryRun:             true,
			WouldCall:          &DryRunCall{Method: rec.Method, URL: rec.URL},
			RequestBody:        body,
			PredictedStateDiff: stateDiff(),
		}, nil
	}
	return writeOutput[T]{}, err
}

// listFn is the shape every Favro list method exposes:
// (ctx, page, requestID) → typed PageEnvelope.
type listFn[T any] func(ctx context.Context, page int, requestID string) (favro.PageEnvelope[T], error)

// wrapList adapts a Favro list method into the MCP tool handler the
// SDK expects. Centralizes the err-bridge + envelope projection so
// every favro_list_<resource> tool reduces to one line.
func wrapList[T any](fn listFn[T]) func(context.Context, *mcp.CallToolRequest, listInput) (*mcp.CallToolResult, listOutput[T], error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput[T], error) {
		env, err := fn(ctx, in.favroPage(), in.RequestID)
		if err != nil {
			return nil, listOutput[T]{}, err
		}
		return nil, newListOutput(env), nil
	}
}

// Summary renders a page for the readable half: what came back, where
// in the sequence it sits, and whether there is more. The pagination
// line comes first because it is the one thing a caller cannot get by
// reading the items — and because this server never aggregates pages,
// so a caller that misses `next_page` silently sees a prefix of the
// answer and believes it is the whole one.
func (o listOutput[T]) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s", len(o.Items), render.Plural(len(o.Items), "item", "items"))
	if o.TotalPages > 0 {
		fmt.Fprintf(&b, " · page %d of %d", o.Page, o.TotalPages)
	}
	if o.NextPage != nil {
		fmt.Fprintf(&b, " · next_page %d (not fetched; call again to continue)", *o.NextPage)
	} else {
		b.WriteString(" · last page")
	}
	b.WriteString(render.InlineList(o.Items))
	return b.String()
}

// Summary renders a write for the readable half. The dry-run branch
// leads with the verdict and the method and URL, because "did this
// actually happen" is the only question a caller has about a mutating
// call, and a result that looks like a success is how §2.1's problem
// starts.
func (o writeOutput[T]) Summary() string {
	if o.DryRun {
		var b strings.Builder
		b.WriteString("DRY RUN — nothing was sent to Favro.")
		if o.WouldCall != nil {
			fmt.Fprintf(&b, "\n  would call: %s %s", o.WouldCall.Method, o.WouldCall.URL)
		}
		if o.PredictedStateDiff != "" {
			fmt.Fprintf(&b, "\n  would change: %s", o.PredictedStateDiff)
		}
		return b.String()
	}
	if o.Result == nil {
		return "done; Favro returned no resource."
	}
	return "done:\n" + render.Summary(*o.Result)
}

// Summary renders a name lookup for the readable half: the count
// first, because zero and several are the two answers that need a
// different next call, then the candidates one line each so the
// caller can pick without a second round-trip.
func (o resolveOutput[T]) Summary() string {
	source := "fetched from Favro"
	if o.Cached {
		source = "from cache; pass force_refresh to re-fetch"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s (%s)",
		len(o.Candidates), render.Plural(len(o.Candidates), "candidate", "candidates"), source)
	b.WriteString(render.InlineList(o.Candidates))
	return b.String()
}
