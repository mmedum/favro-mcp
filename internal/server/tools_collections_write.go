package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	createCollectionToolName = "favro_create_collection"
	updateCollectionToolName = "favro_update_collection"
	deleteCollectionToolName = "favro_delete_collection"
)

// createCollectionInput is the input for favro_create_collection.
type createCollectionInput struct {
	dryRunInput
	Name                     string             `json:"name" jsonschema:"the collection name (required)"`
	Color                    string             `json:"color,omitempty" jsonschema:"optional palette color: blue, red, green, lime, purple, cyan, brown, orange, gray, pink, yellow, slategray"`
	Background               string             `json:"background,omitempty" jsonschema:"optional decorative background name (Favro-defined; e.g. 'forest', 'ocean')"`
	IconName                 string             `json:"icon_name,omitempty" jsonschema:"optional icon name (Favro-defined)"`
	PublicSharing            string             `json:"public_sharing,omitempty" jsonschema:"sharing mode: 'off' (default), 'organization' (every member can see it), or 'public' (read-only public link)"`
	FullMembersCanAddWidgets *bool              `json:"full_members_can_add_widgets,omitempty" jsonschema:"if true, full org members (not just owners) can add widgets to this collection"`
	SharedToUsers            []favro.SharedUser `json:"shared_to_users,omitempty" jsonschema:"explicit user share list; alternative to public_sharing. Each entry has email or userId plus role."`
}

// updateCollectionInput is the input for favro_update_collection.
type updateCollectionInput struct {
	dryRunInput
	CollectionID             string             `json:"collection_id" jsonschema:"the Favro collectionId to update. Resolve via favro_resolve_collection if you only have the name."`
	Name                     string             `json:"name,omitempty" jsonschema:"new collection name; omit to keep current"`
	Color                    string             `json:"color,omitempty" jsonschema:"new palette color; omit to keep current"`
	Background               string             `json:"background,omitempty" jsonschema:"new background name; omit to keep current"`
	IconName                 string             `json:"icon_name,omitempty" jsonschema:"new icon name; omit to keep current"`
	PublicSharing            string             `json:"public_sharing,omitempty" jsonschema:"new sharing mode ('off' / 'organization' / 'public'); omit to keep current"`
	FullMembersCanAddWidgets *bool              `json:"full_members_can_add_widgets,omitempty" jsonschema:"flip the full-members-can-add-widgets flag; omit to keep current"`
	SharedToUsers            []favro.SharedUser `json:"shared_to_users,omitempty" jsonschema:"new explicit share list; replaces the existing list when provided"`
	Archive                  *bool              `json:"archive,omitempty" jsonschema:"true to archive, false to unarchive; omit to keep current archive state"`
}

// deleteCollectionInput is the input for favro_delete_collection.
type deleteCollectionInput struct {
	dryRunInput
	CollectionID string `json:"collection_id" jsonschema:"the Favro collectionId to delete"`
}

func registerCreateCollection(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: createCollectionToolName,
		Description: "Create a new Favro collection. `name` is required. Sharing defaults " +
			"to 'off' (only the creator can see it); pass `public_sharing: 'organization'` " +
			"for org-wide visibility. Successful live writes invalidate the collection cache. " +
			"Pass `dry_run: true` to preview the request without contacting Favro.",
		Annotations: mutating("Create Favro collection", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCollectionInput) (*mcp.CallToolResult, writeOutput[favro.Collection], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Collection, error) {
				return r.client.CreateCollection(writeCtx, favro.CreateCollectionRequest{
					Name:                     in.Name,
					SharedToUsers:            in.SharedToUsers,
					PublicSharing:            in.PublicSharing,
					Background:               in.Background,
					Color:                    in.Color,
					IconName:                 in.IconName,
					FullMembersCanAddWidgets: in.FullMembersCanAddWidgets,
				})
			},
			func() string {
				return fmt.Sprintf("would create a new collection named %q in the active organization", in.Name)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		if !out.DryRun {
			r.invalidateCollectionCache()
		}
		return nil, out, nil
	})
}

func registerUpdateCollection(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: updateCollectionToolName,
		Description: "Update a Favro collection. Every body field is optional — pass at " +
			"least one. `archive: true` archives, `archive: false` unarchives, omit to keep " +
			"current. Successful live writes invalidate the collection cache. Pass " +
			"`dry_run: true` to preview.",
		Annotations: mutating("Update Favro collection", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCollectionInput) (*mcp.CallToolResult, writeOutput[favro.Collection], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Collection, error) {
				return r.client.UpdateCollection(writeCtx, in.CollectionID, favro.UpdateCollectionRequest{
					Name:                     in.Name,
					PublicSharing:            in.PublicSharing,
					Background:               in.Background,
					Color:                    in.Color,
					IconName:                 in.IconName,
					FullMembersCanAddWidgets: in.FullMembersCanAddWidgets,
					SharedToUsers:            in.SharedToUsers,
					Archive:                  in.Archive,
				})
			},
			func() string { return updateCollectionStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		if !out.DryRun {
			r.invalidateCollectionCache()
		}
		return nil, out, nil
	})
}

func registerDeleteCollection(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteCollectionToolName,
		Description: "Delete a Favro collection by its collectionId. Destructive — MCP hosts " +
			"may warn before auto-confirming. Favro does not cascade-delete widgets when a " +
			"collection is removed; widgets that lived only in this collection may become " +
			"orphaned. On success the collection / widget / search-cards caches are " +
			"invalidated. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro collection", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteCollectionInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.client.DeleteCollection(writeCtx, in.CollectionID)
			},
			func() string {
				return fmt.Sprintf("would delete collection %q from the active organization", in.CollectionID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		if !out.DryRun {
			r.invalidateCollectionCache()
			r.invalidateWidgetCache()
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

// updateCollectionStateDiff renders the per-tool dry-run state-diff
// phrase for favro_update_collection.
func updateCollectionStateDiff(in *updateCollectionInput) string {
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
		{in.Color != "", str("color", in.Color)},
		{in.Background != "", str("background", in.Background)},
		{in.IconName != "", str("icon_name", in.IconName)},
		{in.PublicSharing != "", str("public_sharing", in.PublicSharing)},
		{in.FullMembersCanAddWidgets != nil, bln("full_members_can_add_widgets", in.FullMembersCanAddWidgets)},
		{in.Archive != nil, bln("archive", in.Archive)},
		{len(in.SharedToUsers) > 0, func() string { return fmt.Sprintf("shared_to_users (%d)", len(in.SharedToUsers)) }},
	}
	var changes []string
	for _, f := range fields {
		if f.when {
			changes = append(changes, f.desc())
		}
	}
	if len(changes) == 0 {
		return fmt.Sprintf("would PUT collection %q with no changed fields (no-op)", in.CollectionID)
	}
	return fmt.Sprintf("would update collection %q: %s", in.CollectionID, strings.Join(changes, ", "))
}
