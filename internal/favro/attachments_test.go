package favro

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUploadAttachment_HappyPath pins the wire shape: POST
// /cards/{id}/attachment?filename=foo.txt with raw bytes body and
// Content-Type=application/octet-stream (NOT application/json — the
// JSON default would trip Favro's body parser on binary content).
func TestUploadAttachment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/cards/ci-1/attachment", rec.Path)
		require.Equal(t, "note.txt", rec.Query.Get("filename"))
		require.Equal(t, "application/octet-stream", rec.Headers.Get("Content-Type"),
			"binary uploads must NOT default to application/json — Favro's body parser would 400")
		require.Equal(t, "raw bytes here", rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"note.txt","fileURL":"https://favro.invalid/a/note.txt"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UploadAttachment(context.Background(), "ci-1", "note.txt", "", []byte("raw bytes here"))
	require.NoError(t, err)
	require.Equal(t, "note.txt", got.Name,
		"Favro returns the attachment object {name, fileURL}, not the Card — pinning the contract")
	require.Equal(t, "https://favro.invalid/a/note.txt", got.FileURL)
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
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
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
	require.ErrorIs(t, err, errMissingFilename)
	require.Empty(t, h.seen())
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
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
	require.Empty(t, h.seen(), "oversize must short-circuit before any HTTP call")
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
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
	require.Contains(t, rec.URL, "/cards/ci-1/attachment")
	require.Contains(t, rec.URL, "filename=x.txt")
	require.Equal(t, []byte("data"), rec.Body)
	require.Equal(t, "application/octet-stream", rec.Headers.Get("Content-Type"))
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
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/cards/ci-1", rec.Path)
		require.JSONEq(t, `{"removeAttachments":["`+objectURL+`"]}`, rec.Body)
		require.NotContains(t, rec.Body, "X-Amz-Signature")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.RemoveAttachment(context.Background(), "ci-1", presigned)
	require.NoError(t, err)
}

// An already-stripped URL must survive untouched, and the caller's
// slice must not be mutated in place.
func TestRemoveAttachment_AlreadyCanonical(t *testing.T) {
	t.Parallel()

	const objectURL = "https://files.invalid/abc.txt"

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.JSONEq(t, `{"removeAttachments":["`+objectURL+`"]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	urls := []string{objectURL}
	_, err := c.RemoveAttachment(context.Background(), "ci-1", urls...)
	require.NoError(t, err)
	require.Equal(t, []string{objectURL}, urls, "caller's slice must not be rewritten")
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
			require.Equal(t, tc.want, CanonicalAttachmentURL(tc.in))
		})
	}
}

func TestRemoveAttachment_MultipleURLs(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.JSONEq(t, `{"removeAttachments":["https://a.invalid/1.txt","https://a.invalid/2.txt"]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.RemoveAttachment(context.Background(), "ci-1",
		"https://a.invalid/1.txt", "https://a.invalid/2.txt")
	require.NoError(t, err)
}

func TestRemoveAttachment_NoURLs_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.RemoveAttachment(context.Background(), "ci-1")
	require.ErrorContains(t, err, "at least one attachment fileURL")

	_, err = c.RemoveAttachment(context.Background(), "ci-1", "")
	require.ErrorContains(t, err, "must not be empty")

	require.Empty(t, h.seen())
}

// The comment upload endpoint differs only in its path; pin that the
// filename and mimeType still ride on the query string.
func TestUploadCommentAttachment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/comments/cm-1/attachment", rec.Path)
		require.Equal(t, "note.txt", rec.Query.Get("filename"))
		require.Equal(t, "text/plain", rec.Query.Get("mimeType"))
		require.Equal(t, "application/octet-stream", rec.Headers.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"note.txt","fileURL":"https://s3.invalid/note.txt"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UploadCommentAttachment(context.Background(), "cm-1", "note.txt", "text/plain", []byte("data"))
	require.NoError(t, err)
	require.Equal(t, "note.txt", got.Name)
	require.Equal(t, "https://s3.invalid/note.txt", got.FileURL)
}

func TestUploadCommentAttachment_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UploadCommentAttachment(context.Background(), "", "x.txt", "", []byte("data"))
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

// mimeType is optional — omitting it must leave the query parameter
// off entirely so Favro falls back to extension sniffing.
func TestUploadAttachment_OmitsEmptyMimeType(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.False(t, rec.Query.Has("mimeType"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"x.txt"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UploadAttachment(context.Background(), "ci-1", "x.txt", "", []byte("data"))
	require.NoError(t, err)
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
	require.NoError(t, err)
	t.Cleanup(func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	})
	require.Equal(t, "application/octet-stream", got,
		"caller-supplied Content-Type must REPLACE the JSON default; comma-joined header would 400")
	require.NotContains(t, got, "json")
}
