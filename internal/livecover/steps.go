package livecover

import "sort"

// Steps is every call the live driver makes, and the list the
// `live-cover` gate measures against the binary's published schema.
//
// It lives here rather than in the driver so the gate and the driver
// cannot disagree: a gate that parsed the driver's source would be a
// parser agreeing with a program, and the two drift the first time
// somebody writes a step in a loop.
//
// **Every mutating step carries dry_run.** Nothing here writes to the
// organization it runs against. The dry-run path is still real
// coverage — the tool resolves names, builds the request and validates
// its arguments before the client's gate turns it into a record rather
// than a request — and it is the only way to exercise a delete against
// a tenant somebody works in.
func Steps() []Step {
	steps := append(seedSteps(), optionSteps()...)
	steps = append(steps, extraSteps()...)
	sort.SliceStable(steps, func(i, j int) bool { return rank(steps[i]) < rank(steps[j]) })
	return steps
}

// seedSteps fill the id pool, and they are hand-written because their
// dependencies are Favro's: columns are listed per widget, comments and
// checklists per card, and a card listing needs a widget or a
// collection to scope it.
//
// Two properties matter and neither survives generation. They take the
// minimum arguments, so a seed cannot fail on an option unrelated to
// the id it is there to fetch; and they set no filter, because a
// generated `name` filter is what made the first run list zero tags and
// then skip every step needing a tag id.
func seedSteps() []Step {
	return []Step{
		{Tool: "favro_ping", Args: map[string]any{}, Why: "seed: the server answers at all"},
		{Tool: "favro_list_organizations", Args: map[string]any{}, Why: "seed: an organization id"},
		{Tool: "favro_list_users", Args: map[string]any{}, Why: "seed: a user id"},
		{Tool: "favro_list_collections", Args: map[string]any{}, Why: "seed: a collection id"},
		{Tool: "favro_list_widgets", Args: map[string]any{}, Why: "seed: a widget id"},
		{Tool: "favro_list_tags", Args: map[string]any{}, Why: "seed: a tag id"},
		{Tool: "favro_list_custom_fields", Args: map[string]any{}, Why: "seed: a custom field id"},
		{Tool: "favro_list_groups", Args: map[string]any{}, Why: "seed: a group id"},
		{
			Tool: "favro_list_columns",
			Args: map[string]any{"widget_common_id": AnyWidgetCommonID},
			Why:  "seed: a column id, which only exists per widget",
		},
		{
			Tool: "favro_list_cards",
			Args: map[string]any{"widget_common_id": AnyWidgetCommonID},
			Why:  "seed: a card id and a card common id",
		},
		{
			Tool: "favro_list_comments",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "seed: a comment id, which only exists per card",
		},
		{
			Tool: "favro_list_tasklists",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "seed: a checklist id",
		},
		{
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
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"card_id": AnyCardID,
				"comment": "livefavro probe",
				"dry_run": true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_add_dependencies",
			Args: map[string]any{
				"card_id":      AnyCardID,
				"dependencies": []map[string]any{{"cardId": AnyCardCommonID, "isBefore": true}},
				"dry_run":      true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_add_tag_to_card",
			Args: map[string]any{
				"card_id":  AnyCardID,
				"tag_name": "livefavro-probe-no-such-tag",
				"dry_run":  true,
			},
			ExpectError: "not_found",
			Why:         "hard rule 7, live: an unknown tag name must be refused, not created",
		},
		{
			Tool: "favro_append_card_description",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
				"text":    "livefavro probe",
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_archive_card",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
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
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_create_collection",
			Args: map[string]any{
				"background":                   "blue",
				"color":                        "blue",
				"dry_run":                      true,
				"full_members_can_add_widgets": false,
				"icon_name":                    "rocket",
				"name":                         "livefavro probe",
				"public_sharing":               "organization",
				"star_page":                    false,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_create_column",
			Args: map[string]any{
				"color":            "blue",
				"dry_run":          true,
				"name":             "livefavro probe",
				"position":         1,
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_create_comment",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"comment":        "livefavro probe",
				"dry_run":        true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_create_group",
			Args: map[string]any{
				"dry_run": true,
				"name":    "livefavro probe",
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_create_tag",
			Args: map[string]any{
				"color":   "blue",
				"dry_run": true,
				"name":    "livefavro probe",
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_create_task",
			Args: map[string]any{
				"completed":    false,
				"dry_run":      true,
				"name":         "livefavro probe",
				"position":     1,
				"task_list_id": AnyTaskListID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_create_tasklist",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"dry_run":        true,
				"name":           "livefavro probe",
				"position":       1,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
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
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_all_dependencies",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_card",
			Args: map[string]any{
				"card_id":    AnyCardID,
				"dry_run":    true,
				"everywhere": false,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_collection",
			Args: map[string]any{
				"collection_id": AnyCollectionID,
				"dry_run":       true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_column",
			Args: map[string]any{
				"column_id":        AnyColumnID,
				"dry_run":          true,
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_comment",
			Args: map[string]any{
				"comment_id": AnyCommentID,
				"dry_run":    true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_dependency",
			Args: map[string]any{
				"card_id":            AnyCardID,
				"dependency_card_id": "livefavro probe",
				"dry_run":            true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_group",
			Args: map[string]any{
				"dry_run":  true,
				"group_id": AnyGroupID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_tag",
			Args: map[string]any{
				"dry_run": true,
				"tag_id":  AnyTagID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_task",
			Args: map[string]any{
				"dry_run": true,
				"task_id": AnyTaskID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_tasklist",
			Args: map[string]any{
				"dry_run":      true,
				"task_list_id": AnyTaskListID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_webhook",
			Args: map[string]any{
				"dry_run":    true,
				"webhook_id": "wh-probe",
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_delete_widget",
			Args: map[string]any{
				"collection_id":    AnyCollectionID,
				"dry_run":          true,
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_get_card",
			Args: map[string]any{
				"card_id": AnyCardID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_card_full",
			Args: map[string]any{
				"card_id":          AnyCardID,
				"comment_limit":    1,
				"include_comments": false,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_collection",
			Args: map[string]any{
				"collection_id": AnyCollectionID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_column",
			Args: map[string]any{
				"column_id": AnyColumnID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_comment",
			Args: map[string]any{
				"comment_id": AnyCommentID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_custom_field",
			Args: map[string]any{
				"custom_field_id": AnyCustomFieldID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_group",
			Args: map[string]any{
				"group_id": AnyGroupID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_organization",
			Args: map[string]any{},
			Why:  "reads",
		},
		{
			Tool: "favro_get_tag",
			Args: map[string]any{
				"tag_id": AnyTagID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_task",
			Args: map[string]any{
				"task_id": AnyTaskID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_tasklist",
			Args: map[string]any{
				"task_list_id": AnyTaskListID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_user",
			Args: map[string]any{
				"user_id": AnyUserID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_get_widget",
			Args: map[string]any{
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_card_activities",
			Args: map[string]any{
				"card_id": AnyCardID,
				"page":    1,
				"since":   "2020-01-01T00:00:00Z",
			},
			Why: "reads a card's history, with a since filter Favro will accept",
		},
		{
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
			Why: "reads",
		},
		{
			Tool: "favro_list_collections",
			Args: map[string]any{
				"archived":   false,
				"page":       1,
				"request_id": "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_columns",
			Args: map[string]any{
				"page":             1,
				"request_id":       "livefavro probe",
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_comments",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"page":           1,
				"request_id":     "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_custom_fields",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_dependencies",
			Args: map[string]any{
				"card_id": AnyCardID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_groups",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_organizations",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_tags",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_tasklists",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"page":           1,
				"request_id":     "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_tasks",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"page":           1,
				"request_id":     "livefavro probe",
				"task_list_id":   AnyTaskListID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_users",
			Args: map[string]any{
				"page":       1,
				"request_id": "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_webhooks",
			Args: map[string]any{
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_list_widgets",
			Args: map[string]any{
				"archived":      false,
				"collection_id": AnyCollectionID,
				"page":          1,
				"request_id":    "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_move_card",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"column_id": AnyColumnID,
				"dry_run":   true,
			},
			Why: "dry-run: a move needs a destination, and a column is one",
		},
		{
			Tool: "favro_ping",
			Args: map[string]any{},
			Why:  "reads",
		},
		{
			Tool: "favro_prepend_card_description",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
				"text":    "livefavro probe",
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_rate_limit_status",
			Args: map[string]any{},
			Why:  "reads",
		},
		{
			Tool: "favro_remove_attachment",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"dry_run":   true,
				"file_urls": []string{"https://example.invalid/not-attached.txt"},
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_remove_tag_from_card",
			Args: map[string]any{
				"card_id":  AnyCardID,
				"tag_name": "livefavro-probe-no-such-tag",
				"dry_run":  true,
			},
			ExpectError: "not_found",
			Why:         "the same refusal on the way out",
		},
		{
			Tool: "favro_replace_dependencies",
			Args: map[string]any{
				"card_id":      AnyCardID,
				"dependencies": []map[string]any{{"cardId": AnyCardCommonID, "isBefore": false}},
				"dry_run":      true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
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
			Why:         "\u00a77.3, live: a find that matches nothing must refuse rather than PUT the body back unchanged",
		},
		{
			Tool: "favro_resolve_collection",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_resolve_column",
			Args: map[string]any{
				"force_refresh":    true,
				"limit":            5,
				"name":             "livefavro probe",
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_resolve_custom_field",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_resolve_group",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_resolve_tag",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_resolve_user",
			Args: map[string]any{
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_resolve_widget",
			Args: map[string]any{
				"collection_id": AnyCollectionID,
				"force_refresh": true,
				"limit":         5,
				"name":          "livefavro probe",
			},
			Why: "reads",
		},
		{
			Tool: "favro_search_cards",
			Args: map[string]any{
				"force_refresh":    true,
				"include_archived": false,
				"limit":            5,
				"min_score":        1,
				"query":            "a",
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "reads",
		},
		{
			Tool: "favro_set_card_custom_field",
			Args: map[string]any{
				"card_id":         AnyCardID,
				"custom_field_id": AnyCustomFieldID,
				"dry_run":         true,
			},
			ExpectError: "invalid",
			Why:         "\u00a77.4, live: the driver cannot know the field's type, and the refusal names every legal input for it",
		},
		{
			Tool: "favro_unarchive_card",
			Args: map[string]any{
				"card_id": AnyCardID,
				"dry_run": true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_update_card",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"drag_mode": "livefavro probe",
				"dry_run":   true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
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
				"public_sharing":               "organization",
				"star_page":                    false,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_update_column",
			Args: map[string]any{
				"color":            "blue",
				"column_id":        AnyColumnID,
				"dry_run":          true,
				"name":             "livefavro probe",
				"position":         1,
				"widget_common_id": AnyWidgetCommonID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_update_comment",
			Args: map[string]any{
				"comment":    "livefavro probe",
				"comment_id": AnyCommentID,
				"dry_run":    true,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_update_dependency",
			Args: map[string]any{
				"card_id":            AnyCardID,
				"dependency_card_id": "livefavro probe",
				"dry_run":            true,
				"is_before":          false,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_update_group",
			Args: map[string]any{
				"dry_run":  true,
				"group_id": AnyGroupID,
				"name":     "livefavro probe",
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_update_tag",
			Args: map[string]any{
				"color":   "blue",
				"dry_run": true,
				"name":    "livefavro probe",
				"tag_id":  AnyTagID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_update_tags",
			Args: map[string]any{
				"updates": []map[string]any{{"tag_id": AnyTagID, "name": "livefavro probe"}},
				"dry_run": true,
			},
			Why: "dry-run: the bulk path, which is a client-side fan-out rather than a Favro endpoint",
		},
		{
			Tool: "favro_update_task",
			Args: map[string]any{
				"completed": false,
				"dry_run":   true,
				"name":      "livefavro probe",
				"position":  1,
				"task_id":   AnyTaskID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_update_tasklist",
			Args: map[string]any{
				"dry_run":      true,
				"name":         "livefavro probe",
				"position":     1,
				"task_list_id": AnyTaskListID,
			},
			Why: "dry-run: builds the request without sending it",
		},
		{
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
			Why: "dry-run: builds the request without sending it",
		},
		{
			Tool: "favro_upload_attachment",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"file_path": "/dev/null",
				"dry_run":   true,
			},
			ExpectError: "invalid",
			Why:         "the not-a-regular-file guard, live: a device node must be refused before anything is read into memory",
		},
		{
			Tool: "favro_upload_comment_attachment",
			Args: map[string]any{
				"comment_id": AnyCommentID,
				"file_path":  "/dev/null",
				"dry_run":    true,
			},
			ExpectError: "invalid",
			Why:         "the not-a-regular-file guard, live: a device node must be refused before anything is read into memory",
		},
	}
}

// rank orders the run: seeds first, in the order they are written, then
// the remaining reads, then everything that mutates — which by then has
// real ids to work with, and carries dry_run regardless.
func rank(s Step) int {
	for i, seed := range seedSteps() {
		if seed.Tool == s.Tool && len(s.Args) == len(seed.Args) && s.Why == seed.Why {
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
