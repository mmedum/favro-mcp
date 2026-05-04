package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resolveCustomFieldToolName = "favro_resolve_custom_field"

type resolveCustomFieldInput struct {
	Name         string `json:"name" jsonschema:"the custom field name (or part of it) to resolve; matched case-insensitively"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max candidates to return (default 10, max 50)"`
	ForceRefresh bool   `json:"force_refresh,omitempty" jsonschema:"bypass the 5-minute custom-field cache and re-fetch from Favro before matching"`
}

func registerResolveCustomField(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: resolveCustomFieldToolName,
		Description: "Resolve a custom field name (or part of it) to one or more customFieldId " +
			"candidates. Each candidate includes the field's `type` (\"Single select\", \"Date\", " +
			"\"Members\", etc.) — useful for disambiguation when an org has multiple " +
			"\"Priority\" fields of different kinds. " + resolveScoreScaleDoc +
			"Custom-field list caches for 5 minutes; pass `force_refresh: true` to bypass. Read-only.",
		Annotations: readOnly("Resolve Favro custom field name"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveCustomFieldInput) (*mcp.CallToolResult, resolveOutput[ResolvedCustomField], error) {
		matches, cached, err := r.ResolveCustomField(ctx, in.Name, in.Limit, in.ForceRefresh)
		if err != nil {
			return nil, resolveOutput[ResolvedCustomField]{}, err
		}
		return nil, resolveOutput[ResolvedCustomField]{Candidates: matches, Cached: cached}, nil
	})
}
