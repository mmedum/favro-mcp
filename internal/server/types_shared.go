package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// listInput is the shared input shape for every list-tool that maps
// to a Favro paginated endpoint. The `request_id` round-trip is
// required because Favro's pagination is stateful: page N+1 is only
// reachable from the backend that produced page N's cursor.
type listInput struct {
	// Page is the 1-indexed page number. Omit (or 0) for the first
	// page. Pass values from a prior call's `next_page`.
	Page int `json:"page,omitempty" jsonschema:"1-indexed page; omit for the first page"`
	// RequestID is the prior call's `request_id`. Required when Page
	// > 0; ignored otherwise.
	RequestID string `json:"request_id,omitempty" jsonschema:"request_id from a prior page; required when page > 0 (Favro routes via X-Favro-Backend-Identifier)"`
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
	out := listOutput[T]{
		Items:      env.Entities,
		Page:       env.Page,
		TotalPages: env.Pages,
		RequestID:  env.RequestID,
	}
	if env.HasNextPage() {
		next := env.Page + 1
		out.NextPage = &next
	}
	return out
}

// readOnly returns the canonical ToolAnnotations for a read-only tool
// with the given title. Centralizing the ReadOnlyHint policy here
// lets a future audit confirm "every read tool actually sets this".
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint: true,
		Title:        title,
	}
}

// listFn is the shape every Favro list method exposes:
// (ctx, page, requestID) → typed PageEnvelope.
type listFn[T any] func(ctx context.Context, page int, requestID string) (favro.PageEnvelope[T], error)

// wrapList adapts a Favro list method into the MCP tool handler the
// SDK expects. Centralizes the err-bridge + envelope projection so
// every favro_list_<resource> tool reduces to one line.
func wrapList[T any](fn listFn[T]) func(context.Context, *mcp.CallToolRequest, listInput) (*mcp.CallToolResult, listOutput[T], error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput[T], error) {
		env, err := fn(ctx, in.Page, in.RequestID)
		if err != nil {
			return nil, listOutput[T]{}, err
		}
		return nil, newListOutput(env), nil
	}
}
