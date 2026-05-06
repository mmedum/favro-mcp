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

	c := customFieldFixture(t, nil, []favro.CustomField{
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
	require.False(t, res.IsError)
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

	c := customFieldFixture(t, nil, []favro.CustomField{
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
	require.False(t, res.IsError)
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

// TestMCP_SetCardCustomField_StillDeferredType pins that types
// outside the dispatch table (Tags, Timeline, Voting, Progress,
// Relations, Sequential ID, Date created) reject with a typed
// long-tail error rather than silently falling through to one of
// the supported applicators.
func TestMCP_SetCardCustomField_StillDeferredType(t *testing.T) {
	t.Parallel()

	cases := []string{"Tags", "Timeline", "Voting", "Progress", "Relations", "Sequential ID", "Date created"}
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
			require.True(t, res.IsError, "still-deferred type %q must reject", deferred)
			require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "not supported")
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
			"card_id":         "ci-1",
			"custom_field_id": "cf-mem",
			"member_user_ids": []string{"u-1", "u-2"},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":["u-1","u-2"]`,
		"Members write must send value as a JSON array of userIds")
}

// Members with an empty (non-nil) array clears the list — distinct
// from omitting the field. Pins that the dispatch table treats
// `member_user_ids: []` as "set to empty" rather than "unset".
func TestMCP_SetCardCustomField_Members_EmptyArrayClears(t *testing.T) {
	t.Parallel()

	var body string
	c := customFieldFixture(t, &body, []favro.CustomField{
		{CustomFieldID: "cf-mem", Type: "Members", Name: "Owners"},
	})

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: setCardCustomFieldToolName,
		Arguments: map[string]any{
			"card_id":         "ci-1",
			"custom_field_id": "cf-mem",
			"member_user_ids": []string{},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":[]`,
		"empty member_user_ids must serialize as a JSON empty array (clear members)")
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
	require.Contains(t, body, `"customFieldItemIds":["it-doing"]`,
		"Status write must send customFieldItemIds as a single-element array")
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
	require.Contains(t, body, `"customFieldItemIds":["it-a","it-b","it-c"]`)
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
			"rating_total":    5,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))
	require.Contains(t, body, `"value":4`)
	require.Contains(t, body, `"total":5`)
}

// Rating without rating_total must fail before any HTTP call —
// half-set Rating writes are a known foot-gun.
func TestMCP_SetCardCustomField_Rating_MissingTotal(t *testing.T) {
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
			"rating_value":    4,
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "rating_total")
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
	require.Contains(t, body, `"value":"https://example.com/spec"`)
	require.Contains(t, body, `"linkText":"Spec"`)
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
	require.NotContains(t, body, "linkText", "linkText must be omitted when link_text is empty")
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
