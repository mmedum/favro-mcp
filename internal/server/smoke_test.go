package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// This file is the tools/call counterpart to
// TestMCP_ToolsList_IncludesFavroPing, which only proves a tool is
// advertised. Registering a tool and being able to *invoke* it are
// different things: a handler can panic on minimal input, an input
// struct can drift from the arguments the tool description tells the
// LLM to send, a field can be required by the handler but optional in
// the schema (or the reverse), and a dry-run path can forget to
// short-circuit before touching HTTP. None of those show up in
// tools/list.

// smokeFilePathPlaceholder stands in for a real file path in
// smokeToolInputs. The attachment tools read from disk, so the value
// is substituted for a temp file created per-run — a package-level
// map can't hold a path that doesn't exist yet.
const smokeFilePathPlaceholder = "<TEMPFILE>"

// Fixture identifiers, shared between smokeFixtureHandler's responses
// and the arguments below so lookups resolve rather than 404.
const (
	smokeCardID        = "ci-1"
	smokeCardCommonID  = "cc-1"
	smokeCollectionID  = "c-1"
	smokeWidgetID      = "w-1"
	smokeColumnID      = "col-1"
	smokeCommentID     = "cm-1"
	smokeTagID         = "t-1"
	smokeTagName       = "smoke-tag"
	smokeCustomFieldID = "cf-1"
	smokeGroupID       = "g-1"
	smokeUserID        = "u-1"
	smokeOrgID         = "fixture-org-1"
	smokeTaskID        = "tk-1"
	smokeTaskListID    = "tl-1"
)

// smokeToolInputs is the minimal argument set for every registered
// tool. "Minimal" means what the tool's own description says is
// required to do the simplest useful thing — not every optional knob.
//
// Mutating tools pass dry_run: true. That is not merely to avoid
// writes: several of them still perform a *read* first (the
// description editors fetch the card, favro_add_tag_to_card resolves
// the tag name, favro_set_card_custom_field resolves the field type),
// so the dry-run path exercises real request-building while the write
// itself short-circuits.
//
// Adding a tool without adding a row here fails the test — see the
// coverage assertion in TestMCP_AllTools_SmokeCallable.
var smokeToolInputs = map[string]map[string]any{
	// ---- Diagnostics (never contact Favro) ----
	pingToolName:      {},
	rateLimitToolName: {},

	// ---- Reads ----
	listOrgsToolName:         {},
	getOrgToolName:           {"organization_id": smokeOrgID},
	listUsersToolName:        {},
	getUserToolName:          {"user_id": smokeUserID},
	listCollectionsToolName:  {},
	getCollectionToolName:    {"collection_id": smokeCollectionID},
	listWidgetsToolName:      {},
	getWidgetToolName:        {"widget_common_id": smokeWidgetID},
	listColumnsToolName:      {"widget_common_id": smokeWidgetID},
	getColumnToolName:        {"column_id": smokeColumnID},
	listCardsToolName:        {"widget_common_id": smokeWidgetID},
	getCardToolName:          {"card_id": smokeCardID},
	listCommentsToolName:     {"card_common_id": smokeCardCommonID},
	getCommentToolName:       {"comment_id": smokeCommentID},
	listTagsToolName:         {},
	getTagToolName:           {"tag_id": smokeTagID},
	listCustomFieldsToolName: {},
	getCustomFieldToolName:   {"custom_field_id": smokeCustomFieldID},
	listGroupsToolName:       {},
	getGroupToolName:         {"group_id": smokeGroupID},
	listTasksToolName:        {"card_common_id": smokeCardCommonID},
	getTaskToolName:          {"task_id": smokeTaskID},
	listTasklistsToolName:    {"card_common_id": smokeCardCommonID},
	getTasklistToolName:      {"task_list_id": smokeTaskListID},
	listDependenciesToolName: {"card_id": smokeCardID},

	listCardActivitiesToolName: {"card_id": smokeCardID},

	// ---- Workflow reads ----
	// The resolvers rank against the org-global list; a miss returns
	// an empty candidate list, not an error.
	resolveTagToolName:         {"name": smokeTagName},
	resolveUserToolName:        {"name": "Fixture User"},
	resolveCollectionToolName:  {"name": "Fixture Collection"},
	resolveWidgetToolName:      {"name": "Fixture Widget"},
	resolveColumnToolName:      {"widget_common_id": smokeWidgetID, "name": "Doing"},
	resolveCustomFieldToolName: {"name": "Fixture Field"},
	resolveGroupToolName:       {"name": "Fixture Group"},
	// Search requires exactly one of widget_common_id / collection_id
	// — Favro rejects unscoped card listings.
	searchCardsToolName: {"query": "smoke", "widget_common_id": smokeWidgetID},
	// get_card_full requires exactly one identity flavor.
	getCardFullToolName: {"card_id": smokeCardID},

	// ---- Tag writes ----
	createTagToolName: {"name": smokeTagName, "dry_run": true},
	updateTagToolName: {"tag_id": smokeTagID, "name": "renamed", "dry_run": true},
	deleteTagToolName: {"tag_id": smokeTagID, "dry_run": true},
	updateTagsToolName: {
		"updates": []map[string]any{{"tag_id": smokeTagID, "name": "renamed"}},
		"dry_run": true,
	},

	// ---- Comment writes ----
	createCommentToolName: {"card_common_id": smokeCardCommonID, "comment": "hi", "dry_run": true},
	updateCommentToolName: {"comment_id": smokeCommentID, "comment": "edited", "dry_run": true},
	deleteCommentToolName: {"comment_id": smokeCommentID, "dry_run": true},

	// ---- Card writes ----
	createCardToolName:    {"name": "smoke card", "widget_common_id": smokeWidgetID, "dry_run": true},
	updateCardToolName:    {"card_id": smokeCardID, "name": "renamed", "dry_run": true},
	archiveCardToolName:   {"card_id": smokeCardID, "dry_run": true},
	unarchiveCardToolName: {"card_id": smokeCardID, "dry_run": true},
	// A move needs a destination, and a column move additionally needs
	// widget + column + list_position + drag_mode together or Favro
	// silently no-ops it — so the minimal call here is the full set.
	moveCardToolName: {
		"card_id":          smokeCardID,
		"widget_common_id": smokeWidgetID,
		"column_id":        smokeColumnID,
		"list_position":    1,
		"drag_mode":        "move",
		"dry_run":          true,
	},
	deleteCardToolName: {"card_id": smokeCardID, "dry_run": true},

	// ---- Collection / widget / column writes ----
	createCollectionToolName: {"name": "smoke collection", "dry_run": true},
	updateCollectionToolName: {"collection_id": smokeCollectionID, "name": "renamed", "dry_run": true},
	deleteCollectionToolName: {"collection_id": smokeCollectionID, "dry_run": true},
	createWidgetToolName:     {"collection_id": smokeCollectionID, "name": "smoke widget", "dry_run": true},
	updateWidgetToolName:     {"widget_common_id": smokeWidgetID, "name": "renamed", "dry_run": true},
	deleteWidgetToolName:     {"widget_common_id": smokeWidgetID, "dry_run": true},
	createColumnToolName:     {"widget_common_id": smokeWidgetID, "name": "Doing", "dry_run": true},
	updateColumnToolName:     {"column_id": smokeColumnID, "name": "renamed", "dry_run": true},
	deleteColumnToolName:     {"column_id": smokeColumnID, "dry_run": true},

	// ---- Group writes ----
	createGroupToolName: {"name": "smoke group", "dry_run": true},
	updateGroupToolName: {"group_id": smokeGroupID, "name": "renamed", "dry_run": true},
	deleteGroupToolName: {"group_id": smokeGroupID, "dry_run": true},

	// ---- Custom-field write ----
	// The fixture reports cf-1 as a Text field, so `text` is the
	// matching value input.
	setCardCustomFieldToolName: {
		"card_id":         smokeCardID,
		"custom_field_id": smokeCustomFieldID,
		"text":            "smoke",
		"dry_run":         true,
	},

	// ---- Workflow writes ----
	appendCardDescriptionToolName:    {"card_id": smokeCardID, "text": "appended", "dry_run": true},
	prependCardDescriptionToolName:   {"card_id": smokeCardID, "text": "prepended", "dry_run": true},
	replaceInCardDescriptionToolName: {"card_id": smokeCardID, "find": "body", "replace": "replaced", "dry_run": true},
	addCommentToCardToolName:         {"card_id": smokeCardID, "comment": "hi", "dry_run": true},
	// The tag-to-card tools hard-fail on an unknown tag name, so the
	// fixture's tag list has to contain smokeTagName.
	addTagToCardToolName:      {"card_id": smokeCardID, "tag_name": smokeTagName, "dry_run": true},
	removeTagFromCardToolName: {"card_id": smokeCardID, "tag_name": smokeTagName, "dry_run": true},

	// ---- Attachments ----
	uploadAttachmentToolName: {
		"card_id":   smokeCardID,
		"file_path": smokeFilePathPlaceholder,
		"dry_run":   true,
	},
	uploadCommentAttachmentToolName: {
		"comment_id": smokeCommentID,
		"file_path":  smokeFilePathPlaceholder,
		"dry_run":    true,
	},
	removeAttachmentToolName: {
		"card_id":   smokeCardID,
		"file_urls": []string{"https://files.invalid/smoke.txt"},
		"dry_run":   true,
	},

	// ---- Checklists ----
	createTaskToolName:     {"task_list_id": smokeTaskListID, "name": "smoke task", "dry_run": true},
	updateTaskToolName:     {"task_id": smokeTaskID, "name": "renamed", "dry_run": true},
	deleteTaskToolName:     {"task_id": smokeTaskID, "dry_run": true},
	createTasklistToolName: {"card_common_id": smokeCardCommonID, "name": "smoke list", "dry_run": true},
	updateTasklistToolName: {"task_list_id": smokeTaskListID, "name": "renamed", "dry_run": true},
	deleteTasklistToolName: {"task_list_id": smokeTaskListID, "dry_run": true},

	// ---- Dependencies ----
	addDependenciesToolName: {
		"card_id":      smokeCardID,
		"dependencies": []map[string]any{{"cardId": "ci-2", "isBefore": true}},
		"dry_run":      true,
	},
	replaceDependenciesToolName: {
		"card_id":      smokeCardID,
		"dependencies": []map[string]any{{"cardId": "ci-2", "isBefore": true}},
		"dry_run":      true,
	},
	updateDependencyToolName: {
		"card_id":            smokeCardID,
		"dependency_card_id": "ci-2",
		"is_before":          false,
		"dry_run":            true,
	},
	deleteDependencyToolName:      {"card_id": smokeCardID, "dependency_card_id": "ci-2", "dry_run": true},
	deleteAllDependenciesToolName: {"card_id": smokeCardID, "dry_run": true},
}

// TestMCP_AllTools_SmokeCallable invokes every registered tool over
// the real in-memory MCP transport with minimal valid input and
// asserts none of them errors.
//
// The coverage assertion is the part that keeps this honest: the
// input table is checked against tools/list in both directions, so a
// newly registered tool fails the test until someone decides what
// calling it minimally looks like.
func TestMCP_AllTools_SmokeCallable(t *testing.T) {
	t.Parallel()

	filePath := writeSmokeAttachment(t)
	c := favroFixture(t, smokeFixtureHandler(t))
	cs := connectInMemoryWith(t, c)

	res, err := cs.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, res.Tools)

	advertised := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		advertised[tool.Name] = true
	}
	for name := range smokeToolInputs {
		require.True(t, advertised[name],
			"smokeToolInputs has a row for %q, which is no longer registered — remove it", name)
	}

	for _, tool := range res.Tools {
		args, ok := smokeToolInputs[tool.Name]
		require.True(t, ok,
			"tool %q is registered but has no smokeToolInputs row; add the minimal arguments "+
				"needed to invoke it so it gets smoke coverage", tool.Name)

		t.Run(tool.Name, func(t *testing.T) {
			t.Parallel()

			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      tool.Name,
				Arguments: substituteSmokePlaceholders(args, filePath),
			})
			require.NoError(t, err, "transport-level failure calling %q", tool.Name)
			require.False(t, res.IsError,
				"%s returned a tool error on minimal input: %s",
				tool.Name, serializedResponseString(t, res))
		})
	}
}

// substituteSmokePlaceholders returns a copy of args with
// smokeFilePathPlaceholder replaced by the per-run temp file. Copying
// rather than mutating keeps the package-level table usable from
// parallel subtests.
func substituteSmokePlaceholders(args map[string]any, filePath string) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && s == smokeFilePathPlaceholder {
			out[k] = filePath
			continue
		}
		out[k] = v
	}
	return out
}

// writeSmokeAttachment creates the small file the attachment upload
// tools read from disk.
func writeSmokeAttachment(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smoke.txt")
	require.NoError(t, os.WriteFile(path, []byte("smoke"), 0o600))
	return path
}

// smokeFixtureHandler answers each Favro endpoint the smoke calls
// reach with a minimal well-formed payload. It routes on path rather
// than returning one universal blob so a decode mismatch surfaces as
// a failing tool call instead of silently passing on zero values.
//
// An unrouted path fails the test rather than 404ing: a tool reaching
// an endpoint nobody modelled here is exactly the drift this file
// exists to catch.
func smokeFixtureHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := smokeResponseFor(r)
		if !ok {
			t.Errorf("smoke fixture has no response for %s %s — add one", r.Method, r.URL.Path)
			http.Error(w, "{}", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

// smokeResponseFor picks the payload for one request. Collection
// endpoints ("/tags") return a single-entity page envelope; item
// endpoints ("/tags/t-1") return the bare object.
func smokeResponseFor(r *http.Request) (string, bool) {
	path := strings.Trim(r.URL.Path, "/")
	segments := strings.Split(path, "/")

	// DELETE /cards/{id} is the one delete that returns a body.
	if r.Method == http.MethodDelete {
		if len(segments) == 2 && segments[0] == "cards" {
			return `["` + smokeCardID + `"]`, true
		}
		return `{}`, true
	}

	// Sub-resources: /cards/{id}/<sub>.
	if len(segments) == 3 && segments[0] == "cards" {
		switch segments[2] {
		case "dependencies":
			return `{"cardId":"` + smokeCardID + `","cardCommonId":"` + smokeCardCommonID + `","dependencies":[]}`, true
		case "activities":
			return smokePage(`{"type":"assigned","cardId":"` + smokeCardID + `","byUserId":"` + smokeUserID + `"}`), true
		case "attachment":
			return `{"name":"smoke.txt","fileURL":"https://files.invalid/smoke.txt"}`, true
		}
		return "", false
	}
	if len(segments) == 3 && segments[0] == "comments" && segments[2] == "attachment" {
		return `{"name":"smoke.txt","fileURL":"https://files.invalid/smoke.txt"}`, true
	}

	entity, ok := smokeEntities[segments[0]]
	if !ok {
		return "", false
	}
	if len(segments) == 1 {
		return smokePage(entity), true
	}
	return entity, true
}

// smokeEntities maps a top-level Favro collection path to one
// well-formed entity of that kind.
var smokeEntities = map[string]string{
	"organizations": `{"organizationId":"` + smokeOrgID + `","name":"Fixture Org"}`,
	"users":         `{"userId":"` + smokeUserID + `","name":"Fixture User","email":"user@fixture.invalid"}`,
	"collections":   `{"collectionId":"` + smokeCollectionID + `","name":"Fixture Collection"}`,
	"widgets":       `{"widgetCommonId":"` + smokeWidgetID + `","name":"Fixture Widget","collectionIds":["` + smokeCollectionID + `"]}`,
	"columns":       `{"columnId":"` + smokeColumnID + `","widgetCommonId":"` + smokeWidgetID + `","name":"Doing"}`,
	"cards": `{"cardId":"` + smokeCardID + `","cardCommonId":"` + smokeCardCommonID + `","name":"Fixture Card",` +
		`"widgetCommonId":"` + smokeWidgetID + `","columnId":"` + smokeColumnID + `","detailedDescription":"body"}`,
	"comments":     `{"commentId":"` + smokeCommentID + `","cardCommonId":"` + smokeCardCommonID + `","comment":"hi","userId":"` + smokeUserID + `"}`,
	"tags":         `{"tagId":"` + smokeTagID + `","name":"` + smokeTagName + `","color":"blue"}`,
	"customfields": `{"customFieldId":"` + smokeCustomFieldID + `","name":"Fixture Field","type":"Text","enabled":true}`,
	"groups":       `{"groupId":"` + smokeGroupID + `","name":"Fixture Group"}`,
	"tasks":        `{"taskId":"` + smokeTaskID + `","taskListId":"` + smokeTaskListID + `","name":"Fixture Task"}`,
	"tasklists":    `{"taskListId":"` + smokeTaskListID + `","cardCommonId":"` + smokeCardCommonID + `","name":"Fixture List"}`,
}

// smokePage wraps one entity in the paginated envelope Favro returns
// from collection endpoints.
func smokePage(entity string) string {
	return `{"limit":100,"page":0,"pages":1,"requestId":"smoke","entities":[` + entity + `]}`
}
