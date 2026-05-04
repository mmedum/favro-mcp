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

// resolveTagOutput is the output for favro_resolve_tag. Always
// returns a candidate list (possibly empty) so the LLM can
// disambiguate when more than one tag matches; an exact name
// match scores 1.0 and will sort first.
type resolveTagOutput struct {
	Candidates []ResolvedTag `json:"candidates" jsonschema:"ranked tag candidates; empty when nothing matches"`
	// Cached is true when the underlying tag list came from the
	// in-memory cache (no Favro round-trip happened on this call).
	// Useful for callers that want to know whether a follow-up
	// force_refresh might surface newly-created tags.
	Cached bool `json:"cached" jsonschema:"true when the tag list came from the in-memory cache rather than a fresh Favro fetch"`
}

func registerResolveTag(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: resolveTagToolName,
		Description: "Resolve a tag name (or part of it) to one or more tagId candidates. " +
			"Use this before any tool that needs a tagId so you don't have to walk the " +
			"full tag list yourself. Matching is case-insensitive: exact match scores 1.0, " +
			"prefix match 0.7, substring match 0.4. Returns up to `limit` candidates ranked " +
			"by score descending (default 10, max 50). The tag list is cached for 5 minutes; " +
			"pass `force_refresh: true` to bypass the cache when you suspect a tag was just " +
			"created or renamed. Read-only.",
		Annotations: readOnly("Resolve Favro tag name"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveTagInput) (*mcp.CallToolResult, resolveTagOutput, error) {
		matches, cached, err := r.ResolveTag(ctx, in.Name, in.Limit, in.ForceRefresh)
		if err != nil {
			return nil, resolveTagOutput{}, err
		}
		return nil, resolveTagOutput{Candidates: matches, Cached: cached}, nil
	})
}
