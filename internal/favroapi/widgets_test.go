package favroapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListWidgets_DefaultPage(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-w",
			Entities: []favro.Widget{
				{WidgetCommonID: "w-1", Name: "Engineering Backlog", Type: "backlog", Color: "blue"},
				{WidgetCommonID: "w-2", Name: "Sprint Board", Type: "board", CollectionIDs: []string{"c-1", "c-2"}},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListWidgets(context.Background(), 0, "", favro.ListWidgetsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-w" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-w")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Name; got != "Engineering Backlog" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "Engineering Backlog")
	}
	if got := env.Entities[0].Type; got != "backlog" {
		t.Errorf("env.Entities[0].Type = %v, want %v", got, "backlog")
	}
	gotIDs := slices.Clone(env.Entities[1].CollectionIDs)
	slices.Sort(gotIDs)
	if want := []string{"c-1", "c-2"}; !slices.Equal(gotIDs, want) {
		t.Errorf("CollectionIDs = %v, want %v in any order", env.Entities[1].CollectionIDs, want)
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/widgets" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/widgets")
	}
	if len(rec[0].Query.Get("page")) != 0 {
		t.Errorf("page=0 must NOT add ?page=: got %v", rec[0].Query.Get("page"))
	}
	if len(rec[0].Query.Get("collection")) != 0 {
		t.Errorf("empty collectionID must NOT add ?collection=: got %v", rec[0].Query.Get("collection"))
	}
}

func TestListWidgets_FiltersByCollection(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{Page: 0, Pages: 1})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListWidgets(context.Background(), 0, "", favro.ListWidgetsFilter{CollectionID: "c-xyz"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("collectionId"); got != "c-xyz" {
		t.Errorf("non-empty collectionID must be sent as ?collectionId= (Favro's actual filter parameter, verified live): got %v, want %v", got, "c-xyz")
	}
}

func TestListWidgets_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Widget]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListWidgets(context.Background(), 2, "req-prior", favro.ListWidgetsFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("page"); got != "2" {
		t.Errorf("rec[0].Query.Get(\"page\") = %v, want %v", got, "2")
	}
	if got := rec[0].Headers.Get(headerRequestID); got != "req-prior" {
		t.Errorf("rec[0].Headers.Get(headerRequestID) = %v, want %v", got, "req-prior")
	}
}

func TestGetWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Widget{
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := w.WidgetCommonID; got != "w-zzz" {
		t.Errorf("w.WidgetCommonID = %v, want %v", got, "w-zzz")
	}
	if got := w.Type; got != "board" {
		t.Errorf("w.Type = %v, want %v", got, "board")
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/widgets/w-zzz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/widgets/w-zzz")
	}
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
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestListWidgets_ArchivedFilterForwarded pins that favro.ListWidgetsFilter.Archived
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

	env, err := c.ListWidgets(context.Background(), 0, "", favro.ListWidgetsFilter{Archived: true})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := h.seen()[0].Query.Get("archived"); got != "true" {
		t.Errorf("h.seen()[0].Query.Get(\"archived\") = %v, want %v", got, "true")
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	w := env.Entities[0]
	if got := w.OrganizationID; got != "org-1" {
		t.Errorf("w.OrganizationID = %v, want %v", got, "org-1")
	}
	if !w.Archived {
		t.Error("w.Archived = false, want true")
	}
	if len(w.Lanes) != 1 {
		t.Fatalf("len(w.Lanes) = %d, want 1", len(w.Lanes))
	}
	if got := w.Lanes[0].Name; got != "Frontend" {
		t.Errorf("w.Lanes[0].Name = %v, want %v", got, "Frontend")
	}
	if len(w.Columns) != 2 {
		t.Fatalf("len(w.Columns) = %d, want 2", len(w.Columns))
	}
	if got := w.Columns[1].Name; got != "Done" {
		t.Errorf("w.Columns[1].Name = %v, want %v", got, "Done")
	}
	if got := w.Columns[1].Color; got != "green" {
		t.Errorf("w.Columns[1].Color = %v, want %v", got, "green")
	}
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
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestCreateWidget_HappyPath pins POST /widgets — collectionId +
// name + type + color, response decoded back as favro.Widget.
func TestCreateWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/widgets" {
			t.Errorf("rec.Path = %v, want %v", got, "/widgets")
		}
		requireJSONEq(t, `{"collectionId":"c-1","name":"Sprint","type":"backlog","color":"blue"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-new","name":"Sprint","type":"backlog","color":"blue","collectionIds":["c-1"]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateWidget(context.Background(), favro.CreateWidgetRequest{
		CollectionID: "c-1",
		Name:         "Sprint",
		Type:         "backlog",
		Color:        "blue",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.WidgetCommonID; got != "w-new" {
		t.Errorf("got.WidgetCommonID = %v, want %v", got, "w-new")
	}
}

func TestCreateWidget_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  favro.CreateWidgetRequest
	}{
		{"missing collection_id", favro.CreateWidgetRequest{Name: "x"}},
		{"missing name", favro.CreateWidgetRequest{CollectionID: "c-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			c := newTestClient(srv)

			_, err := c.CreateWidget(context.Background(), tc.req)
			if err == nil {
				t.Fatal("err should have failed")
			}
			if len(h.seen()) != 0 {
				t.Errorf("h.seen() = %v, want empty", h.seen())
			}
		})
	}
}

func TestCreateWidget_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateWidget(WithDryRun(context.Background()), favro.CreateWidgetRequest{CollectionID: "c-1", Name: "x"})
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

func TestUpdateWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/widgets/w-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/widgets/w-1")
		}
		requireJSONEq(t, `{"name":"renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-1","name":"renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateWidget(context.Background(), "w-1", favro.UpdateWidgetRequest{Name: "renamed"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "renamed" {
		t.Errorf("got.Name = %v, want %v", got, "renamed")
	}
}

func TestUpdateWidget_ArchiveTrue(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		requireJSONEq(t, `{"archive":true,"collectionId":"c-1"}`, rec.Body,
			"&true must marshal as archive:true; *bool keeps the explicit boolean from being omitempty-elided")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-1","archived":true}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	archive := true
	got, err := c.UpdateWidget(context.Background(), "w-1", favro.UpdateWidgetRequest{
		Archive:      &archive,
		CollectionID: "c-1",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !got.Archived {
		t.Error("got.Archived = false, want true")
	}
}

func TestUpdateWidget_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateWidget(context.Background(), "", favro.UpdateWidgetRequest{Name: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestDeleteWidget_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/widgets/w-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/widgets/w-1")
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteWidget(context.Background(), "w-1", ""); err != nil {
		t.Fatalf("c.DeleteWidget(context.Background(), \"w-1\", \"\"): %v", err)
	}
}

// A widget can live in several collections. Passing a collectionId
// scopes the delete to one of them; omitting it (above) deletes every
// instance.
func TestDeleteWidget_ScopedToCollection(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/widgets/w-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/widgets/w-1")
		}
		if got := rec.Query.Get("collectionId"); got != "c-1" {
			t.Errorf("rec.Query.Get(\"collectionId\") = %v, want %v", got, "c-1")
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteWidget(context.Background(), "w-1", "c-1"); err != nil {
		t.Fatalf("c.DeleteWidget(context.Background(), \"w-1\", \"c-1\"): %v", err)
	}
}

func TestDeleteWidget_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if !errors.Is(c.DeleteWidget(context.Background(), "", ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteWidget(context.Background(), "", ""))
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// Favro scopes a widget archive to a single collection, so an
// archive without collectionId must fail before any HTTP work rather
// than being rejected server-side with a vaguer message.
func TestUpdateWidget_ArchiveRequiresCollectionID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	archive := true
	_, err := c.UpdateWidget(context.Background(), "w-1", favro.UpdateWidgetRequest{Archive: &archive})
	if err == nil || !strings.Contains(err.Error(), "collection_id") {
		t.Fatalf("got %v, want it to mention %q", err, "collection_id")
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}

	_, err = c.UpdateWidget(context.Background(), "w-1", favro.UpdateWidgetRequest{
		Archive:      &archive,
		CollectionID: "c-1",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}
