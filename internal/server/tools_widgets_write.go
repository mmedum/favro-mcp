package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	createWidgetToolName = "favro_create_widget"
	updateWidgetToolName = "favro_update_widget"
	deleteWidgetToolName = "favro_delete_widget"
)

// createWidgetInput is the input for favro_create_widget.
type createWidgetInput struct {
	dryRunInput
	CollectionID          string `json:"collection_id" jsonschema:"the Favro collectionId to create the widget in (required). Resolve via favro_resolve_collection."`
	Name                  string `json:"name" jsonschema:"the widget name (required)"`
	Type                  string `json:"type,omitempty" jsonschema:"widget type: 'backlog', 'board', 'calendar', 'table', or 'matrix'. Empty lets Favro pick its default ('backlog')."`
	Color                 string `json:"color,omitempty" jsonschema:"optional palette color"`
	BreakdownCardCommonID string `json:"breakdown_card_common_id,omitempty" jsonschema:"optional breakdown cardCommonId — pins the widget to a parent card's breakdown view"`
	OwnerRole             string `json:"owner_role,omitempty" jsonschema:"role required to act as owner ('owners', 'fullMembers', 'guests'); Favro picks a default when omitted"`
	EditRole              string `json:"edit_role,omitempty" jsonschema:"role required to edit the widget; same value vocabulary as owner_role"`
}

// updateWidgetInput is the input for favro_update_widget.
type updateWidgetInput struct {
	dryRunInput
	WidgetCommonID        string `json:"widget_common_id" jsonschema:"the Favro widgetCommonId to update. Resolve via favro_resolve_widget if you only have the name."`
	Name                  string `json:"name,omitempty" jsonschema:"new widget name; omit to keep current"`
	Type                  string `json:"type,omitempty" jsonschema:"new widget type; omit to keep current"`
	Color                 string `json:"color,omitempty" jsonschema:"new palette color; omit to keep current"`
	BreakdownCardCommonID string `json:"breakdown_card_common_id,omitempty" jsonschema:"new breakdown cardCommonId; omit to keep current"`
	OwnerRole             string `json:"owner_role,omitempty" jsonschema:"new owner role; omit to keep current"`
	EditRole              string `json:"edit_role,omitempty" jsonschema:"new edit role; omit to keep current"`
	Archive               *bool  `json:"archive,omitempty" jsonschema:"true to archive, false to unarchive; omit to keep current"`
}

// deleteWidgetInput is the input for favro_delete_widget.
type deleteWidgetInput struct {
	dryRunInput
	WidgetCommonID string `json:"widget_common_id" jsonschema:"the Favro widgetCommonId to delete"`
}

func registerCreateWidget(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: createWidgetToolName,
		Description: "Create a new Favro widget (board) inside a collection. `collection_id` " +
			"and `name` are required. `type` defaults to Favro's choice ('backlog') when " +
			"omitted; pass 'board' / 'calendar' / 'table' / 'matrix' to override. Successful " +
			"live writes invalidate the widget cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Create Favro widget", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createWidgetInput) (*mcp.CallToolResult, writeOutput[favro.Widget], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Widget, error) {
				return r.client.CreateWidget(writeCtx, favro.CreateWidgetRequest{
					CollectionID:          in.CollectionID,
					Name:                  in.Name,
					Type:                  in.Type,
					Color:                 in.Color,
					BreakdownCardCommonID: in.BreakdownCardCommonID,
					OwnerRole:             in.OwnerRole,
					EditRole:              in.EditRole,
				})
			},
			func() string {
				kind := in.Type
				if kind == "" {
					kind = "default-type"
				}
				return fmt.Sprintf("would create a new widget %q (%s) in collection %q", in.Name, kind, in.CollectionID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Widget]{}, err
		}
		if !out.DryRun {
			r.invalidateWidgetCache()
		}
		return nil, out, nil
	})
}

func registerUpdateWidget(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: updateWidgetToolName,
		Description: "Update a Favro widget. Every body field is optional — pass at least one. " +
			"`archive: true` archives, `archive: false` unarchives. Successful live writes " +
			"invalidate the widget cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Update Favro widget", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateWidgetInput) (*mcp.CallToolResult, writeOutput[favro.Widget], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Widget, error) {
				return r.client.UpdateWidget(writeCtx, in.WidgetCommonID, favro.UpdateWidgetRequest{
					Name:                  in.Name,
					Type:                  in.Type,
					Color:                 in.Color,
					BreakdownCardCommonID: in.BreakdownCardCommonID,
					OwnerRole:             in.OwnerRole,
					EditRole:              in.EditRole,
					Archive:               in.Archive,
				})
			},
			func() string { return updateWidgetStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Widget]{}, err
		}
		if !out.DryRun {
			r.invalidateWidgetCache()
		}
		return nil, out, nil
	})
}

func registerDeleteWidget(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteWidgetToolName,
		Description: "Delete a Favro widget by its widgetCommonId. Destructive — MCP hosts " +
			"may warn before auto-confirming. Cards on the widget are removed; columns on " +
			"the widget become inaccessible. On success the widget / column / search-cards " +
			"caches are invalidated. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro widget", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteWidgetInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.client.DeleteWidget(writeCtx, in.WidgetCommonID)
			},
			func() string {
				return fmt.Sprintf("would delete widget %q (cards on the widget are removed; columns become inaccessible)", in.WidgetCommonID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		if !out.DryRun {
			r.invalidateWidgetCache()
			r.invalidateAllColumnCaches()
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

// updateWidgetStateDiff renders the dry-run state-diff phrase for
// favro_update_widget.
func updateWidgetStateDiff(in *updateWidgetInput) string {
	type field struct {
		when bool
		desc func() string
	}
	str := func(label, val string) func() string {
		return func() string { return fmt.Sprintf("%s → %q", label, val) }
	}
	bln := func(label string, p *bool) func() string {
		return func() string { return fmt.Sprintf("%s → %t", label, *p) }
	}
	fields := []field{
		{in.Name != "", str("name", in.Name)},
		{in.Type != "", str("type", in.Type)},
		{in.Color != "", str("color", in.Color)},
		{in.BreakdownCardCommonID != "", str("breakdown_card_common_id", in.BreakdownCardCommonID)},
		{in.OwnerRole != "", str("owner_role", in.OwnerRole)},
		{in.EditRole != "", str("edit_role", in.EditRole)},
		{in.Archive != nil, bln("archive", in.Archive)},
	}
	var changes []string
	for _, f := range fields {
		if f.when {
			changes = append(changes, f.desc())
		}
	}
	if len(changes) == 0 {
		return fmt.Sprintf("would PUT widget %q with no changed fields (no-op)", in.WidgetCommonID)
	}
	return fmt.Sprintf("would update widget %q: %s", in.WidgetCommonID, strings.Join(changes, ", "))
}
