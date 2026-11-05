package livecover

import "github.com/mmedum/favro-mcp/v4/internal/config"

// extraSteps exercise what one call per tool leaves untouched.
//
// A tool called once with its required arguments covers a fraction of
// its surface: `favro_update_card` takes seventeen options and needs
// one. These are the rest — the alternative identities a tool accepts,
// and the optional fields a write can carry — and they are written by
// hand because each is a decision about what a meaningful call looks
// like rather than something a schema implies.
//
// Every mutating one carries dry_run, so the request is built and
// validated and never sent.
func extraSteps() []Step {
	return []Step{
		// The three other ways to name a card, each a different code
		// path: a cross-widget id, the integer people paste from a
		// reference, and a search over a scoped corpus.
		{
			Name: "read a card in full by common id",
			Tool: "favro_get_card_full",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "the cross-widget identity, which Favro's own GET /cards/{id} answers 403 for",
		},
		{
			Name: "read a card in full by sequential id",
			Tool: "favro_get_card_full",
			Args: map[string]any{"sequential_id": AnySequentialID},
			Why:  "the integer inside a human card reference",
		},
		{
			Name: "add a comment to a card by common id, dry run",
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"comment":        "livefavro probe",
				"dry_run":        true,
			},
		},
		{
			Name: "add a comment to a card by sequential id, dry run",
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"sequential_id": AnySequentialID,
				"comment":       "livefavro probe",
				"dry_run":       true,
			},
		},
		{
			Name: "add a comment to a card found by search in a widget, dry run",
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"search_query":     "a",
				"widget_common_id": AnyWidgetCommonID,
				"comment":          "livefavro probe",
				"dry_run":          true,
			},
		},
		{
			Name: "add a comment to a card found by search in a collection, dry run",
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"search_query":  "a",
				"collection_id": AnyCollectionID,
				"comment":       "livefavro probe",
				"dry_run":       true,
			},
		},

		// The card listing's other filters.
		{
			Name: "list cards by common id",
			Tool: "favro_list_cards",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "the cross-widget filter, which is the only way to fetch a card by common id",
		},
		{
			Name: "list cards by sequential id",
			Tool: "favro_list_cards",
			Args: map[string]any{"sequential_id": AnySequentialID},
		},
		{
			Name: "list the cards in one column of a widget",
			Tool: "favro_list_cards",
			Args: map[string]any{
				"widget_common_id": AnyWidgetCommonID,
				"column_id":        AnyColumnID,
			},
		},
		{
			Name: "list the signed-in account's todo cards",
			Tool: "favro_list_cards",
			Args: map[string]any{"todo_list": true},
			Why:  "the todo list is scoped by the token rather than by a widget",
		},

		// The archived flag on the three listings that take it. The seeds
		// list with the default, which sends archived=false; the flag sends
		// no archived parameter, which Favro answers with both kinds.
		{
			Name: "list a widget's cards, archived ones included",
			Tool: "favro_list_cards",
			Args: map[string]any{"widget_common_id": AnyWidgetCommonID, "archived": true},
			Why:  "archived=true would return archived cards only",
		},
		{
			Name: "list collections, archived ones included",
			Tool: "favro_list_collections",
			Args: map[string]any{"archived": true},
			Why:  "archived=true would return archived collections only",
		},
		{
			Name: "list widgets, archived ones included",
			Tool: "favro_list_widgets",
			Args: map[string]any{"archived": true},
			Why:  "archived=true would return archived widgets only",
		},
		{
			Name: "list tags by a name no tag has",
			Tool: "favro_list_tags",
			Args: map[string]any{"name": "livefavro-probe-no-such-tag"},
			Why:  "the name filter answers an empty page rather than an error",
		},
		{
			Name: "create a collection shared with the signed-in account, dry run",
			Tool: "favro_create_collection",
			Args: map[string]any{
				"name":           "livefavro probe",
				"share_to_users": []any{map[string]any{"userId": OwnUserID, "role": "guest"}},
				"dry_run":        true,
			},
			Why: "the write-side sharing key, which Favro spells differently from the read",
		},
		{
			Name: "create a group with the signed-in account as a member, dry run",
			Tool: "favro_create_group",
			Args: map[string]any{
				"name":    "livefavro probe",
				"members": []any{map[string]any{"userId": OwnUserID, "role": "member"}},
				"dry_run": true,
			},
			Why: "the membership goes in the same call, after the user is read as a member of the organization",
		},
		{
			Name: "create a task list with its tasks, dry run",
			Tool: "favro_create_tasklist",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"name":           "livefavro probe",
				"tasks":          []any{map[string]any{"name": "first"}, map[string]any{"name": "second", "completed": false}},
				"dry_run":        true,
			},
		},
		{
			Name: "search a collection's cards",
			Tool: "favro_search_cards",
			Args: map[string]any{"query": "a", "collection_id": AnyCollectionID, "limit": 5},
		},
		{
			Name: "list a card's activities until a date",
			Tool: "favro_list_card_activities",
			Args: map[string]any{"card_id": AnyCardID, "until": "2030-01-01T00:00:00Z"},
		},

		// The optional fields a write can carry. Dry-run builds the
		// body, so this is the shape of the request being checked.
		{
			Name: "create a card with a lane, a parent, tags and assignees, dry run",
			Tool: "favro_create_card",
			Args: map[string]any{
				"name":             "livefavro probe",
				"widget_common_id": AnyWidgetCommonID,
				"column_id":        AnyColumnID,
				"lane_id":          "livefavro-probe-lane",
				"parent_card_id":   AnyCardCommonID,
				"assignment_ids":   []any{AnyUserID},
				"tag_ids":          []any{AnyTagID},
				"list_position":    1,
				"sheet_position":   1,
				"dry_run":          true,
			},
			Why: "every optional field a new card can carry",
		},
		{
			Name: "update every field of a card at once, dry run",
			Tool: "favro_update_card",
			Args: map[string]any{
				"card_id":               AnyCardID,
				"name":                  "livefavro probe",
				"detailed_description":  "livefavro probe",
				"widget_common_id":      AnyWidgetCommonID,
				"column_id":             AnyColumnID,
				"lane_id":               "livefavro-probe-lane",
				"parent_card_id":        AnyCardCommonID,
				"add_tag_ids":           []any{AnyTagID},
				"remove_tag_ids":        []any{AnyTagID},
				"add_assignment_ids":    []any{AnyUserID},
				"remove_assignment_ids": []any{AnyUserID},
				"start_date":            "2026-01-01",
				"due_date":              "2026-02-01",
				"list_position":         1,
				"sheet_position":        1,
				"dry_run":               true,
			},
		},
		{
			Name: "detach a card from its parent, dry run",
			Tool: "favro_update_card",
			Args: map[string]any{
				"card_id":          AnyCardID,
				"widget_common_id": AnyWidgetCommonID,
				"clear_parent":     true,
				"dry_run":          true,
			},
			Why: "the one structural write that does not read the card's current parent first",
		},
		{
			Name: "update a card structurally without naming its parent, dry run",
			Tool: "favro_update_card",
			Args: map[string]any{
				"card_id":          AnyCardID,
				"widget_common_id": AnyWidgetCommonID,
				"name":             "livefavro-probe-structural-rename",
				"dry_run":          true,
			},
			Why: "the path that reads the card's current parent and carries it through, which every other update step short-circuits past",
		},
		{
			Name: "move a card naming its own parent, dry run",
			Tool: "favro_move_card",
			Args: map[string]any{
				"card_id":          AnyCardID,
				"widget_common_id": AnyWidgetCommonID,
				"column_id":        AnyColumnID,
				"parent_card_id":   AnyCardID,
				"dry_run":          true,
			},
			Why: "a named parent skips the read of the current one",
		},
		{
			Name: "move a card and detach it from its parent, dry run",
			Tool: "favro_move_card",
			Args: map[string]any{
				"card_id":          AnyCardID,
				"widget_common_id": AnyWidgetCommonID,
				"column_id":        AnyColumnID,
				"clear_parent":     true,
				"dry_run":          true,
			},
			Why: "the one move path that does not read the card's current parent first",
		},
		{
			Name: "update a card's placement with the read-back off, dry run",
			Tool: "favro_update_card",
			Args: map[string]any{
				"card_id":          AnyCardID,
				"widget_common_id": AnyWidgetCommonID,
				"column_id":        AnyColumnID,
				"clear_parent":     true,
				"skip_verify":      true,
				"dry_run":          true,
			},
		},
		{
			Name: "move a card across widgets with every positioning option, dry run",
			Tool: "favro_move_card",
			Args: map[string]any{
				"card_id":          AnyCardID,
				"widget_common_id": AnyWidgetCommonID,
				"column_id":        AnyColumnID,
				"lane_id":          "livefavro-probe-lane",
				"list_position":    1,
				"sheet_position":   1,
				"skip_verify":      true,
				"drag_mode":        "commit",
				"dry_run":          true,
			},
		},
		{
			Name: "update a collection's sharing under both keys, dry run",
			Tool: "favro_update_collection",
			Args: map[string]any{
				"collection_id":  AnyCollectionID,
				"share_to_users": []any{map[string]any{"userId": OwnUserID, "role": "guest"}},
				"members":        []any{map[string]any{"userId": OwnUserID, "role": "guest"}},
				"dry_run":        true,
			},
			Why: "Favro reads one sharing key and writes another",
		},
		{
			Name: "replace a group's members, dry run",
			Tool: "favro_update_group",
			Args: map[string]any{
				"group_id": AnyGroupID,
				"members":  []any{map[string]any{"userId": OwnUserID, "role": "member"}},
				"dry_run":  true,
			},
			Why: "whole-list membership, which Favro's reference describes as a delta, read against who the group holds",
		},
		{
			Name: "remove an attachment from a comment by URL, dry run",
			Tool: "favro_update_comment",
			Args: map[string]any{
				"comment_id":         AnyCommentID,
				"comment":            "livefavro probe",
				"remove_attachments": []any{"https://example.invalid/not-attached.txt"},
				"dry_run":            true,
			},
		},
		{
			Name: "upload a file to a card with a name and type, dry run",
			Tool: "favro_upload_attachment",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"file_path": UploadFile,
				"filename":  "livefavro-probe.txt",
				"mime_type": "text/plain",
				"dry_run":   true,
			},
			Why: "a file inside " + config.EnvUploadDir + " is read",
		},
		{
			Name: "upload a file to a comment with a name and type, dry run",
			Tool: "favro_upload_comment_attachment",
			Args: map[string]any{
				"comment_id": AnyCommentID,
				"file_path":  UploadFile,
				"filename":   "livefavro-probe.txt",
				"mime_type":  "text/plain",
				"dry_run":    true,
			},
		},

		// What the ordinary writes refuse, so the refusal is seen
		// against a real organization. Each carries dry_run as well:
		// the refusal comes first either way, and if it ever did not,
		// nothing is written.
		{
			Name:        "creating a public collection is [blocked]",
			Tool:        "favro_create_collection",
			Args:        map[string]any{"name": "livefavro probe", "public_sharing": "public", "dry_run": true},
			Why:         "a public collection is its own tool's, never this one's",
			ExpectError: "blocked",
		},
		{
			Name:        "opening a collection to the organization on an update is [blocked]",
			Tool:        "favro_update_collection",
			Args:        map[string]any{"collection_id": AnyCollectionID, "public_sharing": "organization", "dry_run": true},
			ExpectError: "blocked",
		},
		{
			Name: "sharing a new collection by email is [blocked]",
			Tool: "favro_create_collection",
			Args: map[string]any{
				"name":           "livefavro probe",
				"share_to_users": []any{map[string]any{"email": "livefavro-probe@example.invalid", "role": "view"}},
				"dry_run":        true,
			},
			Why:         "an email may reach outside the organization",
			ExpectError: "blocked",
		},
		{
			Name: "sharing a new collection with a user outside the organization is [blocked]",
			Tool: "favro_create_collection",
			Args: map[string]any{
				"name":           "livefavro probe",
				"share_to_users": []any{map[string]any{"userId": OutsideUserID, "role": "guest"}},
				"dry_run":        true,
			},
			Why:         "refused after reading who the organization holds",
			ExpectError: "blocked",
		},
		{
			Name: "a new group with a member outside the organization is [blocked]",
			Tool: "favro_create_group",
			Args: map[string]any{
				"name":    "livefavro probe",
				"members": []any{map[string]any{"userId": OutsideUserID, "role": "member"}},
				"dry_run": true,
			},
			ExpectError: "blocked",
		},
		{
			Name: "sharing a collection with a user outside the organization is [blocked]",
			Tool: "favro_update_collection",
			Args: map[string]any{
				"collection_id":  AnyCollectionID,
				"share_to_users": []any{map[string]any{"userId": OutsideUserID, "role": "guest"}},
				"dry_run":        true,
			},
			Why:         "refused after reading who the collection is shared with",
			ExpectError: "blocked",
		},
		{
			Name: "adding a user outside the organization to a group is [blocked]",
			Tool: "favro_update_group",
			Args: map[string]any{
				"group_id": AnyGroupID,
				"members":  []any{map[string]any{"userId": OutsideUserID, "role": "member"}},
				"dry_run":  true,
			},
			Why:         "refused after reading who the group and the organization hold",
			ExpectError: "blocked",
		},
		{
			Name:        "a new widget owned by guests is [blocked]",
			Tool:        "favro_create_widget",
			Args:        map[string]any{"collection_id": AnyCollectionID, "name": "livefavro probe", "owner_role": "guests", "dry_run": true},
			ExpectError: "blocked",
		},
		{
			Name:        "making guests a widget's editors is [blocked]",
			Tool:        "favro_update_widget",
			Args:        map[string]any{"widget_common_id": AnyWidgetCommonID, "edit_role": "guests", "dry_run": true},
			ExpectError: "blocked",
		},
		{
			Name: "widening a collection's sharing with the wrong name is [conflict]",
			Tool: "favro_widen_collection_sharing",
			Args: map[string]any{
				"collection_id":   AnyCollectionID,
				"collection_name": "livefavro probe: not this collection's name",
				"public_sharing":  "organization",
				"confirm":         true,
				"dry_run":         true,
			},
			Why:         "the collection is read and its name held against the witness; -writes opens a probe collection",
			ExpectError: "conflict",
		},
		{
			Name: "delete a widget from every collection, dry run",
			Tool: "favro_delete_widget",
			Args: map[string]any{
				"widget_common_id": AnyWidgetCommonID,
				"everywhere":       true,
				"confirm":          true,
				"dry_run":          true,
			},
			Why: "the delete from every collection is named rather than reached by omission",
		},
	}
}
