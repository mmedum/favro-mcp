package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	listDependenciesToolName      = "favro_list_dependencies"
	addDependenciesToolName       = "favro_add_dependencies"
	replaceDependenciesToolName   = "favro_replace_dependencies"
	updateDependencyToolName      = "favro_update_dependency"
	deleteDependencyToolName      = "favro_delete_dependency"
	deleteAllDependenciesToolName = "favro_delete_all_dependencies"
)

// dependencyDirectionDoc is the shared explanation of what isBefore
// means, embedded in every tool description that takes it. Getting
// the direction backwards silently inverts the schedule, so the
// wording lives in one place rather than being paraphrased per tool.
const dependencyDirectionDoc = "`is_before: true` means the linked card must be done BEFORE this one; " +
	"`is_before: false` means it comes after. "

type listDependenciesInput struct {
	CardID string `json:"card_id" jsonschema:"the per-widget cardId whose dependencies to list"`
}

type writeDependenciesInput struct {
	dryRunInput
	CardID       string                       `json:"card_id" jsonschema:"the per-widget cardId to link from"`
	Dependencies []favro.CardDependencyOption `json:"dependencies" jsonschema:"the links to set, each {cardId, isBefore}. cardId is the per-widget cardId of the OTHER card."`
}

type updateDependencyInput struct {
	dryRunInput
	CardID           string `json:"card_id" jsonschema:"the per-widget cardId that owns the dependency"`
	DependencyCardID string `json:"dependency_card_id" jsonschema:"the per-widget cardId of the linked card whose direction to change"`
	IsBefore         *bool  `json:"is_before" jsonschema:"true if the linked card must be done before this one, false if after"`
}

type deleteDependencyInput struct {
	dryRunInput
	CardID           string `json:"card_id" jsonschema:"the per-widget cardId that owns the dependency"`
	DependencyCardID string `json:"dependency_card_id" jsonschema:"the per-widget cardId of the link to remove"`
}

type deleteAllDependenciesInput struct {
	dryRunInput
	CardID string `json:"card_id" jsonschema:"the per-widget cardId to clear every dependency from"`
}

func registerDependencies(srv *mcp.Server, client *favro.Client) {
	registerReadDependencies(srv, client)
	registerWriteDependencies(srv, client)
	registerDeleteDependencies(srv, client)
}

func registerReadDependencies(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: listDependenciesToolName,
		Description: "List a Favro card's dependencies — the before/after links to other " +
			"cards. Not paginated: Favro returns the full list in one response. Each entry " +
			"names the other card and whether it comes before or after this one. Read-only.",
		Annotations: readOnly("List Favro card dependencies"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listDependenciesInput) (*mcp.CallToolResult, favro.CardDependencies, error) {
		deps, err := client.ListDependencies(ctx, in.CardID)
		if err != nil {
			return nil, favro.CardDependencies{}, err
		}
		return nil, deps, nil
	})
}

func registerWriteDependencies(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: addDependenciesToolName,
		Description: "ADD dependencies to a Favro card, keeping the ones already there. " +
			dependencyDirectionDoc +
			"Returns the card's full dependency list afterwards. Use " +
			"favro_replace_dependencies to overwrite the whole list instead. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Add Favro card dependencies", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in writeDependenciesInput) (*mcp.CallToolResult, writeOutput[favro.CardDependencies], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.CardDependencies, error) {
				return client.CreateDependencies(writeCtx, in.CardID, in.Dependencies)
			},
			func() string {
				return fmt.Sprintf("would add %d dependenc(ies) to card %q, keeping existing ones", len(in.Dependencies), in.CardID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.CardDependencies]{}, err
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: replaceDependenciesToolName,
		Description: "REPLACE a Favro card's dependency list: every existing link is removed " +
			"and the supplied set becomes the whole list. " + dependencyDirectionDoc +
			"Use favro_add_dependencies to add without clearing. Returns the resulting " +
			"list. Destructive — MCP hosts may warn before auto-confirming. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Replace Favro card dependencies", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in writeDependenciesInput) (*mcp.CallToolResult, writeOutput[favro.CardDependencies], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.CardDependencies, error) {
				return client.ReplaceDependencies(writeCtx, in.CardID, in.Dependencies)
			},
			func() string {
				return fmt.Sprintf("would REPLACE every dependency on card %q with %d new one(s)", in.CardID, len(in.Dependencies))
			},
		)
		if err != nil {
			return nil, writeOutput[favro.CardDependencies]{}, err
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: updateDependencyToolName,
		Description: "Flip the direction of one existing dependency on a Favro card. " +
			dependencyDirectionDoc + "Returns the card's full dependency list afterwards. " +
			"Pass `dry_run: true` to preview.",
		Annotations: mutating("Update Favro card dependency", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateDependencyInput) (*mcp.CallToolResult, writeOutput[favro.CardDependencies], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.CardDependencies, error) {
				return client.UpdateDependency(writeCtx, in.CardID, in.DependencyCardID, favro.UpdateDependencyRequest{
					IsBefore: in.IsBefore,
				})
			},
			func() string {
				return fmt.Sprintf("would change the direction of card %q's dependency on %q", in.CardID, in.DependencyCardID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.CardDependencies]{}, err
		}
		return nil, out, nil
	})
}

func registerDeleteDependencies(srv *mcp.Server, client *favro.Client) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteDependencyToolName,
		Description: "Remove one dependency link from a Favro card. The cards themselves are " +
			"untouched — only the link between them goes away. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro card dependency", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteDependencyInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, client.DeleteDependency(writeCtx, in.CardID, in.DependencyCardID)
			},
			func() string {
				return fmt.Sprintf("would unlink card %q from %q", in.CardID, in.DependencyCardID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteAllDependenciesToolName,
		Description: "Remove EVERY dependency link from a Favro card. The cards themselves " +
			"are untouched. Destructive — MCP hosts may warn before auto-confirming. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Delete all Favro card dependencies", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteAllDependenciesInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) { return struct{}{}, client.DeleteAllDependencies(writeCtx, in.CardID) },
			func() string { return fmt.Sprintf("would remove every dependency from card %q", in.CardID) },
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		return nil, out, nil
	})
}
