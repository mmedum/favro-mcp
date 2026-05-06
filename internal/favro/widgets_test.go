package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListWidgets_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Widget]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-w",
			Entities: []Widget{
				{WidgetCommonID: "w-1", Name: "Engineering Backlog", Type: "backlog", Color: "blue"},
				{WidgetCommonID: "w-2", Name: "Sprint Board", Type: "board", CollectionIDs: []string{"c-1", "c-2"}},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListWidgets(context.Background(), 0, "", ListWidgetsFilter{})
	require.NoError(t, err)
	require.Equal(t, "req-w", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Engineering Backlog", env.Entities[0].Name)
	require.Equal(t, "backlog", env.Entities[0].Type)
	require.ElementsMatch(t, []string{"c-1", "c-2"}, env.Entities[1].CollectionIDs)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/widgets", rec[0].Path)
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page=")
	require.Empty(t, rec[0].Query.Get("collection"), "empty collectionID must NOT add ?collection=")
}

func TestListWidgets_FiltersByCollection(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Widget]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListWidgets(context.Background(), 0, "", ListWidgetsFilter{CollectionID: "c-xyz"})
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "c-xyz", rec[0].Query.Get("collectionId"),
		"non-empty collectionID must be sent as ?collectionId= (Favro's actual filter parameter, verified live)")
}

func TestListWidgets_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Widget]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListWidgets(context.Background(), 2, "req-prior", ListWidgetsFilter{})
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Widget{
			WidgetCommonID: "w-zzz",
			Name:           "Looked Up",
			Type:           "board",
			CollectionIDs:  []string{"c-only"},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	w, err := c.GetWidget(context.Background(), "w-zzz")
	require.NoError(t, err)
	require.Equal(t, "w-zzz", w.WidgetCommonID)
	require.Equal(t, "board", w.Type)

	rec := h.seen()
	require.Equal(t, "/widgets/w-zzz", rec[0].Path)
}

func TestGetWidget_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetWidget(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

// TestListWidgets_ArchivedFilterForwarded pins that ListWidgetsFilter.Archived
// becomes ?archived=true on the wire, and that the new response
// fields (organizationId, archived, lanes, columns summary) decode.
func TestListWidgets_ArchivedAndNewFields(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[{
			"widgetCommonId":"w-1","name":"Sprint",
			"organizationId":"org-1","archived":true,
			"lanes":[{"laneId":"lane-1","name":"Frontend"}],
			"columns":[
				{"columnId":"col-1","name":"To do","color":"gray"},
				{"columnId":"col-2","name":"Done","color":"green"}
			]
		}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListWidgets(context.Background(), 0, "", ListWidgetsFilter{Archived: true})
	require.NoError(t, err)
	require.Equal(t, "true", h.seen()[0].Query.Get("archived"))
	require.Len(t, env.Entities, 1)
	w := env.Entities[0]
	require.Equal(t, "org-1", w.OrganizationID)
	require.True(t, w.Archived)
	require.Len(t, w.Lanes, 1)
	require.Equal(t, "Frontend", w.Lanes[0].Name)
	require.Len(t, w.Columns, 2)
	require.Equal(t, "Done", w.Columns[1].Name)
	require.Equal(t, "green", w.Columns[1].Color)
}

func TestGetWidget_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetWidget(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}

// TestCreateWidget_HappyPath pins POST /widgets — collectionId +
// name + type + color, response decoded back as Widget.
func TestCreateWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/widgets", rec.Path)
		require.JSONEq(t, `{"collectionId":"c-1","name":"Sprint","type":"backlog","color":"blue"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-new","name":"Sprint","type":"backlog","color":"blue","collectionIds":["c-1"]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateWidget(context.Background(), CreateWidgetRequest{
		CollectionID: "c-1",
		Name:         "Sprint",
		Type:         "backlog",
		Color:        "blue",
	})
	require.NoError(t, err)
	require.Equal(t, "w-new", got.WidgetCommonID)
}

func TestCreateWidget_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  CreateWidgetRequest
	}{
		{"missing collection_id", CreateWidgetRequest{Name: "x"}},
		{"missing name", CreateWidgetRequest{CollectionID: "c-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			c := newTestClient(srv)

			_, err := c.CreateWidget(context.Background(), tc.req)
			require.Error(t, err)
			require.Empty(t, h.seen())
		})
	}
}

func TestCreateWidget_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateWidget(WithDryRun(context.Background()), CreateWidgetRequest{CollectionID: "c-1", Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
}

func TestUpdateWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/widgets/w-1", rec.Path)
		require.JSONEq(t, `{"name":"renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-1","name":"renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateWidget(context.Background(), "w-1", UpdateWidgetRequest{Name: "renamed"})
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
}

func TestUpdateWidget_ArchiveTrue(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.JSONEq(t, `{"archive":true}`, rec.Body,
			"&true must marshal as archive:true; *bool keeps the explicit boolean from being omitempty-elided")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-1","archived":true}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	archive := true
	got, err := c.UpdateWidget(context.Background(), "w-1", UpdateWidgetRequest{Archive: &archive})
	require.NoError(t, err)
	require.True(t, got.Archived)
}

func TestUpdateWidget_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateWidget(context.Background(), "", UpdateWidgetRequest{Name: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestDeleteWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		require.Equal(t, "/widgets/w-1", rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.NoError(t, c.DeleteWidget(context.Background(), "w-1"))
}

func TestDeleteWidget_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.ErrorIs(t, c.DeleteWidget(context.Background(), ""), errMissingID)
	require.Empty(t, h.seen())
}
