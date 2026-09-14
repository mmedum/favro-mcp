package favroapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListTasks_ForwardsFilters(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodGet {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodGet)
		}
		if got := rec.Path; got != "/tasks" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasks")
		}
		if got := rec.Query.Get("cardCommonId"); got != "cc-1" {
			t.Errorf("rec.Query.Get(\"cardCommonId\") = %v, want %v", got, "cc-1")
		}
		if got := rec.Query.Get("taskListId"); got != "tl-1" {
			t.Errorf("rec.Query.Get(\"taskListId\") = %v, want %v", got, "tl-1")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[
			{"taskId":"t-1","taskListId":"tl-1","cardCommonId":"cc-1","name":"do it","completed":false,"position":0}
		]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListTasks(context.Background(), 0, "", favro.ListTasksFilter{
		CardCommonID: "cc-1",
		TaskListID:   "tl-1",
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	if got := env.Entities[0].TaskID; got != "t-1" {
		t.Errorf("env.Entities[0].TaskID = %v, want %v", got, "t-1")
	}
	if got := env.Entities[0].Name; got != "do it" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "do it")
	}
}

// Favro rejects an unscoped /tasks listing. Catching it client-side
// gives a clearer message and costs no rate-limit budget.
func TestListTasks_RequiresCardCommonID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListTasks(context.Background(), 0, "", favro.ListTasksFilter{})
	if err == nil || !strings.Contains(err.Error(), "card_common_id is required") {
		t.Fatalf("got %v, want it to mention %q", err, "card_common_id is required")
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// Favro slots items between siblings with fractional positions, so
// decoding position as an int would fail the whole page.
func TestListTasks_FractionalPositionDecodes(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"entities":[
			{"taskId":"t-1","name":"x","position":3.125}
		]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListTasks(context.Background(), 0, "", favro.ListTasksFilter{CardCommonID: "cc-1"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if math.Abs(env.Entities[0].Position-3.125) > 0.0001 {
		t.Errorf("env.Entities[0].Position = %v, want %v within 0.0001", env.Entities[0].Position, 3.125)
	}
}

func TestGetTask_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Path; got != "/tasks/t-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasks/t-1")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"t-1","name":"do it","completed":true}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.GetTask(context.Background(), "t-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.TaskID; got != "t-1" {
		t.Errorf("got.TaskID = %v, want %v", got, "t-1")
	}
	if !got.Completed {
		t.Error("got.Completed = false, want true")
	}
}

func TestCreateTask_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/tasks" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasks")
		}
		requireJSONEq(t, `{"taskListId":"tl-1","name":"do it","position":2}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"t-new","name":"do it"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	pos := 2.0
	got, err := c.CreateTask(context.Background(), favro.CreateTaskRequest{
		TaskListID: "tl-1",
		Name:       "do it",
		Position:   &pos,
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.TaskID; got != "t-new" {
		t.Errorf("got.TaskID = %v, want %v", got, "t-new")
	}
}

func TestCreateTask_MissingFields_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateTask(context.Background(), favro.CreateTaskRequest{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "task_list_id is required") {
		t.Fatalf("got %v, want it to mention %q", err, "task_list_id is required")
	}

	_, err = c.CreateTask(context.Background(), favro.CreateTaskRequest{TaskListID: "tl-1"})
	if err == nil || !strings.Contains(err.Error(), "task name is required") {
		t.Fatalf("got %v, want it to mention %q", err, "task name is required")
	}

	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// Un-ticking an item is a legitimate update, so completed:false must
// survive omitempty rather than being elided into a no-op.
func TestUpdateTask_ExplicitFalseCompletedSurvives(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/tasks/t-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasks/t-1")
		}
		requireJSONEq(t, `{"completed":false}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"t-1","completed":false}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	done := false
	got, err := c.UpdateTask(context.Background(), "t-1", favro.UpdateTaskRequest{Completed: &done})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Completed {
		t.Error("got.Completed = true, want false")
	}
}

func TestUpdateTask_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateTask(context.Background(), "", favro.UpdateTaskRequest{Name: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestDeleteTask_HappyPathAndEmptyID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/tasks/t-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/tasks/t-1")
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteTask(context.Background(), "t-1"); err != nil {
		t.Fatalf("c.DeleteTask(context.Background(), \"t-1\"): %v", err)
	}
	if !errors.Is(c.DeleteTask(context.Background(), ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteTask(context.Background(), ""))
	}
}

func TestTaskWrites_DryRun_NeverDispatch(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}
	ctx := WithDryRun(context.Background())

	_, err := c.CreateTask(ctx, favro.CreateTaskRequest{TaskListID: "tl-1", Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}

	_, err = c.UpdateTask(ctx, "t-1", favro.UpdateTaskRequest{Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}

	if !errors.Is(c.DeleteTask(ctx, "t-1"), ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", c.DeleteTask(ctx, "t-1"))
	}
}
