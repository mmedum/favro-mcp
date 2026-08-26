package favro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const dependenciesFixture = `{
	"cardId":"ci-1",
	"cardCommonId":"cc-1",
	"organizationId":"org-1",
	"dependencies":[{"cardId":"ci-2","cardCommonId":"cc-2","isBefore":true,"reverseCardId":"ci-1"}]
}`

// The dependencies endpoint is not paginated — it returns one object
// with the full list, unlike every other Favro collection endpoint.
func TestListDependencies_ReturnsUnpaginatedObject(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodGet, rec.Method)
		require.Equal(t, "/cards/ci-1/dependencies", rec.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dependenciesFixture))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.ListDependencies(context.Background(), "ci-1")
	require.NoError(t, err)
	require.Equal(t, "ci-1", got.CardID)
	require.Len(t, got.Dependencies, 1)
	require.Equal(t, "ci-2", got.Dependencies[0].CardID)
	require.True(t, got.Dependencies[0].IsBefore)
}

// POST adds to the existing list, PUT replaces it wholesale. Pin the
// method mapping — getting it backwards silently wipes a card's
// dependencies.
func TestDependencies_AddUsesPost_ReplaceUsesPut(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		call       func(*Client) (CardDependencies, error)
		wantMethod string
	}{
		{
			name: "add",
			call: func(c *Client) (CardDependencies, error) {
				return c.CreateDependencies(context.Background(), "ci-1",
					[]CardDependencyOption{{CardID: "ci-2", IsBefore: true}})
			},
			wantMethod: http.MethodPost,
		},
		{
			name: "replace",
			call: func(c *Client) (CardDependencies, error) {
				return c.ReplaceDependencies(context.Background(), "ci-1",
					[]CardDependencyOption{{CardID: "ci-2", IsBefore: true}})
			},
			wantMethod: http.MethodPut,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
				require.Equal(t, tc.wantMethod, rec.Method)
				require.Equal(t, "/cards/ci-1/dependencies", rec.Path)
				require.JSONEq(t, `{"dependencies":[{"cardId":"ci-2","isBefore":true}]}`, rec.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(dependenciesFixture))
			}}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)

			got, err := tc.call(newTestClient(srv))
			require.NoError(t, err)
			require.Len(t, got.Dependencies, 1)
		})
	}
}

func TestDependencies_WriteValidation_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateDependencies(context.Background(), "", []CardDependencyOption{{CardID: "ci-2"}})
	require.ErrorIs(t, err, errMissingID)

	_, err = c.CreateDependencies(context.Background(), "ci-1", nil)
	require.ErrorContains(t, err, "at least one dependency")

	_, err = c.CreateDependencies(context.Background(), "ci-1", []CardDependencyOption{{IsBefore: true}})
	require.ErrorContains(t, err, "missing cardId")

	require.Empty(t, h.seen())
}

// Flipping a link to "after" sends isBefore:false, which *bool keeps
// from being elided by omitempty into a no-op PATCH.
func TestUpdateDependency_ExplicitFalseSurvives(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPatch, rec.Method)
		require.Equal(t, "/cards/ci-1/dependencies/ci-2", rec.Path)
		require.JSONEq(t, `{"isBefore":false}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dependenciesFixture))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	after := false
	_, err := c.UpdateDependency(context.Background(), "ci-1", "ci-2", UpdateDependencyRequest{IsBefore: &after})
	require.NoError(t, err)
}

func TestDeleteDependency_SingleAndAll(t *testing.T) {
	t.Parallel()

	var paths []string
	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		paths = append(paths, rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.NoError(t, c.DeleteDependency(context.Background(), "ci-1", "ci-2"))
	require.NoError(t, c.DeleteAllDependencies(context.Background(), "ci-1"))
	require.Equal(t, []string{"/cards/ci-1/dependencies/ci-2", "/cards/ci-1/dependencies"}, paths)

	require.ErrorIs(t, c.DeleteDependency(context.Background(), "ci-1", ""), errMissingID)
	require.ErrorIs(t, c.DeleteAllDependencies(context.Background(), ""), errMissingID)
}

func TestDependencyWrites_DryRun_NeverDispatch(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}
	ctx := WithDryRun(context.Background())
	deps := []CardDependencyOption{{CardID: "ci-2"}}

	_, err := c.CreateDependencies(ctx, "ci-1", deps)
	require.ErrorIs(t, err, ErrDryRun)

	_, err = c.ReplaceDependencies(ctx, "ci-1", deps)
	require.ErrorIs(t, err, ErrDryRun)

	_, err = c.UpdateDependency(ctx, "ci-1", "ci-2", UpdateDependencyRequest{})
	require.ErrorIs(t, err, ErrDryRun)

	require.ErrorIs(t, c.DeleteDependency(ctx, "ci-1", "ci-2"), ErrDryRun)
	require.ErrorIs(t, c.DeleteAllDependencies(ctx, "ci-1"), ErrDryRun)
}
