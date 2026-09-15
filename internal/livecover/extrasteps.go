package livecover

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
			Tool: "favro_get_card_full",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "the cross-widget identity, which Favro's own GET /cards/{id} answers 403 for",
		},
		{
			Tool: "favro_get_card_full",
			Args: map[string]any{"sequential_id": AnySequentialID},
			Why:  "the integer inside a human card reference",
		},
		{
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"comment":        "livefavro probe",
				"dry_run":        true,
			},
			Why: "dry-run: addressing the card by its cross-widget id",
		},
		{
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"sequential_id": AnySequentialID,
				"comment":       "livefavro probe",
				"dry_run":       true,
			},
			Why: "dry-run: addressing the card by its sequential id",
		},
		{
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"search_query":     "a",
				"widget_common_id": AnyWidgetCommonID,
				"comment":          "livefavro probe",
				"dry_run":          true,
			},
			Why: "dry-run: finding the card by search, scoped to a widget",
		},
		{
			Tool: "favro_add_comment_to_card",
			Args: map[string]any{
				"search_query":  "a",
				"collection_id": AnyCollectionID,
				"comment":       "livefavro probe",
				"dry_run":       true,
			},
			Why: "dry-run: the same, scoped to a collection",
		},

		// The card listing's other filters.
		{
			Tool: "favro_list_cards",
			Args: map[string]any{"card_common_id": AnyCardCommonID},
			Why:  "the cross-widget filter, which is the only way to fetch a card by common id",
		},
		{
			Tool: "favro_list_cards",
			Args: map[string]any{"sequential_id": AnySequentialID},
			Why:  "the sequential-id filter",
		},
		{
			Tool: "favro_list_cards",
			Args: map[string]any{
				"widget_common_id": AnyWidgetCommonID,
				"column_id":        AnyColumnID,
			},
			Why: "narrowed to one column of one widget",
		},
		{
			Tool: "favro_list_cards",
			Args: map[string]any{"todo_list": true},
			Why:  "the caller's personal todo list, which is scoped by the token rather than by a widget",
		},
		{
			Tool: "favro_list_tags",
			Args: map[string]any{"name": "livefavro-probe-no-such-tag"},
			Why:  "the name filter, which returns an empty page rather than an error",
		},
		{
			Tool: "favro_create_collection",
			Args: map[string]any{
				"name":           "livefavro probe",
				"share_to_users": []any{map[string]any{"userId": AnyUserID, "role": "guest"}},
				"dry_run":        true,
			},
			Why: "dry-run: the write-side sharing key, which §7.4 records Favro spelling differently from the read",
		},
		{
			Tool: "favro_create_group",
			Args: map[string]any{
				"name":    "livefavro probe",
				"members": []any{map[string]any{"userId": AnyUserID, "role": "administrator"}},
				"dry_run": true,
			},
			Why: "dry-run: a group created with its membership in one call",
		},
		{
			Tool: "favro_create_tasklist",
			Args: map[string]any{
				"card_common_id": AnyCardCommonID,
				"name":           "livefavro probe",
				"tasks":          []any{map[string]any{"name": "first"}, map[string]any{"name": "second", "completed": false}},
				"dry_run":        true,
			},
			Why: "dry-run: seeding a checklist with its items in the same request",
		},
		{
			Tool: "favro_search_cards",
			Args: map[string]any{"query": "a", "collection_id": AnyCollectionID, "limit": 5},
			Why:  "search scoped to a collection rather than a widget",
		},
		{
			Tool: "favro_list_card_activities",
			Args: map[string]any{"card_id": AnyCardID, "until": "2030-01-01T00:00:00Z"},
			Why:  "the upper bound of the history window",
		},

		// The optional fields a write can carry. Dry-run builds the
		// body, so this is the shape of the request being checked.
		{
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
			Why: "dry-run: every optional field a new card can carry",
		},
		{
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
			Why: "dry-run: every field an update can set at once",
		},
		{
			Tool: "favro_update_card",
			Args: map[string]any{
				"card_id":     AnyCardID,
				"column_id":   AnyColumnID,
				"skip_verify": true,
				"dry_run":     true,
			},
			Why: "dry-run: the placement write with the read-back turned off",
		},
		{
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
			Why: "dry-run: a move across widgets, with every positioning knob",
		},
		{
			Tool: "favro_update_collection",
			Args: map[string]any{
				"collection_id":  AnyCollectionID,
				"share_to_users": []any{map[string]any{"userId": AnyUserID, "role": "guest"}},
				"members":        []any{map[string]any{"userId": AnyUserID, "role": "guest"}},
				"dry_run":        true,
			},
			Why: "dry-run: the sharing keys, where §7.4 records Favro reading one name and writing another",
		},
		{
			Tool: "favro_update_group",
			Args: map[string]any{
				"group_id": AnyGroupID,
				"members":  []any{map[string]any{"userId": AnyUserID, "role": "administrator"}},
				"dry_run":  true,
			},
			Why: "dry-run: whole-list membership, which §7.4 records the docs disagreeing about",
		},
		{
			Tool: "favro_update_comment",
			Args: map[string]any{
				"comment_id":         AnyCommentID,
				"comment":            "livefavro probe",
				"remove_attachments": []any{"https://example.invalid/not-attached.txt"},
				"dry_run":            true,
			},
			Why: "dry-run: attachment removal, matched by URL rather than by name",
		},
		{
			Tool: "favro_upload_attachment",
			Args: map[string]any{
				"card_id":   AnyCardID,
				"file_path": "/dev/null",
				"filename":  "livefavro-probe.txt",
				"mime_type": "text/plain",
				"dry_run":   true,
			},
			ExpectError: "invalid",
			Why:         "the guard again, with the name and type overrides set",
		},
		{
			Tool: "favro_upload_comment_attachment",
			Args: map[string]any{
				"comment_id": AnyCommentID,
				"file_path":  "/dev/null",
				"filename":   "livefavro-probe.txt",
				"mime_type":  "text/plain",
				"dry_run":    true,
			},
			ExpectError: "invalid",
			Why:         "the same on the comment path",
		},
	}
}
