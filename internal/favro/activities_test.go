package favro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListCardActivities_ForwardsWindow(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodGet, rec.Method)
		require.Equal(t, "/cards/ci-1/activities", rec.Path)
		require.Equal(t, "2026-01-01T00:00:00Z", rec.Query.Get("since"))
		require.Equal(t, "2026-02-01T00:00:00Z", rec.Query.Get("until"))
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

	env, err := c.ListCardActivities(context.Background(), 0, "", "ci-1", ListActivitiesFilter{
		Since: "2026-01-01T00:00:00Z",
		Until: "2026-02-01T00:00:00Z",
	})
	require.NoError(t, err)
	require.Len(t, env.Entities, 1)
	require.Equal(t, "assigned", env.Entities[0].Type)
	require.Equal(t, "u-1", env.Entities[0].ByUserID)
}

// An unset window must leave both query parameters off entirely
// rather than sending empty strings Favro would try to parse.
func TestListCardActivities_OmitsEmptyWindow(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.False(t, rec.Query.Has("since"))
		require.False(t, rec.Query.Has("until"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"entities":[]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCardActivities(context.Background(), 0, "", "ci-1", ListActivitiesFilter{})
	require.NoError(t, err)
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

	env, err := c.ListCardActivities(context.Background(), 0, "", "ci-1", ListActivitiesFilter{})
	require.NoError(t, err)
	require.Equal(t, "cc-from-example", env.Entities[0].CommonID())
	require.Equal(t, "cc-from-table", env.Entities[1].CommonID())
}

func TestListCardActivities_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListCardActivities(context.Background(), 0, "", "", ListActivitiesFilter{})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}
