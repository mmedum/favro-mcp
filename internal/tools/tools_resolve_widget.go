package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/service"
)

const resolveWidgetToolName = "favro_resolve_widget"

type resolveWidgetInput struct {
	Name         string `json:"name" jsonschema:"the widget (board) name (or part of it) to resolve; matched case-insensitively"`
	CollectionID string `json:"collection_id,omitempty" jsonschema:"optional collection id to scope the search to widgets that belong to that collection (applied client-side after the org-wide list is fetched)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max candidates to return (default 10, max 50)"`
	ForceRefresh bool   `json:"force_refresh,omitempty" jsonschema:"bypass the 60-second widget cache and re-fetch from Favro before matching"`
}

func registerResolveWidget(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: resolveWidgetToolName,
		Description: "Resolve a widget (board) name (or part of it) to one or more widgetCommonId " +
			"candidates. Optional `collection_id` restricts results to widgets that belong to " +
			"that collection — useful when the same widget name lives in multiple collections. " +
			resolveScoreScaleDoc +
			"The widget list caches for 60 seconds (short TTL because widgets are added/renamed " +
			"mid-session). Pass `force_refresh: true` to bypass. Read-only.",
		Annotations: readOnly("Resolve Favro widget name"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveWidgetInput) (*mcp.CallToolResult, resolveOutput[service.ResolvedWidget], error) {
		matches, cached, err := r.ResolveWidget(ctx, in.Name, in.CollectionID, in.Limit, in.ForceRefresh)
		if err != nil {
			return nil, resolveOutput[service.ResolvedWidget]{}, err
		}
		return nil, resolveOutput[service.ResolvedWidget]{Candidates: matches, Cached: cached}, nil
	})
}
