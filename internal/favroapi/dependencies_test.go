package favroapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
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
		if got := rec.Method; got != http.MethodGet {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodGet)
		}
		if got := rec.Path; got != "/cards/ci-1/dependencies" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1/dependencies")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dependenciesFixture))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.ListDependencies(context.Background(), "ci-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.CardID; got != "ci-1" {
		t.Errorf("got.CardID = %v, want %v", got, "ci-1")
	}
	if len(got.Dependencies) != 1 {
		t.Fatalf("len(got.Dependencies) = %d, want 1", len(got.Dependencies))
	}
	if got := got.Dependencies[0].CardID; got != "ci-2" {
		t.Errorf("got.Dependencies[0].CardID = %v, want %v", got, "ci-2")
	}
	if !got.Dependencies[0].IsBefore {
		t.Error("got.Dependencies[0].IsBefore = false, want true")
	}
}

// POST adds to the existing list, PUT replaces it wholesale. Pin the
// method mapping — getting it backwards silently wipes a card's
// dependencies.
func TestDependencies_AddUsesPost_ReplaceUsesPut(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		call       func(*Client) (favro.CardDependencies, error)
		wantMethod string
	}{
		{
			name: "add",
			call: func(c *Client) (favro.CardDependencies, error) {
				return c.CreateDependencies(context.Background(), "ci-1",
					[]favro.CardDependencyOption{{CardID: "ci-2", IsBefore: true}})
			},
			wantMethod: http.MethodPost,
		},
		{
			name: "replace",
			call: func(c *Client) (favro.CardDependencies, error) {
				return c.ReplaceDependencies(context.Background(), "ci-1",
					[]favro.CardDependencyOption{{CardID: "ci-2", IsBefore: true}})
			},
			wantMethod: http.MethodPut,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
				if got := rec.Method; got != tc.wantMethod {
					t.Errorf("rec.Method = %v, want %v", got, tc.wantMethod)
				}
				if got := rec.Path; got != "/cards/ci-1/dependencies" {
					t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1/dependencies")
				}
				requireJSONEq(t, `{"dependencies":[{"cardId":"ci-2","isBefore":true}]}`, rec.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(dependenciesFixture))
			}}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)

			got, err := tc.call(newTestClient(srv))
			if err := err; err != nil {
				t.Fatalf("err: %v", err)
			}
			if len(got.Dependencies) != 1 {
				t.Fatalf("len(got.Dependencies) = %d, want 1", len(got.Dependencies))
			}
		})
	}
}

func TestDependencies_WriteValidation_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateDependencies(context.Background(), "", []favro.CardDependencyOption{{CardID: "ci-2"}})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}

	_, err = c.CreateDependencies(context.Background(), "ci-1", nil)
	if err == nil || !strings.Contains(err.Error(), "at least one dependency") {
		t.Fatalf("got %v, want it to mention %q", err, "at least one dependency")
	}

	_, err = c.CreateDependencies(context.Background(), "ci-1", []favro.CardDependencyOption{{IsBefore: true}})
	if err == nil || !strings.Contains(err.Error(), "missing cardId") {
		t.Fatalf("got %v, want it to mention %q", err, "missing cardId")
	}

	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// Flipping a link to "after" sends isBefore:false, which *bool keeps
// from being elided by omitempty into a no-op PATCH.
func TestUpdateDependency_ExplicitFalseSurvives(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPatch {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPatch)
		}
		if got := rec.Path; got != "/cards/ci-1/dependencies/ci-2" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1/dependencies/ci-2")
		}
		requireJSONEq(t, `{"isBefore":false}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(dependenciesFixture))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	after := false
	_, err := c.UpdateDependency(context.Background(), "ci-1", "ci-2", favro.UpdateDependencyRequest{IsBefore: &after})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestDeleteDependency_SingleAndAll(t *testing.T) {
	t.Parallel()

	var paths []string
	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		paths = append(paths, rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteDependency(context.Background(), "ci-1", "ci-2"); err != nil {
		t.Fatalf("c.DeleteDependency(context.Background(), \"ci-1\", \"ci-2\"): %v", err)
	}
	if err := c.DeleteAllDependencies(context.Background(), "ci-1"); err != nil {
		t.Fatalf("c.DeleteAllDependencies(context.Background(), \"ci-1\"): %v", err)
	}
	if got := paths; !reflect.DeepEqual(got, ([]string{"/cards/ci-1/dependencies/ci-2", "/cards/ci-1/dependencies"})) {
		t.Errorf("paths = %v, want %v", got, []string{"/cards/ci-1/dependencies/ci-2", "/cards/ci-1/dependencies"})
	}

	if !errors.Is(c.DeleteDependency(context.Background(), "ci-1", ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteDependency(context.Background(), "ci-1", ""))
	}
	if !errors.Is(c.DeleteAllDependencies(context.Background(), ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteAllDependencies(context.Background(), ""))
	}
}

func TestDependencyWrites_DryRun_NeverDispatch(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}
	ctx := WithDryRun(context.Background())
	deps := []favro.CardDependencyOption{{CardID: "ci-2"}}

	_, err := c.CreateDependencies(ctx, "ci-1", deps)
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}

	_, err = c.ReplaceDependencies(ctx, "ci-1", deps)
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}

	_, err = c.UpdateDependency(ctx, "ci-1", "ci-2", favro.UpdateDependencyRequest{})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}

	if !errors.Is(c.DeleteDependency(ctx, "ci-1", "ci-2"), ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", c.DeleteDependency(ctx, "ci-1", "ci-2"))
	}
	if !errors.Is(c.DeleteAllDependencies(ctx, "ci-1"), ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", c.DeleteAllDependencies(ctx, "ci-1"))
	}
}
