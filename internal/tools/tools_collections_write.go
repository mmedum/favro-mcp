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
	createCollectionToolName       = "favro_create_collection"
	updateCollectionToolName       = "favro_update_collection"
	deleteCollectionToolName       = "favro_delete_collection"
	widenCollectionSharingToolName = "favro_widen_collection_sharing"
)

// createCollectionInput is the input for favro_create_collection.
type createCollectionInput struct {
	dryRunInput
	Name                     string             `json:"name" jsonschema:"the collection name (required)"`
	Color                    string             `json:"color,omitempty" jsonschema:"optional palette color: blue, red, green, lime, purple, cyan, brown, orange, gray, pink, yellow, slategray"`
	Background               string             `json:"background,omitempty" jsonschema:"optional decorative background name (Favro-defined; e.g. 'forest', 'ocean')"`
	IconName                 string             `json:"icon_name,omitempty" jsonschema:"optional icon name (Favro-defined)"`
	PublicSharing            string             `json:"public_sharing,omitempty" jsonschema:"'users', the default: only the people and groups it is shared with. 'organization' and 'public' are refused; favro_widen_collection_sharing sets them."`
	FullMembersCanAddWidgets *bool              `json:"full_members_can_add_widgets,omitempty" jsonschema:"if true, full org members (not just owners) can add widgets to this collection"`
	StarPage                 *bool              `json:"star_page,omitempty" jsonschema:"if true, star the new collection for the authenticated user"`
	ShareToUsers             []favro.SharedUser `json:"share_to_users,omitempty" jsonschema:"people and groups to share the collection with. Each entry names a member of the organization by userId (from favro_resolve_user), or a group by type 'group' and groupId, plus a role (guest / view / edit). An email, type 'organization', role 'admin' or someone who is not a member of the organization is refused."`
}

// updateCollectionInput is the input for favro_update_collection.
type updateCollectionInput struct {
	dryRunInput
	CollectionID             string             `json:"collection_id" jsonschema:"the Favro collectionId to update. Resolve via favro_resolve_collection if you only have the name."`
	Name                     string             `json:"name,omitempty" jsonschema:"new collection name; omit to keep current"`
	Color                    string             `json:"color,omitempty" jsonschema:"new palette color; omit to keep current"`
	Background               string             `json:"background,omitempty" jsonschema:"new background name; omit to keep current"`
	IconName                 string             `json:"icon_name,omitempty" jsonschema:"new icon name; omit to keep current"`
	PublicSharing            string             `json:"public_sharing,omitempty" jsonschema:"'users' narrows the collection to the people and groups it is shared with; omit to keep current. 'organization' and 'public' are refused; favro_widen_collection_sharing sets them."`
	FullMembersCanAddWidgets *bool              `json:"full_members_can_add_widgets,omitempty" jsonschema:"flip the full-members-can-add-widgets flag; omit to keep current"`
	StarPage                 *bool              `json:"star_page,omitempty" jsonschema:"true to star the collection for the authenticated user, false to unstar"`
	ShareToUsers             []favro.SharedUser `json:"share_to_users,omitempty" jsonschema:"people and groups to ADD to the collection. Each entry names a member of the organization by userId (from favro_resolve_user), or a group by type 'group' and groupId, plus a role. An email, type 'organization', role 'admin' or someone who is not a member of the organization is refused."`
	Members                  []favro.SharedUser `json:"members,omitempty" jsonschema:"role changes for people ALREADY in the collection. Each entry names them by userId plus either a new role or delete: true to remove them. Making someone admin who is not one already is refused."`
	Archive                  *bool              `json:"archive,omitempty" jsonschema:"true to archive, false to unarchive; omit to keep current archive state"`
}

// deleteCollectionInput is the input for favro_delete_collection.
type deleteCollectionInput struct {
	dryRunInput
	confirmInput
	CollectionID string `json:"collection_id" jsonschema:"the Favro collectionId to delete"`
}

// widenCollectionSharingInput is the input for
// favro_widen_collection_sharing. collection_name is a witness: the
// person approving the call sees which collection it opens, where an id
// says nothing.
type widenCollectionSharingInput struct {
	dryRunInput
	confirmInput
	CollectionID   string `json:"collection_id" jsonschema:"the Favro collectionId to open"`
	CollectionName string `json:"collection_name" jsonschema:"the collection's name exactly as Favro holds it, from favro_get_collection. It shows the person approving the call which collection this opens; a name that differs from Favro's is refused [conflict] and nothing is changed."`
	PublicSharing  string `json:"public_sharing" jsonschema:"'organization' for every member of the organization, or 'public' for anyone on the internet with the link, without signing in"`
}

func registerCreateCollection(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: createCollectionToolName,
		Description: "Create a new Favro collection. `name` is required. It is shared only " +
			"with the people and groups in `share_to_users`: members of the organization, " +
			"named by userId (from favro_resolve_user), or groups. To open it to the whole " +
			"organization or to anyone with its link, create it and then call " +
			"favro_widen_collection_sharing. Successful live writes invalidate the collection " +
			"cache. Pass `dry_run: true` to preview the request without contacting Favro.",
		Annotations: mutating("Create Favro collection", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createCollectionInput) (*mcp.CallToolResult, writeOutput[favro.Collection], error) {
		if err := checkPublicSharing(createCollectionToolName, in.PublicSharing); err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		if err := checkGrants(ctx, r, createCollectionToolName, "admin", noneHeld,
			sharedGrants("share_to_users", in.ShareToUsers)); err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Collection, error) {
				return r.Client().CreateCollection(writeCtx, favro.CreateCollectionRequest{
					Name:                     in.Name,
					ShareToUsers:             in.ShareToUsers,
					StarPage:                 in.StarPage,
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
			r.InvalidateCollectionCache()
		}
		return nil, out, nil
	})
}

func registerUpdateCollection(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: updateCollectionToolName,
		Description: "Update a Favro collection. Every body field is optional — pass at " +
			"least one. `archive: true` archives, `archive: false` unarchives, omit to keep " +
			"current. Membership uses two separate lists: `share_to_users` adds members of the " +
			"organization or groups who aren't in the collection yet, while `members` re-roles " +
			"or (with `delete: true`) removes people who already are. Opening the collection to " +
			"the whole organization or to anyone with its link is favro_widen_collection_sharing; " +
			"`public_sharing: 'users'` here narrows it back. Successful live writes invalidate " +
			"the collection cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Update Favro collection", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateCollectionInput) (*mcp.CallToolResult, writeOutput[favro.Collection], error) {
		if err := checkPublicSharing(updateCollectionToolName, in.PublicSharing); err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		// Who already has access is read, not assumed from the call:
		// re-sending a role someone holds widens nothing.
		held := func() (map[string]string, error) {
			col, err := r.Client().GetCollection(ctx, in.CollectionID)
			return heldBy(sharedGrants("", col.SharedToUsers)), err
		}
		grants := append(sharedGrants("share_to_users", in.ShareToUsers), sharedGrants("members", in.Members)...)
		if err := checkGrants(ctx, r, updateCollectionToolName, "admin", held, grants); err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Collection, error) {
				return r.Client().UpdateCollection(writeCtx, in.CollectionID, favro.UpdateCollectionRequest{
					Name:                     in.Name,
					PublicSharing:            in.PublicSharing,
					Background:               in.Background,
					Color:                    in.Color,
					IconName:                 in.IconName,
					FullMembersCanAddWidgets: in.FullMembersCanAddWidgets,
					ShareToUsers:             in.ShareToUsers,
					Members:                  in.Members,
					StarPage:                 in.StarPage,
					Archive:                  in.Archive,
				})
			},
			func() string { return updateCollectionStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		if !out.DryRun {
			r.InvalidateCollectionCache()
		}
		return nil, out, nil
	})
}

func registerDeleteCollection(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: deleteCollectionToolName,
		Description: "Delete a Favro collection by its collectionId. Destructive — MCP hosts " +
			"may warn before auto-confirming. Favro does not cascade-delete widgets when a " +
			"collection is removed; widgets that lived only in this collection may become " +
			"orphaned. On success the collection / widget / search-cards caches are " +
			"invalidated. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro collection", true),
		Meta:        promptEveryCall(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteCollectionInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.Client().DeleteCollection(writeCtx, in.CollectionID)
			},
			func() string {
				return fmt.Sprintf("would delete collection %q from the active organization", in.CollectionID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		if !out.DryRun {
			r.InvalidateCollectionCache()
			r.InvalidateWidgetCache()
			r.InvalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

// errWidenMode is a public_sharing favro_widen_collection_sharing does
// not set.
var errWidenMode = render.Sentinel(render.ClassInvalid,
	"favro_widen_collection_sharing: public_sharing must be 'organization' or 'public'; to narrow a collection "+
		"to the people it is shared with, use favro_update_collection with public_sharing: 'users'")

// errWidenWitness is a collection_name that is not the name Favro holds.
var errWidenWitness = render.Sentinel(render.ClassConflict,
	"favro_widen_collection_sharing: collection_name is not the collection's name in Favro, so the call "+
		"may not open the collection the person approved; nothing was changed. Read it with "+
		"favro_get_collection and pass the name it holds")

func registerWidenCollectionSharing(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: widenCollectionSharingToolName,
		Description: "Open a Favro collection to every member of the organization " +
			"(`public_sharing: 'organization'`) or to anyone on the internet with its link, " +
			"without signing in (`'public'`). Nothing takes back what was seen while it was " +
			"open. Takes `collection_name`, the name Favro holds for the collection, as a " +
			"witness: a name that differs is refused. favro_create_collection and " +
			"favro_update_collection refuse these two modes; favro_update_collection with " +
			"`public_sharing: 'users'` narrows a collection again. The collection is read back " +
			"after the write and `notes` says whether the mode took. Pass `dry_run: true` to preview.",
		Annotations: mutating("Widen Favro collection sharing", true),
		Meta:        promptEveryCall(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in widenCollectionSharingInput) (*mcp.CallToolResult, writeOutput[favro.Collection], error) {
		mode := sharingMode(in.PublicSharing)
		if mode != sharingOrganization && mode != sharingPublic {
			return nil, writeOutput[favro.Collection]{}, errWidenMode
		}
		col, err := r.Client().GetCollection(ctx, in.CollectionID)
		if err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		if strings.TrimSpace(col.Name) != strings.TrimSpace(in.CollectionName) {
			return nil, writeOutput[favro.Collection]{}, errWidenWitness
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Collection, error) {
				return r.Client().UpdateCollection(writeCtx, in.CollectionID, favro.UpdateCollectionRequest{PublicSharing: mode})
			},
			func() string {
				return fmt.Sprintf("would set public_sharing of collection %q to %q", in.CollectionID, mode)
			},
			fmt.Sprintf(noteSharingWas, col.PublicSharing),
		)
		if err != nil {
			return nil, writeOutput[favro.Collection]{}, err
		}
		if !out.DryRun {
			r.InvalidateCollectionCache()
			out.Notes = append(out.Notes, sharingReadBack(ctx, r, in.CollectionID, mode))
		}
		return nil, out, nil
	})
}

// sharingReadBack reads a collection back after a sharing write and
// says whether it holds the mode asked for (hard rule 2).
func sharingReadBack(ctx context.Context, r *service.Resolver, id, mode string) string {
	col, err := r.Client().GetCollection(ctx, id)
	switch {
	case err != nil:
		return fmt.Sprintf(noteReadBackFailed, "collection", err)
	case !strings.EqualFold(col.PublicSharing, mode):
		return fmt.Sprintf(noteSharingIgnored, col.PublicSharing, mode)
	}
	return fmt.Sprintf(noteSharingVerified, col.PublicSharing)
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
		{in.StarPage != nil, bln("star_page", in.StarPage)},
		{len(in.ShareToUsers) > 0, func() string { return fmt.Sprintf("share_to_users (%d invited)", len(in.ShareToUsers)) }},
		{len(in.Members) > 0, func() string { return fmt.Sprintf("members (%d updated)", len(in.Members)) }},
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
