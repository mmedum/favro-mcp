package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	appendCardDescriptionToolName    = "favro_append_card_description"
	prependCardDescriptionToolName   = "favro_prepend_card_description"
	replaceInCardDescriptionToolName = "favro_replace_in_card_description"
)

// appendCardDescriptionInput is the input for favro_append_card_description.
type appendCardDescriptionInput struct {
	dryRunInput
	CardID string `json:"card_id" jsonschema:"the per-widget cardId whose description to extend"`
	Text   string `json:"text" jsonschema:"markdown text to append after a blank-line separator. The blank line preserves paragraph boundaries — without it the appended text would fuse into the prior paragraph and silently change rendered structure for list / heading contexts."`
}

// prependCardDescriptionInput is the input for favro_prepend_card_description.
type prependCardDescriptionInput struct {
	dryRunInput
	CardID string `json:"card_id" jsonschema:"the per-widget cardId whose description to prepend to"`
	Text   string `json:"text" jsonschema:"markdown text to insert at the top, followed by a blank-line separator before the existing body"`
}

// replaceInCardDescriptionInput is the input for favro_replace_in_card_description.
type replaceInCardDescriptionInput struct {
	dryRunInput
	CardID  string `json:"card_id" jsonschema:"the per-widget cardId whose description to edit"`
	Find    string `json:"find" jsonschema:"the substring (or regex if use_regex is true) to find. Required."`
	Replace string `json:"replace" jsonschema:"replacement text. Empty replace + non-empty find effectively deletes the matched text."`
	// Count is *int so the JSON-omitted (default-1) case is
	// distinguishable from explicit 0 (replace all). A plain int
	// would collapse the two into the Go zero value.
	Count    *int `json:"count,omitempty" jsonschema:"how many matches to replace; default 1 when omitted. Pass 0 or negative to replace every match. Defaulting to 1 prevents accidental over-replacement when 'find' has multiple occurrences."`
	UseRegex bool `json:"use_regex,omitempty" jsonschema:"if true, 'find' is compiled as a Go regular expression and 'replace' may include $N backrefs. Default false (literal substring match)."`
}

func registerAppendCardDescription(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: appendCardDescriptionToolName,
		Description: "Append markdown text to a Favro card's description, preserving the " +
			"existing markdown structure. Reads the card with `descriptionFormat=markdown` " +
			"so the existing body comes back in the form the editor expects, then PUTs " +
			"`detailedDescription = old + '\\n\\n' + text`. Returns `{old, new, unified_diff}` " +
			"so the LLM can confirm the change is structurally what it intended. Successful " +
			"live writes invalidate the search-cards cache. Pass `dry_run: true` to preview " +
			"the diff without writing.",
		Annotations: mutating("Append to Favro card description", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in appendCardDescriptionInput) (*mcp.CallToolResult, writeOutput[editorResult], error) {
		card, err := r.client.GetCardWithDescriptionFormat(ctx, in.CardID, "markdown")
		if err != nil {
			return nil, writeOutput[editorResult]{}, err
		}
		oldBody := card.DetailedDescription
		newBody := appendDescription(oldBody, in.Text)
		stateDiff := fmt.Sprintf("would append %d chars to card %q description", len(in.Text), in.CardID)
		out, err := runDescriptionEdit(ctx, r, in.CardID, in.DryRun, oldBody, newBody, stateDiff)
		return nil, out, err
	})
}

func registerPrependCardDescription(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: prependCardDescriptionToolName,
		Description: "Prepend markdown text to a Favro card's description, preserving the " +
			"existing markdown structure. Reads the card with `descriptionFormat=markdown`, " +
			"PUTs `detailedDescription = text + '\\n\\n' + old`. Returns `{old, new, " +
			"unified_diff}`. Successful live writes invalidate the search-cards cache. " +
			"Pass `dry_run: true` to preview.",
		Annotations: mutating("Prepend to Favro card description", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in prependCardDescriptionInput) (*mcp.CallToolResult, writeOutput[editorResult], error) {
		card, err := r.client.GetCardWithDescriptionFormat(ctx, in.CardID, "markdown")
		if err != nil {
			return nil, writeOutput[editorResult]{}, err
		}
		oldBody := card.DetailedDescription
		newBody := prependDescription(oldBody, in.Text)
		stateDiff := fmt.Sprintf("would prepend %d chars to card %q description", len(in.Text), in.CardID)
		out, err := runDescriptionEdit(ctx, r, in.CardID, in.DryRun, oldBody, newBody, stateDiff)
		return nil, out, err
	})
}

func registerReplaceInCardDescription(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: replaceInCardDescriptionToolName,
		Description: "Replace text in a Favro card's description. Default `count: 1` so a " +
			"common substring doesn't accidentally rewrite every occurrence; pass `count: 0` " +
			"(or negative) to replace all. `use_regex: true` compiles `find` as a Go regex " +
			"and lets `replace` use `$N` backrefs. If `find` matches nothing the tool returns " +
			"a typed error rather than PUT-ing an unchanged body. Returns `{old, new, " +
			"unified_diff}`. Successful live writes invalidate the search-cards cache. " +
			"Pass `dry_run: true` to preview.",
		Annotations: mutating("Replace in Favro card description", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in replaceInCardDescriptionInput) (*mcp.CallToolResult, writeOutput[editorResult], error) {
		card, err := r.client.GetCardWithDescriptionFormat(ctx, in.CardID, "markdown")
		if err != nil {
			return nil, writeOutput[editorResult]{}, err
		}
		oldBody := card.DetailedDescription
		count := 1
		if in.Count != nil {
			count = *in.Count
		}
		newBody, hits, err := replaceInDescription(oldBody, in.Find, in.Replace, count, in.UseRegex)
		if err != nil {
			return nil, writeOutput[editorResult]{}, err
		}
		if hits == 0 {
			return nil, writeOutput[editorResult]{}, fmt.Errorf("favro: 'find' matched nothing in card %q description; refusing to PUT an unchanged body", in.CardID)
		}
		stateDiff := fmt.Sprintf("would replace %d match(es) of %q in card %q description", hits, in.Find, in.CardID)
		out, err := runDescriptionEdit(ctx, r, in.CardID, in.DryRun, oldBody, newBody, stateDiff)
		return nil, out, err
	})
}

// runDescriptionEdit issues the PUT (or dry-run preview) and projects
// the (old, new, unified_diff) result into writeOutput. Editor tools
// always populate Result — even in dry-run — so the LLM can preview
// the diff. This deviates from the runWrite pattern (which only
// fills Result on live success) because the diff IS the value here:
// dry-run without the diff would force the caller to do the
// computation itself or PUT to find out.
func runDescriptionEdit(
	ctx context.Context,
	r *Resolver,
	cardID string,
	dryRun bool,
	oldBody, newBody, stateDiff string,
) (writeOutput[editorResult], error) {
	result := makeEditorResult(cardID, oldBody, newBody)
	writeCtx := ctx
	if dryRun {
		writeCtx = favro.WithDryRun(ctx)
	}
	_, err := r.client.UpdateCard(writeCtx, cardID, favro.UpdateCardRequest{DetailedDescription: newBody})
	if err == nil {
		r.invalidateSearchCardCache()
		return writeOutput[editorResult]{Result: &result}, nil
	}
	var rec *favro.DryRunRecord
	if errors.As(err, &rec) {
		var body any
		if len(rec.Body) > 0 {
			if jerr := json.Unmarshal(rec.Body, &body); jerr != nil {
				body = string(rec.Body)
			}
		}
		return writeOutput[editorResult]{
			DryRun:             true,
			Result:             &result,
			WouldCall:          &DryRunCall{Method: rec.Method, URL: rec.URL},
			RequestBody:        body,
			PredictedStateDiff: stateDiff,
		}, nil
	}
	return writeOutput[editorResult]{}, err
}
