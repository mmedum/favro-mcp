package favroapi

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
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
	if got := s.Limit; got != 1000 {
		t.Errorf("s.Limit = %v, want %v", got, 1000)
	}
	if got := s.Remaining; got != 850 {
		t.Errorf("s.Remaining = %v, want %v", got, 850)
	}
	if got := s.Reset.Unix(); got != time.Unix(resetEpoch, 0).Unix() {
		t.Errorf("s.Reset.Unix() = %v, want %v", got, time.Unix(resetEpoch, 0).Unix())
	}
	if got := s.Status; got != http.StatusOK {
		t.Errorf("s.Status = %v, want %v", got, http.StatusOK)
	}
	if got := s.Path; got != "/cards" {
		t.Errorf("s.Path = %v, want %v", got, "/cards")
	}
	if s.RetryAfter != 0 {
		t.Errorf("s.RetryAfter = %v, want 0", s.RetryAfter)
	}
}

func TestParseRateLimitHeaders_AbsentHeadersZero(t *testing.T) {
	t.Parallel()

	//nolint:bodyclose // synthetic *http.Response, Body is nil — nothing to close
	resp := newRespForRateLimitTest(nil, http.StatusOK, "/x")
	s := parseRateLimitHeaders(resp)
	if got := s.Limit; got != 0 {
		t.Errorf("s.Limit = %v, want %v", got, 0)
	}
	if got := s.Remaining; got != -1 {
		t.Errorf("absent must stay distinguishable from 0: got %v, want %v", got, -1)
	}
	if !s.Reset.IsZero() {
		t.Error("s.Reset.IsZero() = false, want true")
	}
	if s.RetryAfter != 0 {
		t.Errorf("s.RetryAfter = %v, want 0", s.RetryAfter)
	}
}

func TestParseRetryAfter_SecondsForm(t *testing.T) {
	t.Parallel()

	if got := parseRetryAfter("5"); got != 5*time.Second {
		t.Errorf("parseRetryAfter(\"5\") = %v, want %v", got, 5*time.Second)
	}
	if got := parseRetryAfter(""); got != 0*time.Second {
		t.Errorf("parseRetryAfter(\"\") = %v, want %v", got, 0*time.Second)
	}
	if got := parseRetryAfter("not-a-number"); got != 0*time.Second {
		t.Errorf("parseRetryAfter(\"not-a-number\") = %v, want %v", got, 0*time.Second)
	}
}

func TestParseRetryAfter_HTTPDateForm(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	// Allow some slack — clock may have advanced between format and parse.
	if got <= 0 || got > 3*time.Second {
		t.Errorf("want positive ≤ 3s, got %v", got)
	}

	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if parseRetryAfter(past) != 0 {
		t.Errorf("parseRetryAfter(past) = %v, want 0", parseRetryAfter(past))
	}
}

func TestRateLimitTracker_RecordAndLatest(t *testing.T) {
	t.Parallel()

	tr := &rateLimitTracker{}
	_, ok := tr.latest()
	if ok {
		t.Error("fresh tracker has no snapshot")
	}

	first := RateLimitSnapshot{Limit: 100, Remaining: 99, Path: "/a", Status: 200, ObservedAt: time.Now()}
	tr.record(first)
	got, ok := tr.latest()
	if !ok {
		t.Error("ok = false, want true")
	}
	if got := got; got != first {
		t.Errorf("got = %v, want %v", got, first)
	}

	second := RateLimitSnapshot{Limit: 100, Remaining: 50, Path: "/b", Status: 200, ObservedAt: time.Now()}
	tr.record(second)
	got, ok = tr.latest()
	if !ok {
		t.Error("ok = false, want true")
	}
	if got := got; got != second {
		t.Errorf("latest must reflect the most recent record: got %v, want %v", got, second)
	}
}
