package favroapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListCardActivities_ForwardsWindow(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodGet {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodGet)
		}
		if got := rec.Path; got != "/cards/ci-1/activities" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1/activities")
		}
		if got := rec.Query.Get("since"); got != "2026-01-01T00:00:00Z" {
			t.Errorf("rec.Query.Get(\"since\") = %v, want %v", got, "2026-01-01T00:00:00Z")
		}
		if got := rec.Query.Get("until"); got != "2026-02-01T00:00:00Z" {
			t.Errorf("rec.Query.Get(\"until\") = %v, want %v", got, "2026-02-01T00:00:00Z")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"entities":[{
			"type":"assigned","source":"follow","cardId":"ci-1",
			"cardName":"This is a card","time":"2026-01-15T06:27:12.466Z",
			"byUserId":"u-1"
		}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCardActivities(context.Background(), 0, "", "ci-1", favro.ListActivitiesFilter{
		Since: "2026-01-01T00:00:00Z",
		Until: "2026-02-01T00:00:00Z",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	if got := env.Entities[0].Type; got != "assigned" {
		t.Errorf("env.Entities[0].Type = %v, want %v", got, "assigned")
	}
	if got := env.Entities[0].ByUserID; got != "u-1" {
		t.Errorf("env.Entities[0].ByUserID = %v, want %v", got, "u-1")
	}
}

// An unset window must leave both query parameters off entirely
// rather than sending empty strings Favro would try to parse.
func TestListCardActivities_OmitsEmptyWindow(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if rec.Query.Has("since") {
			t.Error("rec.Query.Has(\"since\") = true, want false")
		}
		if rec.Query.Has("until") {
			t.Error("rec.Query.Has(\"until\") = true, want false")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"entities":[]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCardActivities(context.Background(), 0, "", "ci-1", favro.ListActivitiesFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}

// Favro's field table says cardCommonId while its example payload
// says cardCommonKey. CommonID() reconciles them so callers don't
// have to know which one arrived.
func TestActivity_CommonID_ReconcilesBothWireKeys(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"entities":[
			{"type":"a","cardCommonKey":"cc-from-example"},
			{"type":"b","cardCommonId":"cc-from-table"}
		]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListCardActivities(context.Background(), 0, "", "ci-1", favro.ListActivitiesFilter{})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.Entities[0].CommonID(); got != "cc-from-example" {
		t.Errorf("env.Entities[0].CommonID() = %v, want %v", got, "cc-from-example")
	}
	if got := env.Entities[1].CommonID(); got != "cc-from-table" {
		t.Errorf("env.Entities[1].CommonID() = %v, want %v", got, "cc-from-table")
	}
}

func TestListCardActivities_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCardActivities(context.Background(), 0, "", "", favro.ListActivitiesFilter{})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}
