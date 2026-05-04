package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resolveCollectionToolName = "favro_resolve_collection"

type resolveCollectionInput struct {
	Name         string `json:"name" jsonschema:"the collection name (or part of it) to resolve; matched case-insensitively"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max candidates to return (default 10, max 50)"`
	ForceRefresh bool   `json:"force_refresh,omitempty" jsonschema:"bypass the 60-second collection cache and re-fetch from Favro before matching"`
}

func registerResolveCollection(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: resolveCollectionToolName,
		Description: "Resolve a collection name (or part of it) to one or more collectionId " +
			"candidates. " + resolveScoreScaleDoc +
			"Collections cache for 60 seconds (shorter than the 5-minute org-metadata caches " +
			"because users add and rename collections mid-session). Pass `force_refresh: true` " +
			"to bypass. Read-only.",
		Annotations: readOnly("Resolve Favro collection name"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveCollectionInput) (*mcp.CallToolResult, resolveOutput[ResolvedCollection], error) {
		matches, cached, err := r.ResolveCollection(ctx, in.Name, in.Limit, in.ForceRefresh)
		if err != nil {
			return nil, resolveOutput[ResolvedCollection]{}, err
		}
		return nil, resolveOutput[ResolvedCollection]{Candidates: matches, Cached: cached}, nil
	})
}
