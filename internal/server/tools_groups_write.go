package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	createGroupToolName = "favro_create_group"
	updateGroupToolName = "favro_update_group"
	deleteGroupToolName = "favro_delete_group"
)

// createGroupInput is the input for favro_create_group.
type createGroupInput struct {
	dryRunInput
	Name    string              `json:"name" jsonschema:"the group name (required)"`
	Members []favro.GroupMember `json:"members,omitempty" jsonschema:"optional initial member list. Each entry identifies a person by userId or email plus a role ('administrator' / 'member'). Resolve userIds via favro_resolve_user."`
}

// updateGroupInput is the input for favro_update_group. Members,
// when set, REPLACES the group's member list — Favro's update has
// no add/remove semantics; the LLM must compose the full intended
// list from the cached current members.
type updateGroupInput struct {
	dryRunInput
	GroupID string              `json:"group_id" jsonschema:"the Favro groupId to update. Resolve via favro_resolve_group."`
	Name    string              `json:"name,omitempty" jsonschema:"new group name; omit to keep current"`
	Members []favro.GroupMember `json:"members,omitempty" jsonschema:"the group's full intended member list — it REPLACES the existing one, so include everyone who should remain. Each entry identifies a person by userId or email plus a role ('administrator' / 'member'). A per-entry delete: true is accepted (Favro documents it) but whole-list replacement is what was observed live."`
}

// deleteGroupInput is the input for favro_delete_group.
type deleteGroupInput struct {
	dryRunInput
	GroupID string `json:"group_id" jsonschema:"the Favro groupId to delete"`
}

func registerCreateGroup(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: createGroupToolName,
		Description: "Create a new org-global Favro group. `name` is required; `members` is " +
			"optional (Favro creates an empty group when omitted). Successful live writes " +
			"invalidate the group cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Create Favro group", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createGroupInput) (*mcp.CallToolResult, writeOutput[favro.Group], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Group, error) {
				return r.client.CreateGroup(writeCtx, favro.CreateGroupRequest{Name: in.Name, Members: in.Members})
			},
			func() string {
				return fmt.Sprintf("would create a new group named %q with %d initial member(s)", in.Name, len(in.Members))
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Group]{}, err
		}
		if !out.DryRun {
			r.invalidateGroupCache()
		}
		return nil, out, nil
	})
}

func registerUpdateGroup(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: updateGroupToolName,
		Description: "Update a Favro group. Both fields optional. `members`, when set, " +
			"REPLACES the group's member list — compose the full intended list before " +
			"sending, and drop anyone who should leave. (Favro's docs describe add/remove " +
			"delta semantics with a per-entry `delete` flag, but a live test observed whole" +
			"-list replacement; sending the full list is correct under either reading, " +
			"whereas sending only a delta is not.) Each entry identifies a person by " +
			"userId or email plus a role. Successful live writes invalidate the group " +
			"cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Update Favro group", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateGroupInput) (*mcp.CallToolResult, writeOutput[favro.Group], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Group, error) {
				return r.client.UpdateGroup(writeCtx, in.GroupID, favro.UpdateGroupRequest{Name: in.Name, Members: in.Members})
			},
			func() string { return updateGroupStateDiff(&in) },
		)
		if err != nil {
			return nil, writeOutput[favro.Group]{}, err
		}
		if !out.DryRun {
			r.invalidateGroupCache()
		}
		return nil, out, nil
	})
}

func registerDeleteGroup(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: deleteGroupToolName,
		Description: "Delete a Favro group by its groupId. Destructive — MCP hosts may warn " +
			"before auto-confirming. The group is removed from any sharing / assignment / " +
			"custom-field references it appeared in. Successful live writes invalidate the " +
			"group cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Delete Favro group", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in deleteGroupInput) (*mcp.CallToolResult, writeOutput[struct{}], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (struct{}, error) {
				return struct{}{}, r.client.DeleteGroup(writeCtx, in.GroupID)
			},
			func() string {
				return fmt.Sprintf("would delete group %q from the active organization", in.GroupID)
			},
		)
		if err != nil {
			return nil, writeOutput[struct{}]{}, err
		}
		if !out.DryRun {
			r.invalidateGroupCache()
		}
		return nil, out, nil
	})
}

// updateGroupStateDiff renders the per-tool dry-run state-diff phrase.
func updateGroupStateDiff(in *updateGroupInput) string {
	var changes []string
	if in.Name != "" {
		changes = append(changes, fmt.Sprintf("name → %q", in.Name))
	}
	if in.Members != nil {
		var removals int
		for _, m := range in.Members {
			if m.Delete != nil && *m.Delete {
				removals++
			}
		}
		if removals > 0 {
			changes = append(changes, fmt.Sprintf("members → %d entries (replaces existing), %d flagged for delete", len(in.Members), removals))
		} else {
			changes = append(changes, fmt.Sprintf("members → %d entries (replaces existing)", len(in.Members)))
		}
	}
	if len(changes) == 0 {
		return fmt.Sprintf("would PUT group %q with no changed fields (no-op)", in.GroupID)
	}
	return fmt.Sprintf("would update group %q: %s", in.GroupID, strings.Join(changes, ", "))
}
