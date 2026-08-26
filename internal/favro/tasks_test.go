package favro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListTasks_ForwardsFilters(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodGet, rec.Method)
		require.Equal(t, "/tasks", rec.Path)
		require.Equal(t, "cc-1", rec.Query.Get("cardCommonId"))
		require.Equal(t, "tl-1", rec.Query.Get("taskListId"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[
			{"taskId":"t-1","taskListId":"tl-1","cardCommonId":"cc-1","name":"do it","completed":false,"position":0}
		]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListTasks(context.Background(), 0, "", ListTasksFilter{
		CardCommonID: "cc-1",
		TaskListID:   "tl-1",
	})
	require.NoError(t, err)
	require.Len(t, env.Entities, 1)
	require.Equal(t, "t-1", env.Entities[0].TaskID)
	require.Equal(t, "do it", env.Entities[0].Name)
}

// Favro rejects an unscoped /tasks listing. Catching it client-side
// gives a clearer message and costs no rate-limit budget.
func TestListTasks_RequiresCardCommonID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListTasks(context.Background(), 0, "", ListTasksFilter{})
	require.ErrorContains(t, err, "card_common_id is required")
	require.Empty(t, h.seen())
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

	env, err := c.ListTasks(context.Background(), 0, "", ListTasksFilter{CardCommonID: "cc-1"})
	require.NoError(t, err)
	require.InDelta(t, 3.125, env.Entities[0].Position, 0.0001)
}

func TestGetTask_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, "/tasks/t-1", rec.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"t-1","name":"do it","completed":true}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.GetTask(context.Background(), "t-1")
	require.NoError(t, err)
	require.Equal(t, "t-1", got.TaskID)
	require.True(t, got.Completed)
}

func TestCreateTask_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/tasks", rec.Path)
		require.JSONEq(t, `{"taskListId":"tl-1","name":"do it","position":2}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"t-new","name":"do it"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	pos := 2.0
	got, err := c.CreateTask(context.Background(), CreateTaskRequest{
		TaskListID: "tl-1",
		Name:       "do it",
		Position:   &pos,
	})
	require.NoError(t, err)
	require.Equal(t, "t-new", got.TaskID)
}

func TestCreateTask_MissingFields_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateTask(context.Background(), CreateTaskRequest{Name: "x"})
	require.ErrorContains(t, err, "task_list_id is required")

	_, err = c.CreateTask(context.Background(), CreateTaskRequest{TaskListID: "tl-1"})
	require.ErrorContains(t, err, "task name is required")

	require.Empty(t, h.seen())
}

// Un-ticking an item is a legitimate update, so completed:false must
// survive omitempty rather than being elided into a no-op.
func TestUpdateTask_ExplicitFalseCompletedSurvives(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/tasks/t-1", rec.Path)
		require.JSONEq(t, `{"completed":false}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"t-1","completed":false}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	done := false
	got, err := c.UpdateTask(context.Background(), "t-1", UpdateTaskRequest{Completed: &done})
	require.NoError(t, err)
	require.False(t, got.Completed)
}

func TestUpdateTask_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateTask(context.Background(), "", UpdateTaskRequest{Name: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestDeleteTask_HappyPathAndEmptyID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		require.Equal(t, "/tasks/t-1", rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.NoError(t, c.DeleteTask(context.Background(), "t-1"))
	require.ErrorIs(t, c.DeleteTask(context.Background(), ""), errMissingID)
}

func TestTaskWrites_DryRun_NeverDispatch(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}
	ctx := WithDryRun(context.Background())

	_, err := c.CreateTask(ctx, CreateTaskRequest{TaskListID: "tl-1", Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)

	_, err = c.UpdateTask(ctx, "t-1", UpdateTaskRequest{Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)

	require.ErrorIs(t, c.DeleteTask(ctx, "t-1"), ErrDryRun)
}
