package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resolveUserToolName = "favro_resolve_user"

type resolveUserInput struct {
	Name         string `json:"name" jsonschema:"the user name OR email (or part of either) to resolve; matched case-insensitively against every user in the org"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max candidates to return (default 10, max 50)"`
	ForceRefresh bool   `json:"force_refresh,omitempty" jsonschema:"bypass the 5-minute user cache and re-fetch the user list from Favro before matching"`
}

func registerResolveUser(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: resolveUserToolName,
		Description: "Resolve a user name or email (or part of either) to one or more userId " +
			"candidates. Matches the better of name and email score. Email is included in " +
			"the response so an LLM disambiguating between two users with similar names can " +
			"show the email to the human. " + resolveScoreScaleDoc +
			"The user list is cached for 5 minutes; pass `force_refresh: true` to bypass " +
			"when you suspect a member was just added. Read-only.",
		Annotations: readOnly("Resolve Favro user name or email"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in resolveUserInput) (*mcp.CallToolResult, resolveOutput[ResolvedUser], error) {
		matches, cached, err := r.ResolveUser(ctx, in.Name, in.Limit, in.ForceRefresh)
		if err != nil {
			return nil, resolveOutput[ResolvedUser]{}, err
		}
		return nil, resolveOutput[ResolvedUser]{Candidates: matches, Cached: cached}, nil
	})
}
