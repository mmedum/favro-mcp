package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
	"github.com/mmedum/favro-mcp/internal/favroapi"
)

// customFieldFixture wires a Favro server that:
//   - GET /customfields → returns the supplied [field] list
//   - PUT /cards/{id}    → echoes a Card back, optionally capturing
//     the request body in *capturedBody when non-nil
//
// Used by every test that exercises favro_set_card_custom_field.
func customFieldFixture(t *testing.T, capturedBody *string, fields []favro.CustomField) *favroapi.Client {
	t.Helper()
	return favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.CustomField]{
				Pages:    1,
				Entities: fields,
			})
		case http.MethodPut:
			if capturedBody != nil {
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read PUT body: %v", err)
				}
				*capturedBody = string(b)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"x"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
}

func TestMCP_SetCardCustomField_Text_HappyPath(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, nil, []favro.CustomField{
		{CustomFieldID: "cf-text", Type: "Text", Name: "Notes"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-text",
			"text":            "hello",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	if got := out.Result.CardID; got != "ci-1" {
		t.Errorf("out.Result.CardID = %v, want %v", got, "ci-1")
	}
}

func TestMCP_SetCardCustomField_Number_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-num", Type: "Number", Name: "Cost"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-num",
			"number":          42,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"total":42`) {
		t.Errorf("Number writes travel in `total`, not `value`: %q missing", `"total":42`)
	}
	if strings.Contains(body, `"value"`) {
		t.Errorf("body unexpectedly contains %q", `"value"`)
	}
}

func TestMCP_SetCardCustomField_Date_HappyPath(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, nil, []favro.CustomField{
		{CustomFieldID: "cf-date", Type: "Date", Name: "Deadline"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-date",
			"date":            "2026-05-06T00:00:00Z",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
}

func TestMCP_SetCardCustomField_Checkbox_HappyPath(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, nil, []favro.CustomField{
		{CustomFieldID: "cf-bool", Type: "Checkbox", Name: "Approved"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-bool",
			"checkbox":        true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
}

func TestMCP_SetCardCustomField_SingleSelect_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{
			CustomFieldID: "cf-sel",
			Type:          "Single select",
			Name:          "Priority",
			CustomFieldItems: []favro.CustomFieldItem{
				{CustomFieldItemID: "item-1", Name: "High"},
			},
		},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":               "ci-1",
			"custom_field_id":       "cf-sel",
			"single_select_item_id": "item-1",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"value":["item-1"]`) {
		t.Errorf("select-flavored writes put item ids in `value`: %q missing", `"value":["item-1"]`)
	}
}

// TestMCP_SetCardCustomField_DryRun pins that the type-resolution
// path runs (so the LLM sees the "would set custom field X (type Y)"
// state-diff) but the network is never touched for the PUT.
func TestMCP_SetCardCustomField_DryRun(t *testing.T) {
	t.Parallel()

	var puts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.CustomField]{
				Pages:    1,
				Entities: []favro.CustomField{{CustomFieldID: "cf-text", Type: "Text", Name: "Notes"}},
			})
		case http.MethodPut:
			puts.Add(1)
		}
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-text",
			"text":            "preview",
			"dry_run":         true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if !strings.Contains(out.PredictedStateDiff, "Notes") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "Notes")
	}
	if !strings.Contains(out.PredictedStateDiff, "Text") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "Text")
	}
	if got := puts.Load(); got != 0 {
		t.Errorf("dry_run must short-circuit before any PUT: got %v, want %v", got, 0)
	}
}

// TestMCP_SetCardCustomField_TypeMismatch pins that supplying the
// wrong value-kind (e.g. `text` for a Number field) is rejected
// with an error that names the field's expected type.
func TestMCP_SetCardCustomField_TypeMismatch(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, nil, []favro.CustomField{
		{CustomFieldID: "cf-num", Type: "Number", Name: "Cost"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-num",
			"text":            "wrong-shape",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "number") {
		t.Errorf("strings.ToLower(serializedResponseString(t, res)) does not contain %q", "number")
	}
}

// TestMCP_SetCardCustomField_UnsupportedType pins that types outside
// the dispatch table reject with a typed error rather than silently
// falling through to one of the supported applicators. Progress and
// Sequential ID are calculated by Favro; Relations and Date created
// have no documented write contract.
func TestMCP_SetCardCustomField_UnsupportedType(t *testing.T) {
	t.Parallel()

	cases := []string{"Progress", "Relations", "Sequential ID", "Date created"}
	for _, deferred := range cases {
		t.Run(deferred, func(t *testing.T) {
			t.Parallel()

			c := customFieldFixture(t, nil, []favro.CustomField{
				{CustomFieldID: "cf-x", Type: deferred, Name: "x"},
			})

			cs := connectInMemoryWith(t, c)
			res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
				Name: setCardCustomFieldToolName,
				Arguments: map[string]any{
					"card_id":         "ci-1",
					"custom_field_id": "cf-x",
					"text":            "anything",
				},
			})
			if err := err; err != nil {
				t.Fatalf("err: %v", err)
			}
			if !res.IsError {
				t.Errorf("unsupported type %q must reject", deferred)
			}
			if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "cannot be set") {
				t.Errorf("strings.ToLower(serializedResponseString(t, res)) does not contain %q", "cannot be set")
			}
		})
	}
}

func TestMCP_SetCardCustomField_Members_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-mem", Type: "Members", Name: "Owners"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":                "ci-1",
			"custom_field_id":        "cf-mem",
			"add_member_user_ids":    []string{"u-1", "u-2"},
			"remove_member_user_ids": []string{"u-3"},
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"members":{"addUserIds":["u-1","u-2"],"removeUserIds":["u-3"]}`) {
		t.Errorf("Members writes travel in a `members` object of add/remove deltas: %q missing", `"members":{"addUserIds":["u-1","u-2"],"removeUserIds":["u-3"]}`)
	}
}

// Favro's Members custom field takes add/remove deltas, so an
// all-empty delta is a no-op request rather than a "clear the list"
// instruction. Pin that it fails before any HTTP call instead of
// sending a meaningless body.
func TestMCP_SetCardCustomField_Members_EmptyDeltaRejected(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, nil, []favro.CustomField{
		{CustomFieldID: "cf-mem", Type: "Members", Name: "Owners"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":             "ci-1",
			"custom_field_id":     "cf-mem",
			"add_member_user_ids": []string{},
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "at least one userid") {
		t.Errorf("strings.ToLower(serializedResponseString(t, res)) does not contain %q", "at least one userid")
	}
}

func TestMCP_SetCardCustomField_Status_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{
			CustomFieldID: "cf-st", Type: "Status", Name: "Phase",
			CustomFieldItems: []favro.CustomFieldItem{{CustomFieldItemID: "it-doing", Name: "Doing"}},
		},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-st",
			"status_item_id":  "it-doing",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"value":["it-doing"]`) {
		t.Errorf("Status writes put the item id in `value` as a single-element array: %q missing", `"value":["it-doing"]`)
	}
}

func TestMCP_SetCardCustomField_MultipleSelect_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-multi", Type: "Multiple select", Name: "Tags"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":               "ci-1",
			"custom_field_id":       "cf-multi",
			"multi_select_item_ids": []string{"it-a", "it-b", "it-c"},
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"value":["it-a","it-b","it-c"]`) {
		t.Errorf("body does not contain %q", `"value":["it-a","it-b","it-c"]`)
	}
}

func TestMCP_SetCardCustomField_Rating_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-rate", Type: "Rating", Name: "Quality"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-rate",
			"rating_value":    4,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"total":4`) {
		t.Errorf("Rating writes travel in `total`; Favro fixes the scale at 0-5: %q missing", `"total":4`)
	}
	if strings.Contains(body, `"value"`) {
		t.Errorf("body unexpectedly contains %q", `"value"`)
	}
}

// Favro documents Rating as an integer 0-5. Out-of-range values must
// fail before any HTTP call rather than being silently clamped.
func TestMCP_SetCardCustomField_Rating_OutOfRange(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, nil, []favro.CustomField{
		{CustomFieldID: "cf-rate", Type: "Rating", Name: "Quality"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-rate",
			"rating_value":    9,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "between 0 and 5") {
		t.Errorf("strings.ToLower(serializedResponseString(t, res)) does not contain %q", "between 0 and 5")
	}
}

func TestMCP_SetCardCustomField_Link_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-link", Type: "Link", Name: "Doc"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-link",
			"link_url":        "https://example.com/spec",
			"link_text":       "Spec",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"link":{"url":"https://example.com/spec","text":"Spec"}`) {
		t.Errorf("Link writes travel in a `link` object: %q missing", `"link":{"url":"https://example.com/spec","text":"Spec"}`)
	}
}

// Link without link_text omits the field entirely — pin so a
// future omitempty regression doesn't sneak through.
func TestMCP_SetCardCustomField_Link_NoLinkText(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-link", Type: "Link", Name: "Doc"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-link",
			"link_url":        "https://example.com/spec",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
	if !strings.Contains(body, `"link":{"url":"https://example.com/spec"}`) {
		t.Errorf("body does not contain %q", `"link":{"url":"https://example.com/spec"}`)
	}
	if strings.Contains(body, `"text"`) {
		t.Errorf("link text must be omitted when link_text is empty: %q present", `"text"`)
	}
}

// TestMCP_SetCardCustomField_UnknownFieldID pins the
// custom-field-not-found error path. The tool should NOT silently
// proceed with an unresolved field.
func TestMCP_SetCardCustomField_UnknownFieldID(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, nil, []favro.CustomField{
		{CustomFieldID: "cf-other", Type: "Text", Name: "Other"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-missing",
			"text":            "x",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "not found") {
		t.Errorf("strings.ToLower(serializedResponseString(t, res)) does not contain %q", "not found")
	}
}

func TestMCP_SetCardCustomField_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	cases := []string{"card_id", "custom_field_id"}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, setCardCustomFieldToolName, field)
		})
	}
}

func TestMCP_SetCardCustomField_Tags_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-tags", Type: "Tags", Name: "Areas"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-tags",
			"add_tag_ids":     []string{"t-1"},
			"remove_tag_ids":  []string{"t-2"},
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"tags":{"addTagIds":["t-1"],"removeTagIds":["t-2"]}`) {
		t.Errorf("body does not contain %q", `"tags":{"addTagIds":["t-1"],"removeTagIds":["t-2"]}`)
	}
	// Only the by-id forms are exposed: Favro's addTags takes names and
	// creates unknown ones, which is the typo foot-gun the card-level
	// tag tools hard-fail to prevent.
	if strings.Contains(body, `"addTags"`) {
		t.Errorf("body unexpectedly contains %q", `"addTags"`)
	}
}

func TestMCP_SetCardCustomField_Timeline_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-tl", Type: "Timeline", Name: "Window"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":             "ci-1",
			"custom_field_id":     "cf-tl",
			"timeline_start_date": "2026-01-01T00:00:00Z",
			"timeline_due_date":   "2026-02-01T00:00:00Z",
			"timeline_show_time":  true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"timeline":{"startDate":"2026-01-01T00:00:00Z","dueDate":"2026-02-01T00:00:00Z","showTime":true}`) {
		t.Errorf("body does not contain %q", `"timeline":{"startDate":"2026-01-01T00:00:00Z","dueDate":"2026-02-01T00:00:00Z","showTime":true}`)
	}
}

// Favro requires both timeline bounds. A half-set Timeline must fail
// before any HTTP call rather than writing a partial window.
func TestMCP_SetCardCustomField_Timeline_MissingBound(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, nil, []favro.CustomField{
		{CustomFieldID: "cf-tl", Type: "Timeline", Name: "Window"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":             "ci-1",
			"custom_field_id":     "cf-tl",
			"timeline_start_date": "2026-01-01T00:00:00Z",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "both required") {
		t.Errorf("strings.ToLower(serializedResponseString(t, res)) does not contain %q", "both required")
	}
}

func TestMCP_SetCardCustomField_Vote_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-vote", Type: "Vote", Name: "Wanted"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-vote",
			"vote":            true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"value":true`) {
		t.Errorf("body does not contain %q", `"value":true`)
	}
}

// "Voting" is the spelling this client used before the write contract
// was re-checked against Favro's docs, which call the type "Vote".
// Both must route to the same applicator.
func TestMCP_SetCardCustomField_Vote_LegacyTypeSpelling(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-vote", Type: "Voting", Name: "Wanted"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-vote",
			"vote":            false,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"value":false`) {
		t.Errorf("body does not contain %q", `"value":false`)
	}
}

func TestMCP_SetCardCustomField_Color_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-color", Type: "Color", Name: "Card color"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-color",
			"color":           "blue-300",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"color":"blue-300"`) {
		t.Errorf("body does not contain %q", `"color":"blue-300"`)
	}
}

// Favro clears a Color field on an empty string, but an empty string
// is indistinguishable from an omitted input — hence the sentinel.
func TestMCP_SetCardCustomField_Color_ClearSentinel(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-color", Type: "Color", Name: "Card color"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-color",
			"color":           colorClearSentinel,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"value":""`) {
		t.Errorf("body does not contain %q", `"value":""`)
	}
	if strings.Contains(body, `"color"`) {
		t.Errorf("body unexpectedly contains %q", `"color"`)
	}
}

func TestMCP_SetCardCustomField_Time_HappyPath(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-time", Type: "Time", Name: "Logged"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":                 "ci-1",
			"custom_field_id":         "cf-time",
			"time_report_ms":          50400000,
			"time_report_description": "pairing",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}
	if !strings.Contains(body, `"addUserReports":[{"value":50400000,"description":"pairing"}]`) {
		t.Errorf("body does not contain %q", `"addUserReports":[{"value":50400000,"description":"pairing"}]`)
	}
}

// verifiableCustomFieldFixture routes by path rather than by method,
// because the read-back these tests are about is a GET too: fields
// come from /customfields, the card comes from /cards/{id}.
// cardFields is what the card carries when it is read back after the
// write.
func verifiableCustomFieldFixture(t *testing.T, fields []favro.CustomField, cardFields []favro.CardCustomFieldValue, gets *atomic.Int32) *favroapi.Client {
	t.Helper()
	return favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut:
			// A write Favro discarded and a write Favro applied come
			// back as the same 200 with the same body.
			_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"x"}`))
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			if gets != nil {
				gets.Add(1)
			}
			_ = json.NewEncoder(w).Encode(favro.Card{
				CardID:             "ci-1",
				CardCommonID:       "cc-1",
				WidgetCommonID:     "w-1",
				CustomFieldsValues: cardFields,
			})
		default:
			_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.CustomField]{Pages: 1, Entities: fields})
		}
	}))
}

// TestMCP_SetCardCustomField_NotOnCard_Reports is the case the tool
// description already warned about in prose: Favro accepts and
// discards a write to a field the card's widget has not enabled, and
// answers 200 either way.
func TestMCP_SetCardCustomField_NotOnCard_Reports(t *testing.T) {
	t.Parallel()

	c := verifiableCustomFieldFixture(t,
		[]favro.CustomField{{CustomFieldID: "cf-text", Type: "Text", Name: "Notes"}},
		nil, nil)

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-text",
			"text":            "hello",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("a discarded write is information, not a failed call: res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	notes := strings.Join(out.Notes, " ")
	if !strings.Contains(notes, "is not on the card after the write") {
		t.Errorf("out.Notes must say the write did not land: %v", out.Notes)
	}
	if !strings.Contains(notes, "not enabled") {
		t.Errorf("out.Notes must name the usual cause: %q missing from %v", "not enabled", out.Notes)
	}
}

// TestMCP_SetCardCustomField_OnCard_ReportsValue: the value the card
// carries after the write is what the caller needs to compare
// against, since Favro normalises some of them on the way in.
func TestMCP_SetCardCustomField_OnCard_ReportsValue(t *testing.T) {
	t.Parallel()

	c := verifiableCustomFieldFixture(t,
		[]favro.CustomField{{CustomFieldID: "cf-text", Type: "Text", Name: "Notes"}},
		[]favro.CardCustomFieldValue{{CustomFieldID: "cf-text", Value: json.RawMessage(`"hello"`)}}, nil)

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-text",
			"text":            "hello",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	notes := strings.Join(out.Notes, " ")
	if !strings.Contains(notes, "verified by reading the card back") {
		t.Errorf("out.Notes must record that the read happened: %v", out.Notes)
	}
	if !strings.Contains(notes, `"hello"`) {
		t.Errorf("out.Notes must carry the value the card now holds: %v", out.Notes)
	}
}

func TestMCP_SetCardCustomField_SkipVerify_DoesNotReadCard(t *testing.T) {
	t.Parallel()

	var cardGets atomic.Int32
	c := verifiableCustomFieldFixture(t,
		[]favro.CustomField{{CustomFieldID: "cf-text", Type: "Text", Name: "Notes"}},
		nil, &cardGets)

	cs := connectInMemoryWith(t, c)
	if _, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-text",
			"text":            "hello",
			"skip_verify":     true,
		},
	}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := cardGets.Load(); got != 0 {
		t.Errorf("skip_verify must drop the read-back: card reads = %v, want %v", got, 0)
	}
}

// TestMCP_SetCardCustomField_DryRun_DoesNotVerify is the no-op case:
// nothing was written, so there is nothing to read back.
func TestMCP_SetCardCustomField_DryRun_DoesNotVerify(t *testing.T) {
	t.Parallel()

	var cardGets atomic.Int32
	c := verifiableCustomFieldFixture(t,
		[]favro.CustomField{{CustomFieldID: "cf-text", Type: "Text", Name: "Notes"}},
		nil, &cardGets)

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-text",
			"text":            "hello",
			"dry_run":         true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if got := cardGets.Load(); got != 0 {
		t.Errorf("dry-run wrote nothing, so it verifies nothing: card reads = %v, want %v", got, 0)
	}
	if len(out.Notes) != 0 {
		t.Errorf("out.Notes = %v, want empty", out.Notes)
	}
}
