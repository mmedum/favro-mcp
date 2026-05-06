package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	addTagToCardToolName      = "favro_add_tag_to_card"
	removeTagFromCardToolName = "favro_remove_tag_from_card"
)

// errTagToCardUnknown is returned when no exact-match tag exists for
// the supplied tag_name. The MCP error message points the LLM at
// favro_create_tag explicitly — auto-creating from a typo is the
// failure mode plan §6a wants prevented at all costs.
var errTagToCardUnknown = errors.New("favro: tag name not found in active organization (typo? to add a brand-new tag, call favro_create_tag explicitly)")

// errTagToCardAmbiguous is returned when multiple tags share the
// requested name (Favro doesn't enforce name uniqueness). The
// caller should use favro_update_card with an explicit add_tag_ids
// containing the correct tagId.
var errTagToCardAmbiguous = errors.New("favro: multiple tags share this exact name; pick one via favro_resolve_tag and use favro_update_card with add_tag_ids / remove_tag_ids directly")

// addTagToCardInput / removeTagFromCardInput share the same shape;
// the action distinguishes them at the favro layer.
type addTagToCardInput struct {
	dryRunInput
	CardID  string `json:"card_id" jsonschema:"the per-widget cardId to tag"`
	TagName string `json:"tag_name" jsonschema:"exact tag name (case-insensitive). The tool hard-fails if no exact match exists — auto-creating from a typo is intentionally not supported. To add a new tag, call favro_create_tag first."`
}

type removeTagFromCardInput struct {
	dryRunInput
	CardID  string `json:"card_id" jsonschema:"the per-widget cardId to untag"`
	TagName string `json:"tag_name" jsonschema:"exact tag name (case-insensitive)"`
}

func registerAddTagToCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: addTagToCardToolName,
		Description: "Add an existing org-global tag to a Favro card by tag NAME. Hard-fails " +
			"if the name doesn't match exactly — typo prevention is the whole point. To add " +
			"a brand-new tag, call favro_create_tag first, THEN this tool. To use a tagId " +
			"directly (or to bypass the exact-match guard), call favro_update_card with " +
			"add_tag_ids. Successful live writes invalidate the search-cards cache. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Add Favro tag to card by name", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addTagToCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		return runTagToCard(ctx, r, in.CardID, in.TagName, in.DryRun, true)
	})
}

func registerRemoveTagFromCard(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: removeTagFromCardToolName,
		Description: "Remove an org-global tag from a Favro card by tag NAME. Hard-fails if " +
			"the name doesn't match exactly. Successful live writes invalidate the " +
			"search-cards cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Remove Favro tag from card by name", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in removeTagFromCardInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		return runTagToCard(ctx, r, in.CardID, in.TagName, in.DryRun, false)
	})
}

// runTagToCard resolves tag_name to an exact-match tagId and PUTs
// /cards/{cardId} with addTagIds or removeTagIds populated.
// adding=true → add; adding=false → remove. Symmetric across the
// two MCP tools because Favro's UpdateCard endpoint accepts both
// shapes; the only thing that differs is which list the resolved id
// lands in.
func runTagToCard(
	ctx context.Context,
	r *Resolver,
	cardID, tagName string,
	dryRun bool,
	adding bool,
) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
	tagID, err := resolveExactTagID(ctx, r, tagName)
	if err != nil {
		return nil, writeOutput[favro.Card]{}, err
	}
	writeCtx := ctx
	if dryRun {
		writeCtx = favro.WithDryRun(ctx)
	}
	out, err := runWrite(
		func() (favro.Card, error) {
			req := favro.UpdateCardRequest{}
			if adding {
				req.AddTagIDs = []string{tagID}
			} else {
				req.RemoveTagIDs = []string{tagID}
			}
			return r.client.UpdateCard(writeCtx, cardID, req)
		},
		func() string {
			verb := "add tag"
			prep := "to"
			if !adding {
				verb = "remove tag"
				prep = "from"
			}
			return fmt.Sprintf("would %s %q (tagId %q) %s card %q", verb, tagName, tagID, prep, cardID)
		},
	)
	if err != nil {
		return nil, writeOutput[favro.Card]{}, err
	}
	if !out.DryRun {
		r.invalidateSearchCardCache()
	}
	return nil, out, nil
}

// resolveExactTagID returns the single tagId whose name matches
// tag_name exactly (case-insensitive). errTagToCardUnknown when
// nothing matches; errTagToCardAmbiguous when >1 tags share the
// exact name (pathological but not blocked server-side).
func resolveExactTagID(ctx context.Context, r *Resolver, tagName string) (string, error) {
	if tagName == "" {
		return "", fmt.Errorf("favro: tag_name is required")
	}
	tags, _, err := r.listAllTags(ctx, false)
	if err != nil {
		return "", err
	}
	lower := strings.ToLower(tagName)
	var matches []favro.Tag
	for _, t := range tags {
		if strings.ToLower(t.Name) == lower {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: %q", errTagToCardUnknown, tagName)
	case 1:
		return matches[0].TagID, nil
	default:
		return "", fmt.Errorf("%w (%d matches for %q)", errTagToCardAmbiguous, len(matches), tagName)
	}
}
