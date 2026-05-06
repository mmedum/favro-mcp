package server

import (
	"encoding/json"
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
//   - PUT /cards/{id}    → echoes a Card back
//
// Used by every test that exercises favro_set_card_custom_field.
func customFieldFixture(t *testing.T, fields []favro.CustomField) *favro.Client {
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
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cardId":"ci-1","cardCommonId":"cc-1","name":"x"}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
}

func TestMCP_SetCardCustomField_Text_HappyPath(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, []favro.CustomField{
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

	c := customFieldFixture(t, []favro.CustomField{
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

	c := customFieldFixture(t, []favro.CustomField{
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

	c := customFieldFixture(t, []favro.CustomField{
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

	c := customFieldFixture(t, []favro.CustomField{
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

	c := customFieldFixture(t, []favro.CustomField{
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

// TestMCP_SetCardCustomField_LongTailType pins that long-tail
// types (Members, Status, Multi-select, Rating, Link, etc.) are
// rejected with a typed error pointing the caller to Phase 7.
func TestMCP_SetCardCustomField_LongTailType(t *testing.T) {
	t.Parallel()

	cases := []string{"Members", "Status", "Multiple select", "Rating", "Link", "Tags"}
	for _, longTail := range cases {
		t.Run(longTail, func(t *testing.T) {
			t.Parallel()

			c := customFieldFixture(t, []favro.CustomField{
				{CustomFieldID: "cf-x", Type: longTail, Name: "x"},
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
			require.True(t, res.IsError, "long-tail type %q must reject", longTail)
			require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "phase 7")
		})
	}
}

// TestMCP_SetCardCustomField_UnknownFieldID pins the
// custom-field-not-found error path. The tool should NOT silently
// proceed with an unresolved field.
func TestMCP_SetCardCustomField_UnknownFieldID(t *testing.T) {
	t.Parallel()

	c := customFieldFixture(t, []favro.CustomField{
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
