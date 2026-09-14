package favroapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListColumns_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-c",
			Entities: []favro.Column{
				{ColumnID: "col-1", WidgetCommonID: "w-1", Name: "Backlog", Position: 0, CardCount: 12},
				{ColumnID: "col-2", WidgetCommonID: "w-1", Name: "Doing", Position: 1, CardCount: 3, TimeSum: 7200000, EstimationSum: 13},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListColumns(context.Background(), 0, "", "w-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-c" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-c")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Name; got != "Backlog" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "Backlog")
	}
	if got := env.Entities[1].Position; got != 1 {
		t.Errorf("env.Entities[1].Position = %v, want %v", got, 1)
	}
	if got := env.Entities[1].TimeSum; got != 7200000 {
		t.Errorf("env.Entities[1].TimeSum = %v, want %v", got, 7200000)
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/columns" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/columns")
	}
	if got := rec[0].Query.Get("widgetCommonId"); got != "w-1" {
		t.Errorf("rec[0].Query.Get(\"widgetCommonId\") = %v, want %v", got, "w-1")
	}
	if len(rec[0].Query.Get("page")) != 0 {
		t.Errorf("page=0 must NOT add ?page=: got %v", rec[0].Query.Get("page"))
	}
}

func TestListColumns_FiltersByWidget(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListColumns(context.Background(), 0, "", "w-xyz")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("widgetCommonId"); got != "w-xyz" {
		t.Errorf("non-empty widgetCommonID must be sent as ?widgetCommonId= (matches Favro's camelCase convention): got %v, want %v", got, "w-xyz")
	}
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
	if !errors.Is(err, errMissingWidgetCommonID) {
		t.Fatalf("got %v, want errMissingWidgetCommonID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("no HTTP call must be made for empty widgetCommonID: got %v", h.seen())
	}
}

func TestListColumns_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Column]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListColumns(context.Background(), 2, "req-prior", "w-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("page"); got != "2" {
		t.Errorf("rec[0].Query.Get(\"page\") = %v, want %v", got, "2")
	}
	if got := rec[0].Query.Get("widgetCommonId"); got != "w-1" {
		t.Errorf("widgetCommonID must be re-sent on every paginated page (Favro does not carry filter state): got %v, want %v", got, "w-1")
	}
	if got := rec[0].Headers.Get(headerRequestID); got != "req-prior" {
		t.Errorf("rec[0].Headers.Get(headerRequestID) = %v, want %v", got, "req-prior")
	}
}

func TestGetColumn_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Column{
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := col.ColumnID; got != "col-zzz" {
		t.Errorf("col.ColumnID = %v, want %v", got, "col-zzz")
	}
	if got := col.Name; got != "Done" {
		t.Errorf("col.Name = %v, want %v", got, "Done")
	}
	if got := col.Position; got != 4 {
		t.Errorf("col.Position = %v, want %v", got, 4)
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/columns/col-zzz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/columns/col-zzz")
	}
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
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
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
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestCreateColumn_HappyPath pins POST /columns — widgetCommonId +
// name + optional position.
func TestCreateColumn_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/columns" {
			t.Errorf("rec.Path = %v, want %v", got, "/columns")
		}
		requireJSONEq(t, `{"widgetCommonId":"w-1","name":"In review","color":"yellow"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columnId":"col-new","widgetCommonId":"w-1","name":"In review","position":2}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateColumn(context.Background(), favro.CreateColumnRequest{
		WidgetCommonID: "w-1",
		Name:           "In review",
		Color:          "yellow",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.ColumnID; got != "col-new" {
		t.Errorf("got.ColumnID = %v, want %v", got, "col-new")
	}
}

func TestCreateColumn_PositionZero_PreservedExplicitly(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		requireJSONEq(t, `{"widgetCommonId":"w-1","name":"x","position":0}`, rec.Body,
			"&0 must marshal as position:0; *int keeps the explicit zero from being omitempty-elided")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columnId":"c1","widgetCommonId":"w-1","name":"x","position":0}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	pos := 0
	_, err := c.CreateColumn(context.Background(), favro.CreateColumnRequest{
		WidgetCommonID: "w-1",
		Name:           "x",
		Position:       &pos,
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestCreateColumn_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  favro.CreateColumnRequest
	}{
		{"missing widget_common_id", favro.CreateColumnRequest{Name: "x"}},
		{"missing name", favro.CreateColumnRequest{WidgetCommonID: "w-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			c := newTestClient(srv)

			_, err := c.CreateColumn(context.Background(), tc.req)
			if err == nil {
				t.Fatal("err should have failed")
			}
			if len(h.seen()) != 0 {
				t.Errorf("h.seen() = %v, want empty", h.seen())
			}
		})
	}
}

func TestCreateColumn_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateColumn(WithDryRun(context.Background()), favro.CreateColumnRequest{WidgetCommonID: "w-1", Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodPost {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
	}
}

func TestUpdateColumn_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/columns/col-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/columns/col-1")
		}
		requireJSONEq(t, `{"name":"renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columnId":"col-1","name":"renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateColumn(context.Background(), "col-1", favro.UpdateColumnRequest{Name: "renamed"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "renamed" {
		t.Errorf("got.Name = %v, want %v", got, "renamed")
	}
}

func TestUpdateColumn_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateColumn(context.Background(), "", favro.UpdateColumnRequest{Name: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestDeleteColumn_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/columns/col-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/columns/col-1")
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteColumn(context.Background(), "col-1"); err != nil {
		t.Fatalf("c.DeleteColumn(context.Background(), \"col-1\"): %v", err)
	}
}

func TestDeleteColumn_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if !errors.Is(c.DeleteColumn(context.Background(), ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteColumn(context.Background(), ""))
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}
