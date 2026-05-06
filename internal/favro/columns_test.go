package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListColumns_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Column]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-c",
			Entities: []Column{
				{ColumnID: "col-1", WidgetCommonID: "w-1", Name: "Backlog", Position: 0, CardCount: 12},
				{ColumnID: "col-2", WidgetCommonID: "w-1", Name: "Doing", Position: 1, CardCount: 3, TimeSum: 7200000, EstimationSum: 13},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListColumns(context.Background(), 0, "", "w-1")
	require.NoError(t, err)
	require.Equal(t, "req-c", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Backlog", env.Entities[0].Name)
	require.Equal(t, 1, env.Entities[1].Position)
	require.Equal(t, 7200000, env.Entities[1].TimeSum)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/columns", rec[0].Path)
	require.Equal(t, "w-1", rec[0].Query.Get("widgetCommonId"))
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page=")
}

func TestListColumns_FiltersByWidget(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Column]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListColumns(context.Background(), 0, "", "w-xyz")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "w-xyz", rec[0].Query.Get("widgetCommonId"),
		"non-empty widgetCommonID must be sent as ?widgetCommonId= (matches Favro's camelCase convention)")
}

func TestListColumns_EmptyWidget_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListColumns(context.Background(), 0, "", "")
	require.ErrorIs(t, err, errMissingWidgetCommonID,
		"empty widgetCommonID must short-circuit (Favro 400s on unfiltered /columns; verified live)")
	require.Empty(t, h.seen(), "no HTTP call must be made for empty widgetCommonID")
}

func TestListColumns_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Column]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListColumns(context.Background(), 2, "req-prior", "w-1")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "w-1", rec[0].Query.Get("widgetCommonId"),
		"widgetCommonID must be re-sent on every paginated page (Favro does not carry filter state)")
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetColumn_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Column{
			ColumnID:       "col-zzz",
			WidgetCommonID: "w-1",
			Name:           "Done",
			Position:       4,
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	col, err := c.GetColumn(context.Background(), "col-zzz")
	require.NoError(t, err)
	require.Equal(t, "col-zzz", col.ColumnID)
	require.Equal(t, "Done", col.Name)
	require.Equal(t, 4, col.Position)

	rec := h.seen()
	require.Equal(t, "/columns/col-zzz", rec[0].Path)
}

func TestGetColumn_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetColumn(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestGetColumn_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetColumn(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}

// TestCreateColumn_HappyPath pins POST /columns — widgetCommonId +
// name + optional position.
func TestCreateColumn_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/columns", rec.Path)
		require.JSONEq(t, `{"widgetCommonId":"w-1","name":"In review","color":"yellow"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columnId":"col-new","widgetCommonId":"w-1","name":"In review","position":2}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateColumn(context.Background(), CreateColumnRequest{
		WidgetCommonID: "w-1",
		Name:           "In review",
		Color:          "yellow",
	})
	require.NoError(t, err)
	require.Equal(t, "col-new", got.ColumnID)
}

func TestCreateColumn_PositionZero_PreservedExplicitly(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.JSONEq(t, `{"widgetCommonId":"w-1","name":"x","position":0}`, rec.Body,
			"&0 must marshal as position:0; *int keeps the explicit zero from being omitempty-elided")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columnId":"c1","widgetCommonId":"w-1","name":"x","position":0}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	pos := 0
	_, err := c.CreateColumn(context.Background(), CreateColumnRequest{
		WidgetCommonID: "w-1",
		Name:           "x",
		Position:       &pos,
	})
	require.NoError(t, err)
}

func TestCreateColumn_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  CreateColumnRequest
	}{
		{"missing widget_common_id", CreateColumnRequest{Name: "x"}},
		{"missing name", CreateColumnRequest{WidgetCommonID: "w-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			c := newTestClient(srv)

			_, err := c.CreateColumn(context.Background(), tc.req)
			require.Error(t, err)
			require.Empty(t, h.seen())
		})
	}
}

func TestCreateColumn_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateColumn(WithDryRun(context.Background()), CreateColumnRequest{WidgetCommonID: "w-1", Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
}

func TestUpdateColumn_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/columns/col-1", rec.Path)
		require.JSONEq(t, `{"name":"renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columnId":"col-1","name":"renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateColumn(context.Background(), "col-1", UpdateColumnRequest{Name: "renamed"})
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
}

func TestUpdateColumn_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateColumn(context.Background(), "", UpdateColumnRequest{Name: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestDeleteColumn_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		require.Equal(t, "/columns/col-1", rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.NoError(t, c.DeleteColumn(context.Background(), "col-1"))
}

func TestDeleteColumn_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.ErrorIs(t, c.DeleteColumn(context.Background(), ""), errMissingID)
	require.Empty(t, h.seen())
}
