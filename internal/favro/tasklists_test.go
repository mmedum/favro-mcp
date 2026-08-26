package favro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListTasklists_ForwardsCardCommonID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, "/tasklists", rec.Path)
		require.Equal(t, "cc-1", rec.Query.Get("cardCommonId"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"entities":[
			{"taskListId":"tl-1","cardCommonId":"cc-1","name":"Acceptance","position":0}
		]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListTasklists(context.Background(), 0, "", "cc-1")
	require.NoError(t, err)
	require.Len(t, env.Entities, 1)
	require.Equal(t, "Acceptance", env.Entities[0].Title())
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
	require.NoError(t, err)
	require.Empty(t, env.Entities[0].Name)
	require.Equal(t, "From the docs example", env.Entities[0].Title())
}

func TestListTasklists_RequiresCardCommonID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListTasklists(context.Background(), 0, "", "")
	require.ErrorContains(t, err, "card_common_id is required")
	require.Empty(t, h.seen())
}

func TestGetTasklist_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, "/tasklists/tl-1", rec.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskListId":"tl-1","name":"Acceptance"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.GetTasklist(context.Background(), "tl-1")
	require.NoError(t, err)
	require.Equal(t, "tl-1", got.TaskListID)
}

// Seeding items at creation time saves one round-trip per item, so
// pin that the tasks array reaches the wire.
func TestCreateTasklist_SeedsTasks(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/tasklists", rec.Path)
		require.JSONEq(t, `{
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

	got, err := c.CreateTasklist(context.Background(), CreateTasklistRequest{
		CardCommonID: "cc-1",
		Name:         "Acceptance",
		Tasks: []CardTask{
			{Name: "first"},
			{Name: "second", Completed: true},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "tl-new", got.TaskListID)
}

func TestCreateTasklist_MissingFields_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateTasklist(context.Background(), CreateTasklistRequest{Name: "x"})
	require.ErrorContains(t, err, "card_common_id is required")

	_, err = c.CreateTasklist(context.Background(), CreateTasklistRequest{CardCommonID: "cc-1"})
	require.ErrorContains(t, err, "tasklist name is required")

	require.Empty(t, h.seen())
}

func TestUpdateAndDeleteTasklist(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, "/tasklists/tl-1", rec.Path)
		if rec.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		require.JSONEq(t, `{"name":"Renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskListId":"tl-1","name":"Renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateTasklist(context.Background(), "tl-1", UpdateTasklistRequest{Name: "Renamed"})
	require.NoError(t, err)
	require.Equal(t, "Renamed", got.Name)

	require.NoError(t, c.DeleteTasklist(context.Background(), "tl-1"))

	_, err = c.UpdateTasklist(context.Background(), "", UpdateTasklistRequest{Name: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.ErrorIs(t, c.DeleteTasklist(context.Background(), ""), errMissingID)
}

func TestTasklistWrites_DryRun_NeverDispatch(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}
	ctx := WithDryRun(context.Background())

	_, err := c.CreateTasklist(ctx, CreateTasklistRequest{CardCommonID: "cc-1", Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)

	_, err = c.UpdateTasklist(ctx, "tl-1", UpdateTasklistRequest{Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)

	require.ErrorIs(t, c.DeleteTasklist(ctx, "tl-1"), ErrDryRun)
}
