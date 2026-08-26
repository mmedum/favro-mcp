package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// customFieldFixture wires a Favro server that:
//   - GET /customfields → returns the supplied [field] list
//   - PUT /cards/{id}    → echoes a Card back, optionally capturing
//     the request body in *capturedBody when non-nil
//
// Used by every test that exercises favro_set_card_custom_field.
func customFieldFixture(t *testing.T, capturedBody *string, fields []favro.CustomField) *favro.Client {
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.False(t, out.DryRun)
	require.Equal(t, "ci-1", out.Result.CardID)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"total":42`,
		"Number writes travel in `total`, not `value`")
	require.NotContains(t, body, `"value"`)
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
	require.NoError(t, err)
	require.False(t, res.IsError)
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
	require.NoError(t, err)
	require.False(t, res.IsError)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":["item-1"]`,
		"select-flavored writes put item ids in `value`")
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
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.Card]](t, res)
	require.True(t, out.DryRun)
	require.Contains(t, out.PredictedStateDiff, "Notes")
	require.Contains(t, out.PredictedStateDiff, "Text")
	require.EqualValues(t, 0, puts.Load(), "dry_run must short-circuit before any PUT")
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
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "number")
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
			require.NoError(t, err)
			require.True(t, res.IsError, "unsupported type %q must reject", deferred)
			require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "cannot be set")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"members":{"addUserIds":["u-1","u-2"],"removeUserIds":["u-3"]}`,
		"Members writes travel in a `members` object of add/remove deltas")
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
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "at least one userid")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":["it-doing"]`,
		"Status writes put the item id in `value` as a single-element array")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":["it-a","it-b","it-c"]`)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"total":4`,
		"Rating writes travel in `total`; Favro fixes the scale at 0-5")
	require.NotContains(t, body, `"value"`)
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
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "between 0 and 5")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"link":{"url":"https://example.com/spec","text":"Spec"}`,
		"Link writes travel in a `link` object")
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
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, body, `"link":{"url":"https://example.com/spec"}`)
	require.NotContains(t, body, `"text"`, "link text must be omitted when link_text is empty")
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
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "not found")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"tags":{"addTagIds":["t-1"],"removeTagIds":["t-2"]}`)
	// Only the by-id forms are exposed: Favro's addTags takes names and
	// creates unknown ones, which is the typo foot-gun the card-level
	// tag tools hard-fail to prevent.
	require.NotContains(t, body, `"addTags"`)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body,
		`"timeline":{"startDate":"2026-01-01T00:00:00Z","dueDate":"2026-02-01T00:00:00Z","showTime":true}`)
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
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "both required")
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":true`)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":false`)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"color":"blue-300"`)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":""`)
	require.NotContains(t, body, `"color"`)
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"addUserReports":[{"value":50400000,"description":"pairing"}]`)
}
