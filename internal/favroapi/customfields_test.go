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

func TestListCustomFields_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.CustomField]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cf",
			Entities: []favro.CustomField{
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
					CustomFieldItems: []favro.CustomFieldItem{
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-cf" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-cf")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Type; got != "Text" {
		t.Errorf("env.Entities[0].Type = %v, want %v", got, "Text")
	}
	if len(env.Entities[0].CustomFieldItems) != 0 {
		t.Errorf("primitive types must NOT carry items (omitempty drops the field): got %v", env.Entities[0].CustomFieldItems)
	}
	if got := env.Entities[1].Type; got != "Single select" {
		t.Errorf("env.Entities[1].Type = %v, want %v", got, "Single select")
	}
	if len(env.Entities[1].CustomFieldItems) != 2 {
		t.Fatalf("len(env.Entities[1].CustomFieldItems) = %d, want 2", len(env.Entities[1].CustomFieldItems))
	}
	if got := env.Entities[1].CustomFieldItems[0].Name; got != "ready" {
		t.Errorf("env.Entities[1].CustomFieldItems[0].Name = %v, want %v", got, "ready")
	}
	if got := env.Entities[1].CustomFieldItems[0].Color; got != "green" {
		t.Errorf("env.Entities[1].CustomFieldItems[0].Color = %v, want %v", got, "green")
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/customfields" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/customfields")
	}
	if len(rec[0].Query.Encode()) != 0 {
		t.Errorf("no filter or page query expected on first-page list: got %v", rec[0].Query.Encode())
	}
}

func TestListCustomFields_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.CustomField]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCustomFields(context.Background(), 2, "req-prior")
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

func TestGetCustomField_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.CustomField{
			CustomFieldID: "cf-zzz",
			Type:          "Single select",
			Name:          "looked up",
			Enabled:       true,
			CustomFieldItems: []favro.CustomFieldItem{
				{CustomFieldItemID: "i-only", Name: "only-option"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	cf, err := c.GetCustomField(context.Background(), "cf-zzz")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := cf.CustomFieldID; got != "cf-zzz" {
		t.Errorf("cf.CustomFieldID = %v, want %v", got, "cf-zzz")
	}
	if got := cf.Type; got != "Single select" {
		t.Errorf("cf.Type = %v, want %v", got, "Single select")
	}
	if len(cf.CustomFieldItems) != 1 {
		t.Fatalf("len(cf.CustomFieldItems) = %d, want 1", len(cf.CustomFieldItems))
	}
	if got := cf.CustomFieldItems[0].Name; got != "only-option" {
		t.Errorf("cf.CustomFieldItems[0].Name = %v, want %v", got, "only-option")
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/customfields/cf-zzz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/customfields/cf-zzz")
	}
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
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
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
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}
