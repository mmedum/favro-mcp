package favro

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/auth"
)

// fixtureToken provides obviously-fake credentials for tests. None of
// these values match a real Favro account.
func fixtureToken() auth.Token {
	return auth.Token{
		Email:          "fixture@example.invalid",
		APIToken:       "fixture-token",
		OrganizationID: "fixture-org",
	}
}

// newTestClient wires a Client to a test server. The defaults match
// production except for the base URL.
func newTestClient(srv *httptest.Server) *Client {
	c := NewClient(fixtureToken())
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()
	return c
}

// recordingHandler captures every received request so tests can
// assert on method, path, headers, and body.
type recordingHandler struct {
	mu       sync.Mutex
	requests []recordedRequest
	respond  func(req recordedRequest, w http.ResponseWriter)
}

type recordedRequest struct {
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    string
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	rec := recordedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Query:   r.URL.Query(),
		Headers: r.Header.Clone(),
		Body:    string(body),
	}
	h.mu.Lock()
	h.requests = append(h.requests, rec)
	h.mu.Unlock()
	h.respond(rec, w)
}

func (h *recordingHandler) seen() []recordedRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]recordedRequest, len(h.requests))
	copy(out, h.requests)
	return out
}

func TestDo_HappyPath_AppliesAuthAndOrgHeader(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(context.Background(), http.MethodGet, "/cards", nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { drainAndClose(resp) })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, http.MethodGet, rec[0].Method)
	require.Equal(t, "/cards", rec[0].Path)

	user, pass, ok := parseBasic(rec[0].Headers.Get("Authorization"))
	require.True(t, ok, "Authorization header must be Basic")
	require.Equal(t, "fixture@example.invalid", user)
	require.Equal(t, "fixture-token", pass)
	require.Equal(t, "fixture-org", rec[0].Headers.Get("organizationId"))
}

func TestDo_429WithinCap_RetriesOnce(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set(headerRetryAfter, "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { drainAndClose(resp) })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 2, calls.Load(), "client should retry once on 429")
}

func TestDo_429AboveCap_ReturnsRateLimitError(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set(headerRetryAfter, "120") // > rateLimitRetryCap (30s)
		w.WriteHeader(http.StatusTooManyRequests)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil)
	drainAndClose(resp) // resp is nil on error; drainAndClose is nil-safe and keeps bodyclose happy
	var rl *RateLimitError
	require.ErrorAs(t, err, &rl)
	require.Equal(t, 120*time.Second, rl.RetryAfter)
}

func TestDo_5xx_RetriesUpToBudgetThenTransientError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)
	// Fast-path the backoff for the test by overriding the schedule
	// via a private helper; we keep the constants intact for production.
	// In the absence of an injection point, we just accept the
	// production schedule (250ms, 1s, 4s) — total ≈ 5.25s. Worth it
	// for correctness coverage; a separate fast-path test could be
	// added later if this lands in the slow-tests bucket.
	resp, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil)
	drainAndClose(resp)
	var te *TransientError
	require.ErrorAs(t, err, &te)
	require.Equal(t, transientMaxAttempts, te.Attempts)
	require.EqualValues(t, transientMaxAttempts, calls.Load(),
		"client should attempt %d times before giving up", transientMaxAttempts)
}

func TestDo_401_ReturnsAuthErrorNoRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil)
	drainAndClose(resp)
	var ae *AuthError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, http.StatusUnauthorized, ae.Status)
	require.EqualValues(t, 1, calls.Load(), "401 must never be retried")
}

// TestDo_403_ReturnsForbiddenErrorNotAuthError pins the contract
// that 403 is mapped to *ForbiddenError, not *AuthError. Favro
// answers 403 for resources the token can't see (intentionally
// avoiding 404 to not leak existence); reporting that as
// "check FAVRO_USER_EMAIL" was misleading. Phase 3.10 follow-up.
func TestDo_403_ReturnsForbiddenErrorNotAuthError(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(context.Background(), http.MethodGet, "/cards/some-id", nil, nil)
	drainAndClose(resp)

	var fe *ForbiddenError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, http.StatusForbidden, fe.Status)
	require.Contains(t, fe.Path, "/cards/some-id")

	// Must NOT also satisfy AuthError — the whole point of the split.
	var ae *AuthError
	require.NotErrorAs(t, err, &ae, "403 must not be classified as AuthError")

	require.EqualValues(t, 1, calls.Load(), "403 must never be retried")
}

func TestDo_404_ReturnsNotFoundErrorNoRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(context.Background(), http.MethodGet, "/cards/missing", nil, nil)
	drainAndClose(resp)
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
	require.Equal(t, "/cards/missing", nf.Path)
	require.EqualValues(t, 1, calls.Load(), "404 must never be retried")
}

func TestDo_400_ReturnsValidationErrorWithBody(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"name required"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(context.Background(), http.MethodPost, "/cards", nil, map[string]any{"foo": "bar"})
	drainAndClose(resp)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	require.Equal(t, http.StatusBadRequest, ve.Status)
	require.Contains(t, ve.Body, "name required")
}

func TestDo_RecordsRateLimitSnapshot(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set(headerRateLimitLimit, "1000")
		w.Header().Set(headerRateLimitRemaining, "997")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil)
	require.NoError(t, err)
	t.Cleanup(func() { drainAndClose(resp) })

	snap, ok := c.LatestRateLimit()
	require.True(t, ok)
	require.Equal(t, 1000, snap.Limit)
	require.Equal(t, 997, snap.Remaining)
}

func TestDo_DryRun_OnMutatingMethod_ShortCircuits(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(WithDryRun(context.Background()), http.MethodPost, "/cards", nil, map[string]any{"name": "x"})
	drainAndClose(resp)
	require.ErrorIs(t, err, ErrDryRun)

	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
	require.Contains(t, rec.URL, "/cards")
	require.Equal(t, "[REDACTED]", rec.Headers.Get("Authorization"))
	require.NotEmpty(t, rec.Body, "body must be captured for review")

	require.EqualValues(t, 0, calls.Load(), "dry-run must NOT contact the server")
}

func TestDo_DryRun_OnGET_DoesNotShortCircuit(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(WithDryRun(context.Background()), http.MethodGet, "/cards", nil, nil)
	require.NoError(t, err, "GETs are never dry-run")
	t.Cleanup(func() { drainAndClose(resp) })
	require.EqualValues(t, 1, calls.Load())
}

func TestDo_ForceDryRun_HonoredEvenWithoutContext(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)
	c.ForceDryRun = true

	resp, err := c.Do(context.Background(), http.MethodDelete, "/cards/x", nil, nil)
	drainAndClose(resp)
	require.ErrorIs(t, err, ErrDryRun)
	require.EqualValues(t, 0, calls.Load())
}

func TestRedactHeaders_Authorization(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set("Authorization", "Basic abc==")
	h.Set("organizationId", "org-1")
	h.Set("Accept", "application/json")

	out := redactHeaders(h)
	require.Equal(t, "[REDACTED]", out["Authorization"])
	require.Equal(t, "org-1", out["Organizationid"])
	require.Equal(t, "application/json", out["Accept"])
}

func TestJoinURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		base, path string
		query      url.Values
		want       string
		wantErr    bool
	}{
		{"https://favro.com/api/v1", "/cards", nil, "https://favro.com/api/v1/cards", false},
		{"https://favro.com/api/v1/", "cards", nil, "https://favro.com/api/v1/cards", false},
		{"https://favro.com/api/v1", "/cards", url.Values{"page": []string{"2"}}, "https://favro.com/api/v1/cards?page=2", false},
		{"https://favro.com/api/v1", "", nil, "", true},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			t.Parallel()
			got, err := joinURL(tc.base, tc.path, tc.query)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEncodeBody(t *testing.T) {
	t.Parallel()

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		got, err := encodeBody(nil)
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("raw bytes pass-through", func(t *testing.T) {
		t.Parallel()
		got, err := encodeBody([]byte(`{"raw":1}`))
		require.NoError(t, err)
		require.Equal(t, `{"raw":1}`, string(got))
	})
	t.Run("struct json-encoded", func(t *testing.T) {
		t.Parallel()
		got, err := encodeBody(map[string]string{"k": "v"})
		require.NoError(t, err)
		require.JSONEq(t, `{"k":"v"}`, string(got))
	})
}

func TestDo_ContextCancellation_StopsRetry(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	resp, err := c.Do(ctx, http.MethodGet, "/x", nil, nil)
	drainAndClose(resp)
	require.Error(t, err)
	// Either the first attempt errors with the context error, or the
	// retry sleep returns it. Either way the surface error must wrap
	// context.Canceled.
	require.ErrorIs(t, err, context.Canceled)
}

// parseBasic decodes a "Basic <base64(user:pass)>" header value.
func parseBasic(v string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(v, prefix) {
		return "", "", false
	}
	req := &http.Request{Header: http.Header{"Authorization": []string{v}}}
	return req.BasicAuth()
}
