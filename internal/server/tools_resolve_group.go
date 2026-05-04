package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resolveGroupToolName = "favro_resolve_group"

type resolveGroupInput struct {
	Name         string `json:"name" jsonschema:"the group name (or part of it) to resolve; matched case-insensitively"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max candidates to return (default 10, max 50)"`
	ForceRefresh bool   `json:"force_refresh,omitempty" jsonschema:"bypass the 5-minute group cache and re-fetch from Favro before matching"`
}

func registerResolveGroup(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: resolveGroupToolName,
		Description: "Resolve a group name (or part of it) to one or more groupId candidates. " +
			resolveScoreScaleDoc +
			"Group list caches for 5 minutes; pass `force_refresh: true` to bypass. Read-only.",
		Annotations: readOnly("Resolve Favro group name"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveGroupInput) (*mcp.CallToolResult, resolveOutput[ResolvedGroup], error) {
		matches, cached, err := r.ResolveGroup(ctx, in.Name, in.Limit, in.ForceRefresh)
		if err != nil {
			return nil, resolveOutput[ResolvedGroup]{}, err
		}
		return nil, resolveOutput[ResolvedGroup]{Candidates: matches, Cached: cached}, nil
	})
}
