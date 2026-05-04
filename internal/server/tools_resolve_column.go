package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resolveColumnToolName = "favro_resolve_column"

type resolveColumnInput struct {
	WidgetCommonID string `json:"widget_common_id" jsonschema:"the widget id whose columns to search; required because column names ('Doing', 'Done') repeat across widgets and an unscoped resolver would always return ambiguous garbage"`
	Name           string `json:"name" jsonschema:"the column name (or part of it) to resolve; matched case-insensitively against columns on the given widget"`
	Limit          int    `json:"limit,omitempty" jsonschema:"max candidates to return (default 10, max 50)"`
	ForceRefresh   bool   `json:"force_refresh,omitempty" jsonschema:"bypass the 60-second per-widget column cache and re-fetch from Favro before matching"`
}

func registerResolveColumn(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: resolveColumnToolName,
		Description: "Resolve a column name to one or more columnId candidates on a given widget. " +
			"`widget_common_id` is REQUIRED — column names repeat across widgets and an " +
			"unscoped resolver would return ambiguous garbage. Position is included in each " +
			"candidate so the LLM can disambiguate between similarly-named columns by board " +
			"order. " + resolveScoreScaleDoc +
			"Column lists cache for 60 seconds per widget. Pass `force_refresh: true` to bypass. Read-only.",
		Annotations: readOnly("Resolve Favro column name on a widget"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveColumnInput) (*mcp.CallToolResult, resolveOutput[ResolvedColumn], error) {
		matches, cached, err := r.ResolveColumn(ctx, in.WidgetCommonID, in.Name, in.Limit, in.ForceRefresh)
		if err != nil {
			return nil, resolveOutput[ResolvedColumn]{}, err
		}
		return nil, resolveOutput[ResolvedColumn]{Candidates: matches, Cached: cached}, nil
	})
}
