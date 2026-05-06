package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	createColumnToolName = "favro_create_column"
	updateColumnToolName = "favro_update_column"
	deleteColumnToolName = "favro_delete_column"
)

// createColumnInput is the input for favro_create_column.
type createColumnInput struct {
	dryRunInput
	WidgetCommonID string `json:"widget_common_id" jsonschema:"the Favro widgetCommonId to create the column on (required). Resolve via favro_resolve_widget."`
	Name           string `json:"name" jsonschema:"the column name (required)"`
	Color          string `json:"color,omitempty" jsonschema:"optional palette color"`
	Position       *int   `json:"position,omitempty" jsonschema:"optional 0-based slot in the column ordering. Omit to append to the end."`
}

// updateColumnInput is the input for favro_update_column.
type updateColumnInput struct {
	dryRunInput
	ColumnID       string `json:"column_id" jsonschema:"the Favro columnId to update"`
	WidgetCommonID string `json:"widget_common_id,omitempty" jsonschema:"optional widgetCommonId; if known, lets the cache invalidation be scoped to one widget rather than sweeping all column caches"`
	Name           string `json:"name,omitempty" jsonschema:"new column name; omit to keep current"`
	Color          string `json:"color,omitempty" jsonschema:"new palette color; omit to keep current"`
	Position       *int   `json:"position,omitempty" jsonschema:"new 0-based position; omit to keep current"`
}

// deleteColumnInput is the input for favro_delete_column.
type deleteColumnInput struct {
	dryRunInput
	ColumnID       string `json:"column_id" jsonschema:"the Favro columnId to delete"`
	WidgetCommonID string `json:"widget_common_id,omitempty" jsonschema:"optional widgetCommonId; if known, lets the cache invalidation be scoped to one widget rather than sweeping all column caches"`
}

func registerCreateColumn(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: createColumnToolName,
		Description: "Create a new column on a Favro widget. `widget_common_id` and `name` " +
			"are required. `position` is 0-based; omit to append. Successful live writes " +
			"invalidate the column cache for the widget. Pass `dry_run: true` to preview.",
		Annotations: mutating("Create Favro column", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createColumnInput) (*mcp.CallToolResult, writeOutput[favro.Column], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Column, error) {
				return r.client.CreateColumn(writeCtx, favro.CreateColumnRequest{
					WidgetCommonID: in.WidgetCommonID,
					Name:           in.Name,
					Color:          in.Color,
					Position:       in.Position,
				})
			},
			func() string {
				return fmt.Sprintf("would create a new column %q on widget %q", in.Name, in.WidgetCommonID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Column]{}, err
		}
		if !out.DryRun {
			invalidateColumnCacheScoped(r, in.WidgetCommonID)
		}
		return nil, out, nil
	})
}

func registerUpdateColumn(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: updateColumnToolName,
		Description: "Update a Favro column. Every body field is optional — pass at least one. " +
			"Pass `widget_common_id` if known so the cache invalidation can be scoped to one " +
			"widget; otherwise every cached column list is dropped on success. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Update Favro column", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateColumnInput) (*mcp.CallToolResult, writeOutput[favro.Column], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Column, error) {
				return r.client.UpdateColumn(writeCtx, in.ColumnID, favro.UpdateColumnRequest{
					Name:     in.Name,
					Color:    in.Color,
					Position: in.Position,
				})
			},
			func() string { return updateColumnStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Column]{}, err
		}
		if !out.DryRun {
			invalidateColumnCacheScoped(r, in.WidgetCommonID)
		}
		return nil, out, nil
	})
}

func registerDeleteColumn(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteColumnToolName,
		Description: "Delete a Favro column by its columnId. Destructive — MCP hosts may warn " +
			"before auto-confirming. Favro forbids deleting a column that contains cards " +
			"(returns HTTP 400); move or archive the cards out first. Pass `widget_common_id` " +
			"if known for scoped cache invalidation. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro column", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteColumnInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.client.DeleteColumn(writeCtx, in.ColumnID)
			},
			func() string {
				return fmt.Sprintf("would delete column %q (Favro rejects this with HTTP 400 if the column still has cards on it)", in.ColumnID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		if !out.DryRun {
			invalidateColumnCacheScoped(r, in.WidgetCommonID)
		}
		return nil, out, nil
	})
}

// invalidateColumnCacheScoped is the prefix-sweep fallback for column
// writes that don't have the parent widgetCommonID — fetching it
// would be an extra round-trip; sweeping is cheap and the cache
// re-warms lazily.
func invalidateColumnCacheScoped(r *Resolver, widgetCommonID string) {
	if widgetCommonID != "" {
		r.invalidateColumnCache(widgetCommonID)
		return
	}
	r.invalidateAllColumnCaches()
}

// updateColumnStateDiff renders the dry-run state-diff phrase for
// favro_update_column.
func updateColumnStateDiff(in *updateColumnInput) string {
	type field struct {
		when bool
		desc func() string
	}
	str := func(label, val string) func() string {
		return func() string { return fmt.Sprintf("%s → %q", label, val) }
	}
	num := func(label string, p *int) func() string {
		return func() string { return fmt.Sprintf("%s → %d", label, *p) }
	}
	fields := []field{
		{in.Name != "", str("name", in.Name)},
		{in.Color != "", str("color", in.Color)},
		{in.Position != nil, num("position", in.Position)},
	}
	var changes []string
	for _, f := range fields {
		if f.when {
			changes = append(changes, f.desc())
		}
	}
	if len(changes) == 0 {
		return fmt.Sprintf("would PUT column %q with no changed fields (no-op)", in.ColumnID)
	}
	return fmt.Sprintf("would update column %q: %s", in.ColumnID, strings.Join(changes, ", "))
}
