package favro

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newRespForRateLimitTest(headers map[string]string, status int, path string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Request: &http.Request{
			URL: &url.URL{Path: path},
		},
	}
}

func TestParseRateLimitHeaders_AllHeadersPresent(t *testing.T) {
	t.Parallel()

	resetEpoch := time.Now().Add(time.Hour).Unix()
	//nolint:bodyclose // synthetic *http.Response, Body is nil — nothing to close
	resp := newRespForRateLimitTest(map[string]string{
		headerRateLimitLimit:     "1000",
		headerRateLimitRemaining: "850",
		headerRateLimitReset:     strconv.FormatInt(resetEpoch, 10),
	}, http.StatusOK, "/cards")

	s := parseRateLimitHeaders(resp)
	require.Equal(t, 1000, s.Limit)
	require.Equal(t, 850, s.Remaining)
	require.Equal(t, time.Unix(resetEpoch, 0).Unix(), s.Reset.Unix())
	require.Equal(t, http.StatusOK, s.Status)
	require.Equal(t, "/cards", s.Path)
	require.Zero(t, s.RetryAfter, "Retry-After should only be populated on 429")
}

func TestParseRateLimitHeaders_AbsentHeadersZero(t *testing.T) {
	t.Parallel()

	//nolint:bodyclose // synthetic *http.Response, Body is nil — nothing to close
	resp := newRespForRateLimitTest(nil, http.StatusOK, "/x")
	s := parseRateLimitHeaders(resp)
	require.Equal(t, 0, s.Limit)
	require.Equal(t, -1, s.Remaining, "absent must stay distinguishable from 0")
	require.True(t, s.Reset.IsZero())
	require.Zero(t, s.RetryAfter)
}

func TestParseRetryAfter_SecondsForm(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5*time.Second, parseRetryAfter("5"))
	require.Equal(t, 0*time.Second, parseRetryAfter(""))
	require.Equal(t, 0*time.Second, parseRetryAfter("not-a-number"))
}

func TestParseRetryAfter_HTTPDateForm(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	// Allow some slack — clock may have advanced between format and parse.
	require.True(t, got > 0 && got <= 3*time.Second, "want positive ≤ 3s, got %v", got)

	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	require.Zero(t, parseRetryAfter(past), "past timestamps must clamp to zero")
}

func TestRateLimitTracker_RecordAndLatest(t *testing.T) {
	t.Parallel()

	tr := &rateLimitTracker{}
	_, ok := tr.latest()
	require.False(t, ok, "fresh tracker has no snapshot")

	first := RateLimitSnapshot{Limit: 100, Remaining: 99, Path: "/a", Status: 200, ObservedAt: time.Now()}
	tr.record(first)
	got, ok := tr.latest()
	require.True(t, ok)
	require.Equal(t, first, got)

	second := RateLimitSnapshot{Limit: 100, Remaining: 50, Path: "/b", Status: 200, ObservedAt: time.Now()}
	tr.record(second)
	got, ok = tr.latest()
	require.True(t, ok)
	require.Equal(t, second, got, "latest must reflect the most recent record")
}
