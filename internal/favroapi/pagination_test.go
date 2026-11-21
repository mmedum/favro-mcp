package favroapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
)

// pagedHandler is a fake Favro that serves N pages of testEntities, as
// the reference describes: page 0 returns a requestId and a backend
// identifier header, and every later page must send the requestId as a
// query parameter and the identifier as a header.
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
		gotID, gotBackend := r.URL.Query().Get("requestId"), r.Header.Get(headerBackendID)
		if gotID != p.requestID || gotBackend != testBackendID {
			p.t.Errorf("page %d: requestId=%q and %s=%q, want %q and %q",
				page, gotID, headerBackendID, gotBackend, p.requestID, testBackendID)
			http.Error(w, "missing or wrong request id", http.StatusBadRequest)
			return
		}
	}
	w.Header().Set(headerBackendID, testBackendID)

	env := favro.PageEnvelope[testEntity]{
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

// testBackendID is the X-Favro-Backend-Identifier the fake answers with.
const testBackendID = "backend-7"

func TestPaginate_SinglePage_VisitsOnce(t *testing.T) {
	t.Parallel()

	h := &pagedHandler{totalPages: 1, requestID: "req-1", t: t}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	var visits []string
	err := Paginate[testEntity](context.Background(), c, "/cards", nil,
		func(env favro.PageEnvelope[testEntity]) error {
			for _, e := range env.Entities {
				visits = append(visits, e.ID)
			}
			return nil
		},
	)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := visits; !reflect.DeepEqual(got, ([]string{"e-0"})) {
		t.Errorf("visits = %v, want %v", got, []string{"e-0"})
	}
	if got := h.calls.Load(); got != 1 {
		t.Errorf("h.calls.Load() = %v, want %v", got, 1)
	}
}

func TestPaginate_MultiplePages_EchoesRequestID(t *testing.T) {
	t.Parallel()

	h := &pagedHandler{totalPages: 3, requestID: "req-multi", t: t}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	var visits []string
	err := Paginate[testEntity](context.Background(), c, "/cards", nil,
		func(env favro.PageEnvelope[testEntity]) error {
			for _, e := range env.Entities {
				visits = append(visits, e.ID)
			}
			return nil
		},
	)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := visits; !reflect.DeepEqual(got, ([]string{"e-0", "e-1", "e-2"})) {
		t.Errorf("visits = %v, want %v", got, []string{"e-0", "e-1", "e-2"})
	}
	if got := h.calls.Load(); got != 3 {
		t.Errorf("h.calls.Load() = %v, want %v", got, 3)
	}
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
		func(env favro.PageEnvelope[testEntity]) error {
			visited++
			if visited == 2 {
				return stop
			}
			return nil
		},
	)
	if !errors.Is(err, stop) {
		t.Fatalf("got %v, want stop", err)
	}
	if got := visited; got != 2 {
		t.Errorf("visited = %v, want %v", got, 2)
	}
	if got := h.calls.Load(); got != 2 {
		t.Errorf("must not fetch the third page after visit returns: got %v, want %v", got, 2)
	}
}

func TestPaginate_NilCallback_Errors(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	err := Paginate[testEntity](context.Background(), c, "/cards", nil, nil)
	if err == nil {
		t.Fatal("err should have failed")
	}
}

func TestPaginate_NilClient_Errors(t *testing.T) {
	t.Parallel()

	err := Paginate[testEntity](context.Background(), nil, "/cards", nil, func(_ favro.PageEnvelope[testEntity]) error { return nil })
	if err == nil {
		t.Fatal("err should have failed")
	}
}

func TestCloneQuery_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := url.Values{"a": []string{"1"}, "b": []string{"2", "3"}}
	clone := cloneQuery(original)

	clone.Set("a", "999")
	clone.Add("b", "4")

	if got := original["a"]; !reflect.DeepEqual(got, ([]string{"1"})) {
		t.Errorf("original must not be mutated: got %v, want %v", got, []string{"1"})
	}
	if got := original["b"]; !reflect.DeepEqual(got, ([]string{"2", "3"})) {
		t.Errorf("original[\"b\"] = %v, want %v", got, []string{"2", "3"})
	}
}
