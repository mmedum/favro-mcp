package livecover

import (
	"sort"

	"github.com/mmedum/favro-mcp/v4/internal/config"
)

// Steps is every call the live driver makes, and the list the
// `live-cover` gate measures against the binary's published schema.
//
// It lives here rather than in the driver so the gate and the driver
// cannot disagree: a gate that parsed the driver's source would be a
// parser agreeing with a program, and the two drift the first time
// somebody writes a step in a loop.
//
// **Every mutating step carries dry_run.** Nothing here writes to the
// organization it runs against. The live-cover gate fails on a step
// whose tool is not read-only, by its readOnlyHint, and lacks it; the
// driver also starts the server with --dry-run. The dry-run path is still real
// coverage — the tool resolves names, builds the request and validates
// its arguments before the client's gate turns it into a record rather
// than a request — and it is the only way to exercise a delete against
// a tenant somebody works in.
func Steps() []Step {
	steps := append(SeedSteps(), optionSteps()...)
	steps = append(steps, extraSteps()...)
	sort.SliceStable(steps, func(i, j int) bool { return rank(steps[i]) < rank(steps[j]) })
	return steps
}

// SeedSteps fill the id pool, and they are hand-written because their
// dependencies are Favro's: columns are listed per widget, comments and
// checklists per card, and a card listing needs a widget or a
// collection to scope it.
//
// Two properties matter and neither survives generation. They take the
// minimum arguments, so a seed cannot fail on an option unrelated to
// the id it is there to fetch; and they set no filter, because a
// generated `name` filter can list zero tags and then skip every step
// needing a tag id.
func SeedSteps() []Step {
	return []Step{
		{Name: "ping the server", Tool: "favro_ping", Args: map[string]any{}, Why: "seed: the server answers at all"},
		{Name: "list organizations", Tool: "favro_list_organizations", Args: map[string]any{}, Why: "seed: an organization id"},
		{Name: "list users", Tool: "favro_list_users", Args: map[string]any{}, Why: "seed: a user id"},
		{Name: "list collections", Tool: "favro_list_collections", Args: map[string]any{}, Why: "seed: a collection id"},
		{Name: "list widgets", Tool: "favro_list_widgets", Args: map[string]any{}, Why: "seed: a widget id"},
		{Name: "list tags", Tool: "favro_list_tags", Args: map[string]any{}, Why: "seed: a tag id"},
		{Name: "list custom fields", Tool: "favro_list_custom_fields", Args: map[string]any{}, Why: "seed: a custom field id"},
		{Name: "list groups", Tool: "favro_list_groups", Args: map[string]any{}, Why: "seed: a group id"},
		{
			Name: "list a widget's columns",
			Tool: "favro_list_columns",
			Args: map[string]any{"widget_common_id": AnyWidgetCommonID},
			Why:  "seed: a column id, which only exists per widget",
		},
		{
			Name: "list a widget's cards",
			Tool: "favro_list_cards",
			Args: map[string]any{"widget_common_id": AnyWidgetCommonID},
			Why:  "seed: a card id and a card common id",
		},
		{
			Name: "list a card's comments",
			Tool: "favro_list_comments",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "seed: a comment id, which only exists per card",
		},
		{
			Name: "list a card's task lists",
			Tool: "favro_list_tasklists",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "seed: a checklist id",
		},
		{
			Name: "list a card's tasks",
			Tool: "favro_list_tasks",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "seed: a checklist item id",
		},
	}
}

// optionSteps exercise each tool's options. Generated from the
// published schema and then corrected where a tool refuses more than
// one of a group — favro_get_card_full takes exactly one identity,
// favro_set_card_custom_field exactly one value input — because a step
// that sets all of them tests the refusal and nothing else.
func optionSteps() []Step {
	return []Step{
		{
			Name: "add a comment to a card by id, dry run",
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"card_id": AnyCardID,
				"comment": "livefavro probe",
				"dry_run": true,
			},
		},
		{
			Name: "add a dependency to a card, dry run",
			Tool: "favro_add_dependencies",
			Args: map[string]any{
				"card_id":      AnyCardID,
				"dependencies": []map[string]any{{"cardId": AnyCardCommonID, "isBefore": true}},
				"dry_run":      true,
			},
		},
		{
			Name: "adding an unknown tag name to a card is [not_found]",
			Tool: "favro_add_tag_to_card",
			Args: map[string]any{
				"card_id":  AnyCardID,
				"tag_name": "livefavro-probe-no-such-tag",
				"dry_run":  true,
			},
			ExpectError: "not_found",
			Why:         "refused rather than created: Favro's own addTags turns an unknown name into an organization-wide tag",
		},
		{
			Name: "append to a card's description, dry run",
			Tool: "favro_append_card_description",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
				"text":    "livefavro probe",
			},
		},
		{
			Name: "archive a card, dry run",
			Tool: "favro_archive_card",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
			},
		},
		{
			Name: "create a card, dry run",
			Tool: "favro_create_card",
			Args: map[string]any{
				"column_id":            AnyColumnID,
				"detailed_description": "livefavro probe",
				"dry_run":              true,
				"due_date":             "2026-02-01",
				"name":                 "livefavro probe",
				"start_date":           "2026-01-01",
				"widget_common_id":     AnyWidgetCommonID,
			},
		},
		{
			Name: "create a collection, dry run",
			Tool: "favro_create_collection",
			Args: map[string]any{
				"background":                   "blue",
				"color":                        "blue",
				"dry_run":                      true,
				"full_members_can_add_widgets": false,
				"icon_name":                    "rocket",
				"name":                         "livefavro probe",
				"public_sharing":               "users",
				"star_page":                    false,
			},
		},
		{
			Name: "create a column, dry run",
			Tool: "favro_create_column",
			Args: map[string]any{
				"color":            "blue",
				"dry_run":          true,
				"name":             "livefavro probe",
				"position":         1,
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "create a comment, dry run",
			Tool: "favro_create_comment",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"comment":        "livefavro probe",
				"dry_run":        true,
			},
		},
		{
			Name: "create a group, dry run",
			Tool: "favro_create_group",
			Args: map[string]any{
				"dry_run": true,
				"name":    "livefavro probe",
			},
		},
		{
			Name: "create a tag, dry run",
			Tool: "favro_create_tag",
			Args: map[string]any{
				"color":   "blue",
				"dry_run": true,
				"name":    "livefavro probe",
			},
		},
		{
			Name: "create a task, dry run",
			Tool: "favro_create_task",
			Args: map[string]any{
				"completed":    false,
				"dry_run":      true,
				"name":         "livefavro probe",
				"position":     1,
				"task_list_id": AnyTaskListID,
			},
		},
		{
			Name: "create a task list, dry run",
			Tool: "favro_create_tasklist",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"dry_run":        true,
				"name":           "livefavro probe",
				"position":       1,
			},
		},
		{
			Name: "create a widget, dry run",
			Tool: "favro_create_widget",
			Args: map[string]any{
				"breakdown_card_common_id": "livefavro probe",
				"collection_id":            AnyCollectionID,
				"color":                    "blue",
				"dry_run":                  true,
				"edit_role":                "livefavro probe",
				"name":                     "livefavro probe",
				"owner_role":               "livefavro probe",
				"type":                     "livefavro probe",
			},
		},
		{
			Name: "delete all of a card's dependencies, dry run",
			Tool: "favro_delete_all_dependencies",
			Args: map[string]any{
				"confirm": true,
				"card_id": AnyCardID,
				"dry_run": true,
			},
		},
		{
			Name: "delete a card, dry run",
			Tool: "favro_delete_card",
			Args: map[string]any{
				"confirm":    true,
				"card_id":    AnyCardID,
				"dry_run":    true,
				"everywhere": false,
			},
		},
		{
			Name: "delete a collection, dry run",
			Tool: "favro_delete_collection",
			Args: map[string]any{
				"confirm":       true,
				"collection_id": AnyCollectionID,
				"dry_run":       true,
			},
		},
		{
			Name: "delete a column, dry run",
			Tool: "favro_delete_column",
			Args: map[string]any{
				"confirm":          true,
				"column_id":        AnyColumnID,
				"dry_run":          true,
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "delete a comment, dry run",
			Tool: "favro_delete_comment",
			Args: map[string]any{
				"confirm":    true,
				"comment_id": AnyCommentID,
				"dry_run":    true,
			},
		},
		{
			Name: "delete a dependency, dry run",
			Tool: "favro_delete_dependency",
			Args: map[string]any{
				"confirm":            true,
				"card_id":            AnyCardID,
				"dependency_card_id": "livefavro probe",
				"dry_run":            true,
			},
		},
		{
			Name: "delete a group, dry run",
			Tool: "favro_delete_group",
			Args: map[string]any{
				"confirm":  true,
				"dry_run":  true,
				"group_id": AnyGroupID,
			},
		},
		{
			Name: "delete a tag, dry run",
			Tool: "favro_delete_tag",
			Args: map[string]any{
				"confirm": true,
				"dry_run": true,
				"tag_id":  AnyTagID,
			},
		},
		{
			Name: "delete a task, dry run",
			Tool: "favro_delete_task",
			Args: map[string]any{
				"confirm": true,
				"dry_run": true,
				"task_id": AnyTaskID,
			},
		},
		{
			Name: "delete a task list, dry run",
			Tool: "favro_delete_tasklist",
			Args: map[string]any{
				"confirm":      true,
				"dry_run":      true,
				"task_list_id": AnyTaskListID,
			},
		},
		{
			Name: "delete a webhook, dry run",
			Tool: "favro_delete_webhook",
			Args: map[string]any{
				"confirm":    true,
				"dry_run":    true,
				"webhook_id": "wh-probe",
			},
		},
		{
			Name: "delete a widget from a collection, dry run",
			Tool: "favro_delete_widget",
			Args: map[string]any{
				"confirm":          true,
				"collection_id":    AnyCollectionID,
				"dry_run":          true,
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "read a card as markdown",
			Tool: "favro_get_card",
			Args: map[string]any{
				"card_id":            AnyCardID,
				"description_format": "markdown",
			},
		},
		{
			Name: "read a card in full by id",
			Tool: "favro_get_card_full",
			Args: map[string]any{
				"card_id":            AnyCardID,
				"comment_limit":      1,
				"description_format": "markdown",
				"include_comments":   false,
			},
		},
		{
			Name: "read a collection",
			Tool: "favro_get_collection",
			Args: map[string]any{
				"collection_id": AnyCollectionID,
			},
		},
		{
			Name: "read a column",
			Tool: "favro_get_column",
			Args: map[string]any{
				"column_id": AnyColumnID,
			},
		},
		{
			Name: "read a comment",
			Tool: "favro_get_comment",
			Args: map[string]any{
				"comment_id": AnyCommentID,
			},
		},
		{
			Name: "read a custom field",
			Tool: "favro_get_custom_field",
			Args: map[string]any{
				"custom_field_id": AnyCustomFieldID,
			},
		},
		{
			Name: "read a group",
			Tool: "favro_get_group",
			Args: map[string]any{
				"group_id": AnyGroupID,
			},
		},
		{
			Name: "read the organization",
			Tool: "favro_get_organization",
			Args: map[string]any{},
		},
		{
			Name: "read a tag",
			Tool: "favro_get_tag",
			Args: map[string]any{
				"tag_id": AnyTagID,
			},
		},
		{
			Name: "read a task",
			Tool: "favro_get_task",
			Args: map[string]any{
				"task_id": AnyTaskID,
			},
		},
		{
			Name: "read a task list",
			Tool: "favro_get_tasklist",
			Args: map[string]any{
				"task_list_id": AnyTaskListID,
			},
		},
		{
			Name: "read a user",
			Tool: "favro_get_user",
			Args: map[string]any{
				"user_id": AnyUserID,
			},
		},
		{
			Name: "read a widget",
			Tool: "favro_get_widget",
			Args: map[string]any{
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "list a card's activities since a date",
			Tool: "favro_list_card_activities",
			Args: map[string]any{
				"card_id": AnyCardID,
				"page":    1,
				"since":   "2020-01-01T00:00:00Z",
			},
			Why: "the since filter is in a form Favro accepts",
		},
		{
			Name: "list cards by widget and collection, page 1 with a request id",
			Tool: "favro_list_cards",
			Args: map[string]any{
				"archived":           false,
				"collection_id":      AnyCollectionID,
				"description_format": "livefavro probe",
				"page":               1,
				"request_id":         "livefavro probe",
				"unique":             true,
				"widget_common_id":   AnyWidgetCommonID,
			},
		},
		{
			Name: "list collections, page 1 with a request id",
			Tool: "favro_list_collections",
			Args: map[string]any{
				"archived":   false,
				"page":       1,
				"request_id": "livefavro probe",
			},
		},
		{
			Name: "list a widget's columns, page 1 with a request id",
			Tool: "favro_list_columns",
			Args: map[string]any{
				"page":             1,
				"request_id":       "livefavro probe",
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "list a card's comments, page 1 with a request id",
			Tool: "favro_list_comments",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"page":           1,
				"request_id":     "livefavro probe",
			},
		},
		{
			Name: "list custom fields, page 1 with a request id",
			Tool: "favro_list_custom_fields",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
		},
		{
			Name: "list a card's dependencies",
			Tool: "favro_list_dependencies",
			Args: map[string]any{
				"card_id": AnyCardID,
			},
		},
		{
			Name: "list groups, page 1 with a request id",
			Tool: "favro_list_groups",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
		},
		{
			Name: "list organizations, page 1 with a request id",
			Tool: "favro_list_organizations",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
		},
		{
			Name: "list tags, page 1 with a request id",
			Tool: "favro_list_tags",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
		},
		{
			Name: "list a card's task lists, page 1 with a request id",
			Tool: "favro_list_tasklists",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"page":           1,
				"request_id":     "livefavro probe",
			},
		},
		{
			Name: "list a task list's tasks, page 1 with a request id",
			Tool: "favro_list_tasks",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"page":           1,
				"request_id":     "livefavro probe",
				"task_list_id":   AnyTaskListID,
			},
		},
		{
			Name: "list users, page 1 with a request id",
			Tool: "favro_list_users",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
		},
		{
			Name: "list a widget's webhooks",
			Tool: "favro_list_webhooks",
			Args: map[string]any{
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "list a collection's widgets, page 1 with a request id",
			Tool: "favro_list_widgets",
			Args: map[string]any{
				"archived":      false,
				"collection_id": AnyCollectionID,
				"page":          1,
				"request_id":    "livefavro probe",
			},
		},
		{
			Name: "move a card to another column, dry run",
			Tool: "favro_move_card",
			Args: map[string]any{
				"card_id":          AnyCardID,
				"widget_common_id": AnyWidgetCommonID,
				"column_id":        AnyColumnID,
				"dry_run":          true,
			},
			Why: "Favro answers 200 to a column move and ignores it unless the widget rides along",
		},
		{
			Name: "ping the server after the seeds",
			Tool: "favro_ping",
			Args: map[string]any{},
		},
		{
			Name: "prepend to a card's description, dry run",
			Tool: "favro_prepend_card_description",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
				"text":    "livefavro probe",
			},
		},
		{
			Name: "read the rate limit status",
			Tool: "favro_rate_limit_status",
			Args: map[string]any{},
		},
		{
			Name: "remove an attachment from a card by URL, dry run",
			Tool: "favro_remove_attachment",
			Args: map[string]any{
				"confirm":   true,
				"card_id":   AnyCardID,
				"dry_run":   true,
				"file_urls": []string{"https://example.invalid/not-attached.txt"},
			},
		},
		{
			Name: "removing an unknown tag name from a card is [not_found]",
			Tool: "favro_remove_tag_from_card",
			Args: map[string]any{
				"card_id":  AnyCardID,
				"tag_name": "livefavro-probe-no-such-tag",
				"dry_run":  true,
			},
			ExpectError: "not_found",
		},
		{
			Name: "replace a card's dependencies, dry run",
			Tool: "favro_replace_dependencies",
			Args: map[string]any{
				"confirm":      true,
				"card_id":      AnyCardID,
				"dependencies": []map[string]any{{"cardId": AnyCardCommonID, "isBefore": false}},
				"dry_run":      true,
			},
		},
		{
			Name: "a description find that matches nothing is [not_found]",
			Tool: "favro_replace_in_card_description",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"find":      "a string no card description contains \u2014 livefavro probe",
				"replace":   "b",
				"count":     1,
				"use_regex": false,
				"dry_run":   true,
			},
			ExpectError: "not_found",
			Why:         "the tool refuses rather than PUT the description back unchanged",
		},
		{
			Name: "resolve a collection by name",
			Tool: "favro_resolve_collection",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
		},
		{
			Name: "resolve a column by name",
			Tool: "favro_resolve_column",
			Args: map[string]any{
				"force_refresh":    true,
				"limit":            5,
				"name":             "livefavro probe",
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "resolve a custom field by name",
			Tool: "favro_resolve_custom_field",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
		},
		{
			Name: "resolve a group by name",
			Tool: "favro_resolve_group",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
		},
		{
			Name: "resolve a tag by name",
			Tool: "favro_resolve_tag",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
		},
		{
			Name: "resolve a user by name",
			Tool: "favro_resolve_user",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
		},
		{
			Name: "resolve a widget by name in a collection",
			Tool: "favro_resolve_widget",
			Args: map[string]any{
				"collection_id": AnyCollectionID,
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
		},
		{
			Name: "search a widget's cards",
			Tool: "favro_search_cards",
			Args: map[string]any{
				"force_refresh":    true,
				"include_archived": false,
				"limit":            5,
				"min_score":        1,
				"query":            "a",
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "a custom field write with no value is [invalid]",
			Tool: "favro_set_card_custom_field",
			Args: map[string]any{
				"card_id":         AnyCardID,
				"custom_field_id": AnyCustomFieldID,
				"skip_verify":     true,
				"dry_run":         true,
			},
			ExpectError: "invalid",
			Why:         "the driver cannot know the field's type, and the refusal names every legal input for it",
		},
		{
			Name: "unarchive a card, dry run",
			Tool: "favro_unarchive_card",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
			},
		},
		{
			Name: "update a card's drag mode, dry run",
			Tool: "favro_update_card",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"drag_mode": "livefavro probe",
				"dry_run":   true,
			},
		},
		{
			Name: "update a collection, dry run",
			Tool: "favro_update_collection",
			Args: map[string]any{
				"archive":                      false,
				"background":                   "blue",
				"collection_id":                AnyCollectionID,
				"color":                        "blue",
				"dry_run":                      true,
				"full_members_can_add_widgets": false,
				"icon_name":                    "rocket",
				"name":                         "livefavro probe",
				"public_sharing":               "users",
				"star_page":                    false,
			},
		},
		{
			Name: "update a column, dry run",
			Tool: "favro_update_column",
			Args: map[string]any{
				"color":            "blue",
				"column_id":        AnyColumnID,
				"dry_run":          true,
				"name":             "livefavro probe",
				"position":         1,
				"widget_common_id": AnyWidgetCommonID,
			},
		},
		{
			Name: "update a comment, dry run",
			Tool: "favro_update_comment",
			Args: map[string]any{
				"comment":    "livefavro probe",
				"comment_id": AnyCommentID,
				"dry_run":    true,
			},
		},
		{
			Name: "update a dependency, dry run",
			Tool: "favro_update_dependency",
			Args: map[string]any{
				"card_id":            AnyCardID,
				"dependency_card_id": "livefavro probe",
				"dry_run":            true,
				"is_before":          false,
			},
		},
		{
			Name: "rename a group, dry run",
			Tool: "favro_update_group",
			Args: map[string]any{
				"dry_run":  true,
				"group_id": AnyGroupID,
				"name":     "livefavro probe",
			},
		},
		{
			Name: "update a tag, dry run",
			Tool: "favro_update_tag",
			Args: map[string]any{
				"color":   "blue",
				"dry_run": true,
				"name":    "livefavro probe",
				"tag_id":  AnyTagID,
			},
		},
		{
			Name: "update tags in bulk, dry run",
			Tool: "favro_update_tags",
			Args: map[string]any{
				"updates": []map[string]any{{"tag_id": AnyTagID, "name": "livefavro probe"}},
				"dry_run": true,
			},
			Why: "the bulk path is a client-side fan-out rather than a Favro endpoint",
		},
		{
			Name: "update a task, dry run",
			Tool: "favro_update_task",
			Args: map[string]any{
				"completed": false,
				"dry_run":   true,
				"name":      "livefavro probe",
				"position":  1,
				"task_id":   AnyTaskID,
			},
		},
		{
			Name: "update a task list, dry run",
			Tool: "favro_update_tasklist",
			Args: map[string]any{
				"dry_run":      true,
				"name":         "livefavro probe",
				"position":     1,
				"task_list_id": AnyTaskListID,
			},
		},
		{
			Name: "update a widget, dry run",
			Tool: "favro_update_widget",
			Args: map[string]any{
				"archive":                  false,
				"breakdown_card_common_id": "livefavro probe",
				"collection_id":            AnyCollectionID,
				"color":                    "blue",
				"dry_run":                  true,
				"edit_role":                "livefavro probe",
				"name":                     "livefavro probe",
				"owner_role":               "livefavro probe",
				"type":                     "livefavro probe",
				"widget_common_id":         AnyWidgetCommonID,
			},
		},
		{
			Name: "uploading a folder is [invalid]",
			Tool: "favro_upload_attachment",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"file_path": UploadFolder,
				"dry_run":   true,
			},
			ExpectError: "invalid",
			Why:         "the not-a-regular-file guard refuses it before anything is read into memory",
		},
		{
			Name: "uploading a path outside the upload directory is [invalid]",
			Tool: "favro_upload_attachment",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"file_path": "/dev/null",
				"dry_run":   true,
			},
			ExpectError: "invalid",
			Why:         "a path outside " + config.EnvUploadDir + " is refused before it is opened",
		},
		{
			Name: "a comment upload with .. out of the upload directory is [invalid]",
			Tool: "favro_upload_comment_attachment",
			Args: map[string]any{
				"comment_id": AnyCommentID,
				"file_path":  "../" + UploadFile,
				"dry_run":    true,
			},
			ExpectError: "invalid",
		},
	}
}

// rank orders the run: seeds first, in the order they are written, then
// the remaining reads, then everything that mutates — which by then has
// real ids to work with, and carries dry_run regardless.
func rank(s Step) int {
	for i, seed := range SeedSteps() {
		if seed.Name == s.Name {
			return i
		}
	}
	if readOnly(s.Tool) {
		return 100
	}
	return 200
}

// readOnly reports whether a tool only reads, by the naming convention
// every tool in this server follows.
func readOnly(tool string) bool {
	for _, prefix := range []string{"favro_list_", "favro_get_", "favro_resolve_", "favro_search_"} {
		if len(tool) > len(prefix) && tool[:len(prefix)] == prefix {
			return true
		}
	}
	return tool == "favro_ping" || tool == "favro_rate_limit_status"
}
