package favroapi

import (
	"context"
	"errors"
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	t.Cleanup(func() { drainAndClose(resp) })
	if got := resp.StatusCode; got != http.StatusOK {
		t.Errorf("resp.StatusCode = %v, want %v", got, http.StatusOK)
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Method; got != http.MethodGet {
		t.Errorf("rec[0].Method = %v, want %v", got, http.MethodGet)
	}
	if got := rec[0].Path; got != "/cards" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/cards")
	}

	user, pass, ok := parseBasic(rec[0].Headers.Get("Authorization"))
	if !ok {
		t.Error("Authorization header must be Basic")
	}
	if got := user; got != "fixture@example.invalid" {
		t.Errorf("user = %v, want %v", got, "fixture@example.invalid")
	}
	if got := pass; got != "fixture-token" {
		t.Errorf("pass = %v, want %v", got, "fixture-token")
	}
	if got := rec[0].Headers.Get("organizationId"); got != "fixture-org" {
		t.Errorf("rec[0].Headers.Get(\"organizationId\") = %v, want %v", got, "fixture-org")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	t.Cleanup(func() { drainAndClose(resp) })
	if got := resp.StatusCode; got != http.StatusOK {
		t.Errorf("resp.StatusCode = %v, want %v", got, http.StatusOK)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("client should retry once on 429: got %v, want %v", got, 2)
	}
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
	if !errors.As(err, &rl) {
		t.Fatalf("got %v, want rl", err)
	}
	if got := rl.RetryAfter; got != 120*time.Second {
		t.Errorf("rl.RetryAfter = %v, want %v", got, 120*time.Second)
	}
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
	if !errors.As(err, &te) {
		t.Fatalf("got %v, want te", err)
	}
	if got := te.Attempts; got != transientMaxAttempts {
		t.Errorf("te.Attempts = %v, want %v", got, transientMaxAttempts)
	}
	if got := calls.Load(); got != transientMaxAttempts {
		t.Errorf("client should attempt %d times before giving up: got %v, want %v", transientMaxAttempts, got, transientMaxAttempts)
	}
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
	if !errors.As(err, &ae) {
		t.Fatalf("got %v, want ae", err)
	}
	if got := ae.Status; got != http.StatusUnauthorized {
		t.Errorf("ae.Status = %v, want %v", got, http.StatusUnauthorized)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("401 must never be retried: got %v, want %v", got, 1)
	}
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
	if !errors.As(err, &fe) {
		t.Fatalf("got %v, want fe", err)
	}
	if got := fe.Status; got != http.StatusForbidden {
		t.Errorf("fe.Status = %v, want %v", got, http.StatusForbidden)
	}
	if !strings.Contains(fe.Path, "/cards/some-id") {
		t.Errorf("fe.Path does not contain %q", "/cards/some-id")
	}

	// Must NOT also satisfy AuthError — the whole point of the split.
	var ae *AuthError
	if errors.As(err, &ae) {
		t.Fatalf("403 must not be classified as AuthError, got %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("403 must never be retried: got %v, want %v", got, 1)
	}
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
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
	if got := nf.Path; got != "/cards/missing" {
		t.Errorf("nf.Path = %v, want %v", got, "/cards/missing")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("404 must never be retried: got %v, want %v", got, 1)
	}
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
	if !errors.As(err, &ve) {
		t.Fatalf("got %v, want ve", err)
	}
	if got := ve.Status; got != http.StatusBadRequest {
		t.Errorf("ve.Status = %v, want %v", got, http.StatusBadRequest)
	}
	if !strings.Contains(ve.Body, "name required") {
		t.Errorf("ve.Body does not contain %q", "name required")
	}
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	t.Cleanup(func() { drainAndClose(resp) })

	snap, ok := c.LatestRateLimit()
	if !ok {
		t.Error("ok = false, want true")
	}
	if got := snap.Limit; got != 1000 {
		t.Errorf("snap.Limit = %v, want %v", got, 1000)
	}
	if got := snap.Remaining; got != 997 {
		t.Errorf("snap.Remaining = %v, want %v", got, 997)
	}
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
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}

	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodPost {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
	}
	if !strings.Contains(rec.URL, "/cards") {
		t.Errorf("rec.URL does not contain %q", "/cards")
	}
	if got := rec.Headers.Get("Authorization"); got != "[REDACTED]" {
		t.Errorf("rec.Headers.Get(\"Authorization\") = %v, want %v", got, "[REDACTED]")
	}
	if len(rec.Body) == 0 {
		t.Fatal("body must be captured for review")
	}

	if got := calls.Load(); got != 0 {
		t.Errorf("dry-run must NOT contact the server: got %v, want %v", got, 0)
	}
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
	if err := err; err != nil {
		t.Fatalf("GETs are never dry-run: %v", err)
	}
	t.Cleanup(func() { drainAndClose(resp) })
	if got := calls.Load(); got != 1 {
		t.Errorf("calls.Load() = %v, want %v", got, 1)
	}
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
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
}

func TestRedactHeaders_Authorization(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set("Authorization", "Basic abc==")
	h.Set("organizationId", "org-1")
	h.Set("Accept", "application/json")

	out := redactHeaders(h)
	if got := out["Authorization"]; got != "[REDACTED]" {
		t.Errorf("out[\"Authorization\"] = %v, want %v", got, "[REDACTED]")
	}
	// organizationId names the tenant and rides on every request, so
	// it is redacted too. This assertion used to read the other way,
	// which is how the leak survived: the behaviour was not an
	// oversight, it was pinned.
	if got := out["Organizationid"]; got != "[REDACTED]" {
		t.Errorf("out[\"Organizationid\"] = %v, want %v", got, "[REDACTED]")
	}
	if got := out["Accept"]; got != "application/json" {
		t.Errorf("out[\"Accept\"] = %v, want %v", got, "application/json")
	}
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
				if err == nil {
					t.Fatal("err should have failed")
				}
				return
			}
			if err := err; err != nil {
				t.Fatalf("err: %v", err)
			}
			if got := got; got != tc.want {
				t.Errorf("got = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEncodeBody(t *testing.T) {
	t.Parallel()

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		got, err := encodeBody(nil)
		if err := err; err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != nil {
			t.Errorf("got = %v, want nil", got)
		}
	})
	t.Run("raw bytes pass-through", func(t *testing.T) {
		t.Parallel()
		got, err := encodeBody([]byte(`{"raw":1}`))
		if err := err; err != nil {
			t.Fatalf("err: %v", err)
		}
		if got := string(got); got != `{"raw":1}` {
			t.Errorf("string(got) = %v, want %v", got, `{"raw":1}`)
		}
	})
	t.Run("struct json-encoded", func(t *testing.T) {
		t.Parallel()
		got, err := encodeBody(map[string]string{"k": "v"})
		if err := err; err != nil {
			t.Fatalf("err: %v", err)
		}
		requireJSONEq(t, `{"k":"v"}`, string(got))
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
	if err == nil {
		t.Fatal("err should have failed")
	}
	// Either the first attempt errors with the context error, or the
	// retry sleep returns it. Either way the surface error must wrap
	// context.Canceled.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
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

// TestDryRun_WriteHelpers_NoRoundTrip pins the contract for every
// mutating helper: when the call is in dry-run mode, the
// transport's RoundTrip is never invoked. Each helper drives a
// different mutating method (POST / PUT / PATCH / DELETE) so a
// regression in any one of them surfaces independently.
func TestDryRun_WriteHelpers_NoRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  func(c *Client) error
	}{
		{
			name: "PostJSON",
			run: func(c *Client) error {
				return c.PostJSON(WithDryRun(context.Background()), "/tags", map[string]any{"name": "x"}, nil)
			},
		},
		{
			name: "PutJSON",
			run: func(c *Client) error {
				return c.PutJSON(WithDryRun(context.Background()), "/cards/abc", map[string]any{"name": "y"}, nil)
			},
		},
		{
			name: "PatchJSON",
			run: func(c *Client) error {
				return c.PatchJSON(WithDryRun(context.Background()), "/cards/abc", map[string]any{"name": "z"}, nil)
			},
		},
		{
			name: "DeleteJSON",
			run: func(c *Client) error {
				return c.DeleteJSON(WithDryRun(context.Background()), "/cards/abc", nil)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := NewClient(fixtureToken())
			c.BaseURL = "https://favro.invalid"
			c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

			err := tc.run(c)
			if !errors.Is(err, ErrDryRun) {
				t.Fatalf("got %v, want ErrDryRun", err)
			}
			var rec *DryRunRecord
			if !errors.As(err, &rec) {
				t.Fatalf("got %v, want rec", err)
			}
			if len(rec.Method) == 0 {
				t.Fatal("rec.Method is empty")
			}
			if !strings.Contains(rec.URL, "favro.invalid") {
				t.Errorf("rec.URL does not contain %q", "favro.invalid")
			}
			if got := rec.Headers.Get("Authorization"); got != "[REDACTED]" {
				t.Errorf("DryRunRecord must redact the Authorization header so secrets cannot leak via tool output: got %v, want %v", got, "[REDACTED]")
			}
			if got := rec.Headers.Get("organizationId"); got != "[REDACTED]" {
				t.Errorf("DryRunRecord must redact the organization id, which names the tenant: got %v, want %v", got, "[REDACTED]")
			}
			// The record composes its headers through buildRequest, so
			// an empty set here would mean that failed silently.
			if got := rec.Headers.Get("Accept"); got != "application/json" {
				t.Errorf("DryRunRecord must show the headers the real request would carry: got %v, want %v", got, "application/json")
			}
		})
	}
}

// TestDryRun_ForceDryRunOnClient_NoRoundTrip mirrors the test above
// but exercises the process-wide ForceDryRun gate (rather than the
// per-context WithDryRun). Same contract: no RoundTrip dispatched.
func TestDryRun_ForceDryRunOnClient_NoRoundTrip(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}
	c.ForceDryRun = true

	err := c.PostJSON(context.Background(), "/tags", map[string]any{"name": "x"}, nil)
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
}
