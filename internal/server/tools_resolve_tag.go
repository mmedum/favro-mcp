package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resolveTagToolName = "favro_resolve_tag"

// resolveTagInput is the input for favro_resolve_tag.
type resolveTagInput struct {
	Name         string `json:"name" jsonschema:"the tag name (or part of it) to resolve; matched case-insensitively against every tag in the org"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max candidates to return (default 10, max 50)"`
	ForceRefresh bool   `json:"force_refresh,omitempty" jsonschema:"bypass the 5-minute tag cache and re-fetch the tag list from Favro before matching"`
}

func registerResolveTag(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: resolveTagToolName,
		Description: "Resolve a tag name (or part of it) to one or more tagId candidates. " +
			"Use this before any tool that needs a tagId so you don't have to walk the " +
			"full tag list yourself. " + resolveScoreScaleDoc +
			"The tag list is cached for 5 minutes; pass `force_refresh: true` to bypass " +
			"the cache when you suspect a tag was just created or renamed. Read-only.",
		Annotations: readOnly("Resolve Favro tag name"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveTagInput) (*mcp.CallToolResult, resolveOutput[ResolvedTag], error) {
		matches, cached, err := r.ResolveTag(ctx, in.Name, in.Limit, in.ForceRefresh)
		if err != nil {
			return nil, resolveOutput[ResolvedTag]{}, err
		}
		return nil, resolveOutput[ResolvedTag]{Candidates: matches, Cached: cached}, nil
	})
}
