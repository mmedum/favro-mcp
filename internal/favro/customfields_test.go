package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListCustomFields_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[CustomField]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cf",
			Entities: []CustomField{
				// Type values use the title-case-with-spaces form
				// Favro actually returns (verified live), so the
				// doc-comment's vocabulary claim is anchored here.
				{
					CustomFieldID: "cf-text",
					Type:          "Text",
					Name:          "Notes",
					Enabled:       true,
				},
				{
					CustomFieldID: "cf-select",
					Type:          "Single select",
					Name:          "QA",
					Enabled:       true,
					CustomFieldItems: []CustomFieldItem{
						{CustomFieldItemID: "i-1", Name: "ready", Color: "green"},
						{CustomFieldItemID: "i-2", Name: "blocked", Color: "red"},
					},
				},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCustomFields(context.Background(), 0, "")
	require.NoError(t, err)
	require.Equal(t, "req-cf", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Text", env.Entities[0].Type)
	require.Empty(t, env.Entities[0].CustomFieldItems,
		"primitive types must NOT carry items (omitempty drops the field)")
	require.Equal(t, "Single select", env.Entities[1].Type)
	require.Len(t, env.Entities[1].CustomFieldItems, 2)
	require.Equal(t, "ready", env.Entities[1].CustomFieldItems[0].Name)
	require.Equal(t, "green", env.Entities[1].CustomFieldItems[0].Color)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/customfields", rec[0].Path)
	require.Empty(t, rec[0].Query.Encode(), "no filter or page query expected on first-page list")
}

func TestListCustomFields_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[CustomField]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCustomFields(context.Background(), 2, "req-prior")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetCustomField_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CustomField{
			CustomFieldID: "cf-zzz",
			Type:          "Single select",
			Name:          "looked up",
			Enabled:       true,
			CustomFieldItems: []CustomFieldItem{
				{CustomFieldItemID: "i-only", Name: "only-option"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	cf, err := c.GetCustomField(context.Background(), "cf-zzz")
	require.NoError(t, err)
	require.Equal(t, "cf-zzz", cf.CustomFieldID)
	require.Equal(t, "Single select", cf.Type)
	require.Len(t, cf.CustomFieldItems, 1)
	require.Equal(t, "only-option", cf.CustomFieldItems[0].Name)

	rec := h.seen()
	require.Equal(t, "/customfields/cf-zzz", rec[0].Path)
}

func TestGetCustomField_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetCustomField(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestGetCustomField_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetCustomField(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}
