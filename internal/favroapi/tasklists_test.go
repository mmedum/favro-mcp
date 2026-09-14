package favroapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListTasklists_ForwardsCardCommonID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Path; got != "/tasklists" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasklists")
		}
		if got := rec.Query.Get("cardCommonId"); got != "cc-1" {
			t.Errorf("rec.Query.Get(\"cardCommonId\") = %v, want %v", got, "cc-1")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"entities":[
			{"taskListId":"tl-1","cardCommonId":"cc-1","name":"Acceptance","position":0}
		]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListTasklists(context.Background(), 0, "", "cc-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	if got := env.Entities[0].Title(); got != "Acceptance" {
		t.Errorf("env.Entities[0].Title() = %v, want %v", got, "Acceptance")
	}
}

// Favro's field table calls the checklist title `name` while its
// example payloads use `description`. Title() reconciles the two so
// callers don't have to guess which one Favro sent.
func TestTasklist_Title_FallsBackToDescription(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"entities":[
			{"taskListId":"tl-1","description":"From the docs example"}
		]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListTasklists(context.Background(), 0, "", "cc-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities[0].Name) != 0 {
		t.Errorf("env.Entities[0].Name = %v, want empty", env.Entities[0].Name)
	}
	if got := env.Entities[0].Title(); got != "From the docs example" {
		t.Errorf("env.Entities[0].Title() = %v, want %v", got, "From the docs example")
	}
}

func TestListTasklists_RequiresCardCommonID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListTasklists(context.Background(), 0, "", "")
	if err == nil || !strings.Contains(err.Error(), "card_common_id is required") {
		t.Fatalf("got %v, want it to mention %q", err, "card_common_id is required")
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestGetTasklist_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Path; got != "/tasklists/tl-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasklists/tl-1")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskListId":"tl-1","name":"Acceptance"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.GetTasklist(context.Background(), "tl-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.TaskListID; got != "tl-1" {
		t.Errorf("got.TaskListID = %v, want %v", got, "tl-1")
	}
}

// Seeding items at creation time saves one round-trip per item, so
// pin that the tasks array reaches the wire.
func TestCreateTasklist_SeedsTasks(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/tasklists" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasklists")
		}
		requireJSONEq(t, `{
			"cardCommonId":"cc-1",
			"name":"Acceptance",
			"tasks":[{"name":"first"},{"name":"second","completed":true}]
		}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskListId":"tl-new","name":"Acceptance"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateTasklist(context.Background(), favro.CreateTasklistRequest{
		CardCommonID: "cc-1",
		Name:         "Acceptance",
		Tasks: []favro.CardTask{
			{Name: "first"},
			{Name: "second", Completed: true},
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.TaskListID; got != "tl-new" {
		t.Errorf("got.TaskListID = %v, want %v", got, "tl-new")
	}
}

func TestCreateTasklist_MissingFields_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateTasklist(context.Background(), favro.CreateTasklistRequest{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "card_common_id is required") {
		t.Fatalf("got %v, want it to mention %q", err, "card_common_id is required")
	}

	_, err = c.CreateTasklist(context.Background(), favro.CreateTasklistRequest{CardCommonID: "cc-1"})
	if err == nil || !strings.Contains(err.Error(), "tasklist name is required") {
		t.Fatalf("got %v, want it to mention %q", err, "tasklist name is required")
	}

	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestUpdateAndDeleteTasklist(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Path; got != "/tasklists/tl-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasklists/tl-1")
		}
		if rec.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		requireJSONEq(t, `{"name":"Renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskListId":"tl-1","name":"Renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateTasklist(context.Background(), "tl-1", favro.UpdateTasklistRequest{Name: "Renamed"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "Renamed" {
		t.Errorf("got.Name = %v, want %v", got, "Renamed")
	}

	if err := c.DeleteTasklist(context.Background(), "tl-1"); err != nil {
		t.Fatalf("c.DeleteTasklist(context.Background(), \"tl-1\"): %v", err)
	}

	_, err = c.UpdateTasklist(context.Background(), "", favro.UpdateTasklistRequest{Name: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if !errors.Is(c.DeleteTasklist(context.Background(), ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteTasklist(context.Background(), ""))
	}
}

func TestTasklistWrites_DryRun_NeverDispatch(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}
	ctx := WithDryRun(context.Background())

	_, err := c.CreateTasklist(ctx, favro.CreateTasklistRequest{CardCommonID: "cc-1", Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}

	_, err = c.UpdateTasklist(ctx, "tl-1", favro.UpdateTasklistRequest{Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}

	if !errors.Is(c.DeleteTasklist(ctx, "tl-1"), ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", c.DeleteTasklist(ctx, "tl-1"))
	}
}
