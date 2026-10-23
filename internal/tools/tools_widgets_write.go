package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
	"github.com/mmedum/favro-mcp/v4/internal/favroapi"
	"github.com/mmedum/favro-mcp/v4/internal/render"
	"github.com/mmedum/favro-mcp/v4/internal/service"
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
	OwnerRole             string `json:"owner_role,omitempty" jsonschema:"role required to act as owner ('owners' or 'fullMembers'); Favro picks a default when omitted. 'guests' is refused: it makes people outside the organization owners."`
	EditRole              string `json:"edit_role,omitempty" jsonschema:"role required to edit the widget ('owners' or 'fullMembers'); Favro picks a default when omitted. 'guests' is refused: it lets people outside the organization edit it."`
}

// updateWidgetInput is the input for favro_update_widget.
type updateWidgetInput struct {
	dryRunInput
	WidgetCommonID        string `json:"widget_common_id" jsonschema:"the Favro widgetCommonId to update. Resolve via favro_resolve_widget if you only have the name."`
	Name                  string `json:"name,omitempty" jsonschema:"new widget name; omit to keep current"`
	Type                  string `json:"type,omitempty" jsonschema:"new widget type; omit to keep current"`
	Color                 string `json:"color,omitempty" jsonschema:"new palette color; omit to keep current"`
	BreakdownCardCommonID string `json:"breakdown_card_common_id,omitempty" jsonschema:"new breakdown cardCommonId; omit to keep current"`
	OwnerRole             string `json:"owner_role,omitempty" jsonschema:"new owner role ('owners' or 'fullMembers'); omit to keep current. 'guests' is refused: it makes people outside the organization owners."`
	EditRole              string `json:"edit_role,omitempty" jsonschema:"new edit role ('owners' or 'fullMembers'); omit to keep current. 'guests' is refused: it lets people outside the organization edit it."`
	Archive               *bool  `json:"archive,omitempty" jsonschema:"true to archive, false to unarchive; omit to keep current. Requires collection_id."`
	CollectionID          string `json:"collection_id,omitempty" jsonschema:"the collectionId the archive applies to. REQUIRED whenever archive is set — a widget can belong to several collections."`
}

// deleteWidgetInput is the input for favro_delete_widget. The wider
// delete is never what an omission gets: a call names one collection,
// or says everywhere.
type deleteWidgetInput struct {
	dryRunInput
	confirmInput
	WidgetCommonID string `json:"widget_common_id" jsonschema:"the Favro widgetCommonId to delete"`
	CollectionID   string `json:"collection_id,omitempty" jsonschema:"delete the widget from this collection only. Pass this or everywhere: true, not both."`
	Everywhere     bool   `json:"everywhere,omitempty" jsonschema:"true deletes every instance of the widget, in every collection it belongs to. Pass this or collection_id, not both."`
}

// errDeleteWidgetScope is a favro_delete_widget call that names neither
// one collection nor every one, or both.
var errDeleteWidgetScope = render.Sentinel(render.ClassInvalid,
	"favro_delete_widget: pass collection_id to delete the widget from one collection, or everywhere: true to delete it from every collection it is in; exactly one of them")

func registerCreateWidget(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: createWidgetToolName,
		Description: "Create a new Favro widget (board) inside a collection. `collection_id` " +
			"and `name` are required. `type` defaults to Favro's choice ('backlog') when " +
			"omitted; pass 'board' / 'calendar' / 'table' / 'matrix' to override. Successful " +
			"live writes invalidate the widget cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Create Favro widget", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createWidgetInput) (*mcp.CallToolResult, writeOutput[favro.Widget], error) {
		if err := checkWidgetRoles(in.OwnerRole, in.EditRole); err != nil {
			return nil, writeOutput[favro.Widget]{}, err
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Widget, error) {
				return r.Client().CreateWidget(writeCtx, favro.CreateWidgetRequest{
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
			r.InvalidateWidgetCache()
		}
		return nil, out, nil
	})
}

func registerUpdateWidget(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: updateWidgetToolName,
		Description: "Update a Favro widget. Every body field is optional — pass at least one. " +
			"`archive: true` archives, `archive: false` unarchives — both require " +
			"`collection_id`, because a widget can belong to several collections and Favro " +
			"scopes the archive to one of them. Successful live writes invalidate the " +
			"widget cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Update Favro widget", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateWidgetInput) (*mcp.CallToolResult, writeOutput[favro.Widget], error) {
		if err := checkWidgetRoles(in.OwnerRole, in.EditRole); err != nil {
			return nil, writeOutput[favro.Widget]{}, err
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Widget, error) {
				return r.Client().UpdateWidget(writeCtx, in.WidgetCommonID, favro.UpdateWidgetRequest{
					Name:                  in.Name,
					Type:                  in.Type,
					Color:                 in.Color,
					BreakdownCardCommonID: in.BreakdownCardCommonID,
					OwnerRole:             in.OwnerRole,
					EditRole:              in.EditRole,
					Archive:               in.Archive,
					CollectionID:          in.CollectionID,
				})
			},
			func() string { return updateWidgetStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Widget]{}, err
		}
		if !out.DryRun {
			r.InvalidateWidgetCache()
		}
		return nil, out, nil
	})
}

func registerDeleteWidget(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: deleteWidgetToolName,
		Description: "Delete a Favro widget by its widgetCommonId. Destructive — MCP hosts " +
			"may warn before auto-confirming. Cards on the widget are removed; columns on " +
			"the widget become inaccessible. Pass `collection_id` to remove the widget from " +
			"just that collection, or `everywhere: true` to delete every instance across all " +
			"collections the widget belongs to; one of the two is required. On success the " +
			"widget / column / search-cards caches are invalidated. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro widget", true),
		Meta:        promptEveryCall(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteWidgetInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		if (in.CollectionID == "") == !in.Everywhere {
			return nil, writeOutput[struct{}]{}, errDeleteWidgetScope
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.Client().DeleteWidget(writeCtx, in.WidgetCommonID, in.CollectionID)
			},
			func() string {
				if in.CollectionID != "" {
					return fmt.Sprintf("would delete widget %q from collection %q (cards on the widget are removed; columns become inaccessible)", in.WidgetCommonID, in.CollectionID)
				}
				return fmt.Sprintf("would delete widget %q from EVERY collection it belongs to (cards on the widget are removed; columns become inaccessible)", in.WidgetCommonID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		if !out.DryRun {
			r.InvalidateWidgetCache()
			r.InvalidateAllColumnCaches()
			r.InvalidateSearchCardCache()
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
		{in.CollectionID != "", str("collection_id", in.CollectionID)},
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
