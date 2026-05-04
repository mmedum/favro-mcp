package favro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// pagedHandler is a fake Favro that serves N pages of testEntities.
// Page 0 has the initial requestId; subsequent pages must echo it back.
type pagedHandler struct {
	totalPages int
	requestID  string
	calls      atomic.Int32
	t          *testing.T
}

func (p *pagedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.calls.Add(1)
	page := 0
	if v := r.URL.Query().Get("page"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &page)
	}

	if page > 0 {
		gotID := r.Header.Get(headerRequestID)
		if gotID != p.requestID {
			p.t.Errorf("page %d expected %s=%s, got %s", page, headerRequestID, p.requestID, gotID)
			http.Error(w, "missing or wrong request id", http.StatusBadRequest)
			return
		}
	}

	env := PageEnvelope[testEntity]{
		Limit:     100,
		Page:      page,
		Pages:     p.totalPages,
		RequestID: p.requestID,
		Entities:  []testEntity{{ID: fmt.Sprintf("e-%d", page), Name: fmt.Sprintf("page-%d", page)}},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(env)
}

func TestPaginate_SinglePage_VisitsOnce(t *testing.T) {
	t.Parallel()

	h := &pagedHandler{totalPages: 1, requestID: "req-1", t: t}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	var visits []string
	err := Paginate[testEntity](context.Background(), c, "/cards", nil,
		func(env PageEnvelope[testEntity]) error {
			for _, e := range env.Entities {
				visits = append(visits, e.ID)
			}
			return nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"e-0"}, visits)
	require.EqualValues(t, 1, h.calls.Load())
}

func TestPaginate_MultiplePages_EchoesRequestID(t *testing.T) {
	t.Parallel()

	h := &pagedHandler{totalPages: 3, requestID: "req-multi", t: t}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	var visits []string
	err := Paginate[testEntity](context.Background(), c, "/cards", nil,
		func(env PageEnvelope[testEntity]) error {
			for _, e := range env.Entities {
				visits = append(visits, e.ID)
			}
			return nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"e-0", "e-1", "e-2"}, visits)
	require.EqualValues(t, 3, h.calls.Load())
}

func TestPaginate_VisitErrorStopsIteration(t *testing.T) {
	t.Parallel()

	h := &pagedHandler{totalPages: 5, requestID: "req-err", t: t}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	stop := fmt.Errorf("stop")
	visited := 0
	err := Paginate[testEntity](context.Background(), c, "/cards", nil,
		func(env PageEnvelope[testEntity]) error {
			visited++
			if visited == 2 {
				return stop
			}
			return nil
		},
	)
	require.ErrorIs(t, err, stop)
	require.Equal(t, 2, visited)
	require.EqualValues(t, 2, h.calls.Load(), "must not fetch the third page after visit returns")
}

func TestPaginate_NilCallback_Errors(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	err := Paginate[testEntity](context.Background(), c, "/cards", nil, nil)
	require.Error(t, err)
}

func TestPaginate_NilClient_Errors(t *testing.T) {
	t.Parallel()

	err := Paginate[testEntity](context.Background(), nil, "/cards", nil, func(_ PageEnvelope[testEntity]) error { return nil })
	require.Error(t, err)
}

func TestCloneQuery_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := url.Values{"a": []string{"1"}, "b": []string{"2", "3"}}
	clone := cloneQuery(original)

	clone.Set("a", "999")
	clone.Add("b", "4")

	require.Equal(t, []string{"1"}, original["a"], "original must not be mutated")
	require.Equal(t, []string{"2", "3"}, original["b"])
}
