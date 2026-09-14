package tools

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestMCP_ListCustomFields_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.CustomField]{
			Limit:     100,
			Page:      0,
			Pages:     2,
			RequestID: "req-cf",
			Entities: []favro.CustomField{
				{
					CustomFieldID: "cf-select",
					Type:          "Single select",
					Name:          "QA",
					Enabled:       true,
					CustomFieldItems: []favro.CustomFieldItem{
						{CustomFieldItemID: "i-1", Name: "ready", Color: "green"},
					},
				},
			},
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listCustomFieldsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[listOutput[favro.CustomField]](t, res)
	if len(out.Items) != 1 {
		t.Fatalf("len(out.Items) = %d, want 1", len(out.Items))
	}
	if got := out.Items[0].Type; got != "Single select" {
		t.Errorf("out.Items[0].Type = %v, want %v", got, "Single select")
	}
	if got := out.Items[0].Name; got != "QA" {
		t.Errorf("out.Items[0].Name = %v, want %v", got, "QA")
	}
	if len(out.Items[0].CustomFieldItems) != 1 {
		t.Fatalf("len(out.Items[0].CustomFieldItems) = %d, want 1", len(out.Items[0].CustomFieldItems))
	}
	if got := out.Items[0].CustomFieldItems[0].Name; got != "ready" {
		t.Errorf("out.Items[0].CustomFieldItems[0].Name = %v, want %v", got, "ready")
	}
	if out.NextPage == nil {
		t.Fatal("out.NextPage is nil")
	}
	if got := *out.NextPage; got != 2 {
		t.Errorf("*out.NextPage = %v, want %v", got, 2)
	}
}

func TestMCP_GetCustomField_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/customfields/cf-zzz" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.CustomField{
			CustomFieldID: "cf-zzz",
			Type:          "Text",
			Name:          "looked up",
			Enabled:       true,
		})
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: getCustomFieldToolName,
		Arguments: map[string]any{
			"custom_field_id": "cf-zzz",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[favro.CustomField](t, res)
	if got := out.CustomFieldID; got != "cf-zzz" {
		t.Errorf("out.CustomFieldID = %v, want %v", got, "cf-zzz")
	}
	if got := out.Type; got != "Text" {
		t.Errorf("out.Type = %v, want %v", got, "Text")
	}
	if len(out.CustomFieldItems) != 0 {
		t.Errorf("primitive types must NOT carry items: got %v", out.CustomFieldItems)
	}
}

func TestMCP_GetCustomField_MissingID_ReturnsToolError(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, getCustomFieldToolName, "custom_field_id")
}
