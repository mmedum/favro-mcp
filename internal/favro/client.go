package favro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmedum/favro-mcp/internal/auth"
)

const (
	// DefaultBaseURL is the production Favro REST API base URL.
	DefaultBaseURL = "https://favro.com/api/v1"
	// defaultTimeout caps any single request (including retries
	// re-using the parent context). 30s is generous for a write
	// operation against a live API.
	defaultTimeout = 30 * time.Second
	// rateLimitRetryCap is the upper bound on the Retry-After-driven
	// sleep before we give up and surface RateLimitError. Per plan §7.
	rateLimitRetryCap = 30 * time.Second
	// transientMaxAttempts is the total attempt count for 5xx responses
	// (1 initial + 2 retries). Backoff schedule is below.
	transientMaxAttempts = 3
	// maxBodyBytes caps the per-error body excerpt we keep in memory
	// for ValidationError / APIError. Favro responses are small.
	maxBodyBytes = 4 * 1024
	// redactedValue is the sentinel substituted for sensitive header
	// values in debug logs and dry-run records.
	redactedValue = "[REDACTED]"
)

// transientBackoffSchedule[i] is the sleep before attempt i+1.
// Indices are 0-based; the schedule has transientMaxAttempts-1 entries.
var transientBackoffSchedule = []time.Duration{
	250 * time.Millisecond,
	1 * time.Second,
	4 * time.Second,
}

// Client is an authenticated, retrying Favro REST client with
// rate-limit observation and a per-request dry-run gate. Higher-level
// resource methods compose Do for their wire-level concerns.
type Client struct {
	// BaseURL overrides the default Favro base URL. Tests point it
	// at httptest.Server; production leaves it empty.
	BaseURL string
	// HTTPClient overrides the default *http.Client. Empty means a
	// fresh client with defaultTimeout.
	HTTPClient *http.Client
	// Token is applied to every request via auth.Token.Apply.
	Token auth.Token
	// UserAgent is sent on every request. Empty means a sensible default.
	UserAgent string
	// ForceDryRun, when true, short-circuits every mutating request
	// (POST / PUT / DELETE / PATCH) and returns ErrDryRun together
	// with a *DryRunRecord describing the call that would have been
	// made. Set by the binary's --dry-run flag.
	ForceDryRun bool

	rl *rateLimitTracker
}

// NewClient constructs a Client with sensible defaults.
func NewClient(tok auth.Token) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: defaultTimeout},
		Token:      tok,
		UserAgent:  "favro-mcp/client",
		rl:         &rateLimitTracker{},
	}
}

// LatestRateLimit exposes the most-recent RateLimitSnapshot. Returns
// (zero, false) if no Favro request has been made yet.
func (c *Client) LatestRateLimit() (RateLimitSnapshot, bool) {
	if c.rl == nil {
		return RateLimitSnapshot{}, false
	}
	return c.rl.latest()
}

// dryRunCtxKey is the context key for per-request dry-run.
type dryRunCtxKey struct{}

// WithDryRun returns a context that opts in to dry-run for any
// mutating request executed under it. The Client's ForceDryRun field
// also opts in process-wide; either is sufficient.
func WithDryRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, dryRunCtxKey{}, true)
}

// IsDryRun reports whether ctx is in dry-run mode.
func IsDryRun(ctx context.Context) bool {
	v, _ := ctx.Value(dryRunCtxKey{}).(bool)
	return v
}

// ErrDryRun is returned by Do alongside a *DryRunRecord when the call
// was short-circuited because dry-run is in effect. Callers detect it
// with errors.Is.
var ErrDryRun = errors.New("favro: dry-run — request not sent")

// DryRunRecord describes a request that would have been sent in
// non-dry-run mode. Returned via the (*Response, error) pair: response
// is nil and the error wraps ErrDryRun with a *DryRunRecord
// accessible via errors.As.
type DryRunRecord struct {
	Method  string
	URL     string
	Headers http.Header // Authorization is redacted before storage.
	Body    []byte      // raw request body if any.
}

func (r *DryRunRecord) Error() string {
	return fmt.Sprintf("favro: dry-run %s %s — request not sent", r.Method, r.URL)
}

func (r *DryRunRecord) Unwrap() error { return ErrDryRun }

// RequestOption customizes a single Do call. Composed via
// WithHeader / WithHeaders to inject extra headers (the most common
// case is X-Favro-Backend-Identifier on paginated requests).
type RequestOption func(*requestConfig)

type requestConfig struct {
	headers http.Header
}

// WithHeader adds a single request header. Repeating the same name
// appends to the existing list.
func WithHeader(name, value string) RequestOption {
	return func(cfg *requestConfig) {
		if cfg.headers == nil {
			cfg.headers = http.Header{}
		}
		cfg.headers.Add(name, value)
	}
}

// Do executes an authenticated request with retry and rate-limit
// observation. body may be nil; if not nil it is JSON-encoded.
//
// The returned *http.Response, when non-nil, has its body still open;
// callers must close it. On error the response is nil and one of the
// typed error kinds (AuthError / RateLimitError / NotFoundError /
// ValidationError / TransientError / APIError / *DryRunRecord) is
// returned.
//
// Retry policy (per plan §7):
//   - 429: single retry honoring Retry-After capped at 30s, then
//     RateLimitError.
//   - 5xx: exponential backoff (250ms, 1s, 4s), max 3 attempts total.
//   - 401 / 403 / 404: never retried.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any, opts ...RequestOption) (*http.Response, error) {
	method = strings.ToUpper(method)

	encodedBody, err := encodeBody(body)
	if err != nil {
		return nil, fmt.Errorf("encode request body: %w", err)
	}

	cfg := requestConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	fullURL, err := joinURL(base, path, query)
	if err != nil {
		return nil, err
	}

	if shouldDryRun(c, ctx, method) {
		return nil, c.buildDryRunRecord(method, fullURL, encodedBody, cfg.headers)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	resp, err := c.execute(ctx, httpClient, method, fullURL, encodedBody, cfg.headers)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// execute drives the retry loop. The loop is hand-rolled rather than
// using a middleware pattern because the retry conditions are tightly
// coupled to typed-error mapping.
func (c *Client) execute(ctx context.Context, httpClient *http.Client, method, fullURL string, encodedBody []byte, extra http.Header) (*http.Response, error) {
	for attempt := 1; attempt <= transientMaxAttempts; attempt++ {
		resp, err := c.attempt(ctx, httpClient, method, fullURL, encodedBody, extra, attempt)
		if err != nil {
			return nil, err
		}
		// resp == nil + err == nil means "retry; the inner handler
		// already drained the previous response and slept the backoff".
		if resp != nil {
			return resp, nil
		}
	}
	// Loop body always returns; reaching here means handle5xx /
	// handle429 returned (nil retry-signal) on the final iteration,
	// which they're documented not to do. A panic catches a future
	// regression louder than a fabricated TransientError would.
	panic("favro: execute loop fell through; handler returned retry signal on final attempt")
}

// attempt sends one request and classifies the result. Returns:
//   - (resp, nil) on a 2xx response — caller owns the body.
//   - (nil, err) on a terminal error.
//   - (nil, nil) when the caller should retry (5xx within budget, or
//     a 429 with Retry-After ≤ cap on attempt 1).
func (c *Client) attempt(ctx context.Context, httpClient *http.Client, method, fullURL string, encodedBody []byte, extra http.Header, attempt int) (*http.Response, error) {
	req, err := c.buildRequest(ctx, method, fullURL, encodedBody, extra)
	if err != nil {
		return nil, err
	}
	c.logRequest(req, attempt)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("favro %s %s: %w", method, redactPath(fullURL), err)
	}
	if c.rl != nil {
		c.rl.record(parseRateLimitHeaders(resp))
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, c.handle429(ctx, resp, attempt)
	}
	if resp.StatusCode >= 500 {
		return nil, c.handle5xx(ctx, resp, attempt)
	}
	return nil, classifyClientError(resp)
}

// handle429 implements the single-retry-on-429 policy. Returns nil to
// signal "retry"; otherwise a RateLimitError. The response body is
// always drained.
func (c *Client) handle429(ctx context.Context, resp *http.Response, attempt int) error {
	retryAfterRaw := resp.Header.Get(headerRetryAfter)
	retryAfter := parseRetryAfter(retryAfterRaw)
	drainAndClose(resp)
	// Retry once when the server gave us a Retry-After we can actually
	// wait out (≤ cap). A header of "0" means "retry immediately";
	// missing header falls through to the typed error so the caller
	// decides the policy.
	if attempt == 1 && retryAfterRaw != "" && retryAfter <= rateLimitRetryCap {
		return sleepCtx(ctx, retryAfter)
	}
	return &RateLimitError{RetryAfter: retryAfter, Status: resp.StatusCode}
}

// handle5xx implements the exponential-backoff retry policy for 5xx.
// Returns nil to signal "retry"; otherwise a TransientError.
func (c *Client) handle5xx(ctx context.Context, resp *http.Response, attempt int) error {
	drainAndClose(resp)
	if attempt >= transientMaxAttempts {
		return &TransientError{Status: resp.StatusCode, Attempts: attempt}
	}
	return sleepCtx(ctx, transientBackoffSchedule[attempt-1])
}

// buildRequest composes a fresh *http.Request for one attempt. The body
// is wrapped in bytes.NewReader so retries see the full payload. extra
// headers (e.g. X-Favro-Backend-Identifier on paginated calls) are
// applied last and override any default we set above.
func (c *Client) buildRequest(ctx context.Context, method, fullURL string, encodedBody []byte, extra http.Header) (*http.Request, error) {
	var bodyReader io.Reader = http.NoBody
	if len(encodedBody) > 0 {
		bodyReader = bytes.NewReader(encodedBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	c.Token.Apply(req)
	req.Header.Set("Accept", "application/json")
	if len(encodedBody) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if ua := c.UserAgent; ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	return req, nil
}

// classifyClientError maps a non-retryable client status to a typed error.
func classifyClientError(resp *http.Response) error {
	body := readErrorBody(resp)
	defer drainAndClose(resp)
	path := ""
	if resp.Request != nil && resp.Request.URL != nil {
		path = resp.Request.URL.Path
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &AuthError{Status: resp.StatusCode}
	case http.StatusNotFound:
		return &NotFoundError{Path: path}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return &ValidationError{Status: resp.StatusCode, Body: body}
	default:
		return &APIError{Status: resp.StatusCode, Body: body, Path: path}
	}
}

// shouldDryRun returns true for mutating methods when the client or the
// context has dry-run enabled. GETs always go through.
func shouldDryRun(c *Client, ctx context.Context, method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return c.ForceDryRun || IsDryRun(ctx)
	default:
		return false
	}
}

// buildDryRunRecord builds a redacted *DryRunRecord for a
// short-circuited request. fullURL must be the already-validated URL
// (Do runs joinURL before deciding to dry-run, so we never have to
// re-derive or risk swallowing an invalid-URL error here).
func (c *Client) buildDryRunRecord(method, fullURL string, body []byte, extra http.Header) *DryRunRecord {
	hdr := http.Header{}
	if c.UserAgent != "" {
		hdr.Set("User-Agent", c.UserAgent)
	}
	if c.Token.OrganizationID != "" {
		hdr.Set("organizationId", c.Token.OrganizationID)
	}
	hdr.Set("Authorization", redactedValue)
	if len(body) > 0 {
		hdr.Set("Content-Type", "application/json")
	}
	for k, vs := range extra {
		for _, v := range vs {
			hdr.Add(k, v)
		}
	}
	return &DryRunRecord{
		Method:  method,
		URL:     fullURL,
		Headers: hdr,
		Body:    body,
	}
}

// joinURL composes the request URL from base + path + query.
func joinURL(base, path string, query url.Values) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	if path == "" {
		return "", errors.New("favro: empty path")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

// encodeBody serializes body to JSON, returning nil for nil/empty.
func encodeBody(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	if raw, ok := body.([]byte); ok {
		return raw, nil
	}
	return json.Marshal(body)
}

// readErrorBody reads up to maxBodyBytes from resp.Body and returns it
// as a string. Used to enrich ValidationError / APIError messages.
func readErrorBody(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	limited := io.LimitReader(resp.Body, maxBodyBytes)
	b, _ := io.ReadAll(limited)
	return strings.TrimSpace(string(b))
}

// drainAndClose discards remaining body bytes and closes the body so
// the underlying connection can be reused.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// sleepCtx sleeps for d unless ctx fires first. Returns the context
// error if it does.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// logRequest emits a redacted slog.Debug line for one attempt.
func (c *Client) logRequest(req *http.Request, attempt int) {
	if !slog.Default().Enabled(req.Context(), slog.LevelDebug) {
		return
	}
	// gosec G706 flags request-derived data flowing into log output;
	// the path / query / headers all originate from code-controlled
	// input here (token-derived org id, the caller's chosen path) and
	// the Authorization header is redacted by redactHeaders.
	slog.Debug("favro request", //nolint:gosec
		"method", req.Method,
		"path", req.URL.Path,
		"query", req.URL.RawQuery,
		"attempt", attempt,
		"headers", redactHeaders(req.Header),
	)
}

// redactHeaders returns a copy of h with sensitive header values
// replaced. The original is not mutated. Used in slog debug lines and
// in DryRunRecord.
func redactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if isSensitiveHeader(k) {
			out[k] = redactedValue
			continue
		}
		out[k] = strings.Join(v, ",")
	}
	return out
}

// isSensitiveHeader is the canonical "should this header be redacted"
// rule. Authorization is the obvious one; Cookie / Set-Cookie are
// included defensively even though Favro doesn't use them.
func isSensitiveHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "Cookie", "Set-Cookie", "Proxy-Authorization":
		return true
	}
	return false
}

// redactPath strips the query string from a URL for use in error
// messages — pagination cursors and request IDs aren't load-bearing
// for diagnostics.
func redactPath(full string) string {
	if i := strings.Index(full, "?"); i >= 0 {
		return full[:i]
	}
	return full
}
