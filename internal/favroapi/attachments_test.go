package favroapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// TestUploadAttachment_HappyPath pins the wire shape: POST
// /cards/{id}/attachment?filename=foo.txt with raw bytes body and
// Content-Type=application/octet-stream (NOT application/json — the
// JSON default would trip Favro's body parser on binary content).
func TestUploadAttachment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/cards/ci-1/attachment" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1/attachment")
		}
		if got := rec.Query.Get("filename"); got != "note.txt" {
			t.Errorf("rec.Query.Get(\"filename\") = %v, want %v", got, "note.txt")
		}
		if got := rec.Headers.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("binary uploads must NOT default to application/json — Favro's body parser would 400: got %v, want %v", got, "application/octet-stream")
		}
		if got := rec.Body; got != "raw bytes here" {
			t.Errorf("rec.Body = %v, want %v", got, "raw bytes here")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"note.txt","fileURL":"https://favro.invalid/a/note.txt"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UploadAttachment(context.Background(), "ci-1", "note.txt", "", []byte("raw bytes here"))
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "note.txt" {
		t.Errorf("Favro returns the attachment object {name, fileURL}, not the favro.Card — pinning the contract: got %v, want %v", got, "note.txt")
	}
	if got := got.FileURL; got != "https://favro.invalid/a/note.txt" {
		t.Errorf("got.FileURL = %v, want %v", got, "https://favro.invalid/a/note.txt")
	}
}

// TestUploadAttachment_EmptyCardID_NoNetworkCall pins the empty-id
// short-circuit.
func TestUploadAttachment_EmptyCardID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UploadAttachment(context.Background(), "", "x.txt", "", []byte("data"))
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestUploadAttachment_EmptyFilename_NoNetworkCall pins the
// empty-filename short-circuit.
func TestUploadAttachment_EmptyFilename_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UploadAttachment(context.Background(), "ci-1", "", "", []byte("data"))
	if !errors.Is(err, errMissingFilename) {
		t.Fatalf("got %v, want errMissingFilename", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestUploadAttachment_OversizeCap pins the cap; a content blob
// over the cap surfaces as a typed error before any HTTP call.
func TestUploadAttachment_OversizeCap(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	oversized := make([]byte, UploadAttachmentMaxBytes+1)
	_, err := c.UploadAttachment(context.Background(), "ci-1", "x.txt", "", oversized)
	if err == nil {
		t.Fatal("err should have failed")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err.Error() does not contain %q", "exceeds")
	}
	if len(h.seen()) != 0 {
		t.Errorf("oversize must short-circuit before any HTTP call: got %v", h.seen())
	}
}

// TestUploadAttachment_DryRun_ReturnsRecord pins the dry-run
// contract. Body is the raw bytes; Content-Type override survives
// to the redacted DryRunRecord headers.
func TestUploadAttachment_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.UploadAttachment(WithDryRun(context.Background()), "ci-1", "x.txt", "", []byte("data"))
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
	if !strings.Contains(rec.URL, "/cards/ci-1/attachment") {
		t.Errorf("rec.URL does not contain %q", "/cards/ci-1/attachment")
	}
	if !strings.Contains(rec.URL, "filename=x.txt") {
		t.Errorf("rec.URL does not contain %q", "filename=x.txt")
	}
	if got := rec.Body; !reflect.DeepEqual(got, []byte("data")) {
		t.Errorf("rec.Body = %v, want %v", got, []byte("data"))
	}
	if got := rec.Headers.Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("rec.Headers.Get(\"Content-Type\") = %v, want %v", got, "application/octet-stream")
	}
}

// Favro hands back a presigned fileURL, re-minted on every read with
// a fresh X-Amz-Date and X-Amz-Signature (verified live). Sending it
// whole can never match what Favro stored, so RemoveAttachment must
// strip the query down to the stable object URL.
func TestRemoveAttachment_StripsPresignedQuery(t *testing.T) {
	t.Parallel()

	const objectURL = "https://favro.s3.eu-central-1.amazonaws.com/00000000-0000-0000-0000-000000000000.png"
	const presigned = objectURL + "?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Date=20260826T044924Z" +
		"&X-Amz-Expires=86400&X-Amz-Signature=deadbeef&X-Amz-SignedHeaders=host"

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/cards/ci-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/cards/ci-1")
		}
		requireJSONEq(t, `{"removeAttachments":["`+objectURL+`"]}`, rec.Body)
		if strings.Contains(rec.Body, "X-Amz-Signature") {
			t.Errorf("rec.Body unexpectedly contains %q", "X-Amz-Signature")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.RemoveAttachment(context.Background(), "ci-1", presigned)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}

// An already-stripped URL must survive untouched, and the caller's
// slice must not be mutated in place.
func TestRemoveAttachment_AlreadyCanonical(t *testing.T) {
	t.Parallel()

	const objectURL = "https://files.invalid/abc.txt"

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		requireJSONEq(t, `{"removeAttachments":["`+objectURL+`"]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	urls := []string{objectURL}
	_, err := c.RemoveAttachment(context.Background(), "ci-1", urls...)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := urls; !reflect.DeepEqual(got, ([]string{objectURL})) {
		t.Errorf("caller's slice must not be rewritten: got %v, want %v", got, []string{objectURL})
	}
}

func TestCanonicalAttachmentURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, in, want string }{
		{"strips query", "https://h/o.png?X-Amz-Signature=abc", "https://h/o.png"},
		{"no query is a no-op", "https://h/o.png", "https://h/o.png"},
		{"bare question mark", "https://h/o.png?", "https://h/o.png"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := favro.CanonicalAttachmentURL(tc.in); got != tc.want {
				t.Errorf("favro.CanonicalAttachmentURL(tc.in) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRemoveAttachment_MultipleURLs(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		requireJSONEq(t, `{"removeAttachments":["https://a.invalid/1.txt","https://a.invalid/2.txt"]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.RemoveAttachment(context.Background(), "ci-1",
		"https://a.invalid/1.txt", "https://a.invalid/2.txt")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestRemoveAttachment_NoURLs_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.RemoveAttachment(context.Background(), "ci-1")
	if err == nil || !strings.Contains(err.Error(), "at least one attachment fileURL") {
		t.Fatalf("got %v, want it to mention %q", err, "at least one attachment fileURL")
	}

	_, err = c.RemoveAttachment(context.Background(), "ci-1", "")
	if err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("got %v, want it to mention %q", err, "must not be empty")
	}

	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// The comment upload endpoint differs only in its path; pin that the
// filename and mimeType still ride on the query string.
func TestUploadCommentAttachment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/comments/cm-1/attachment" {
			t.Errorf("rec.Path = %v, want %v", got, "/comments/cm-1/attachment")
		}
		if got := rec.Query.Get("filename"); got != "note.txt" {
			t.Errorf("rec.Query.Get(\"filename\") = %v, want %v", got, "note.txt")
		}
		if got := rec.Query.Get("mimeType"); got != "text/plain" {
			t.Errorf("rec.Query.Get(\"mimeType\") = %v, want %v", got, "text/plain")
		}
		if got := rec.Headers.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("rec.Headers.Get(\"Content-Type\") = %v, want %v", got, "application/octet-stream")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"note.txt","fileURL":"https://s3.invalid/note.txt"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UploadCommentAttachment(context.Background(), "cm-1", "note.txt", "text/plain", []byte("data"))
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "note.txt" {
		t.Errorf("got.Name = %v, want %v", got, "note.txt")
	}
	if got := got.FileURL; got != "https://s3.invalid/note.txt" {
		t.Errorf("got.FileURL = %v, want %v", got, "https://s3.invalid/note.txt")
	}
}

func TestUploadCommentAttachment_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UploadCommentAttachment(context.Background(), "", "x.txt", "", []byte("data"))
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// mimeType is optional — omitting it must leave the query parameter
// off entirely so Favro falls back to extension sniffing.
func TestUploadAttachment_OmitsEmptyMimeType(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if rec.Query.Has("mimeType") {
			t.Error("rec.Query.Has(\"mimeType\") = true, want false")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"x.txt"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UploadAttachment(context.Background(), "ci-1", "x.txt", "", []byte("data"))
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}

// TestBuildRequest_ContentTypeOverride pins the regression: a
// caller-supplied Content-Type via WithHeader fully overrides the
// JSON default (rather than producing a comma-joined "application/
// json, application/octet-stream" pair, which would 400 on the
// server side).
func TestBuildRequest_ContentTypeOverride(t *testing.T) {
	t.Parallel()

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Content-Type")
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	resp, err := c.Do(
		context.Background(),
		http.MethodPost,
		"/anywhere",
		nil,
		[]byte("raw"),
		WithHeader("Content-Type", "application/octet-stream"),
	)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	t.Cleanup(func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	})
	if got := got; got != "application/octet-stream" {
		t.Errorf("caller-supplied Content-Type must REPLACE the JSON default; comma-joined header would 400: got %v, want %v", got, "application/octet-stream")
	}
	if strings.Contains(got, "json") {
		t.Errorf("got unexpectedly contains %q", "json")
	}
}
