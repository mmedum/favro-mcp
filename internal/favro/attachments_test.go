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

	got, err := c.UploadAttachment(context.Background(), "ci-1", "note.txt", []byte("raw bytes here"))
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

	_, err := c.UploadAttachment(context.Background(), "", "x.txt", []byte("data"))
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

	_, err := c.UploadAttachment(context.Background(), "ci-1", "", []byte("data"))
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
	_, err := c.UploadAttachment(context.Background(), "ci-1", "x.txt", oversized)
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

	_, err := c.UploadAttachment(WithDryRun(context.Background()), "ci-1", "x.txt", []byte("data"))
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
	require.Contains(t, rec.URL, "/cards/ci-1/attachment")
	require.Contains(t, rec.URL, "filename=x.txt")
	require.Equal(t, []byte("data"), rec.Body)
	require.Equal(t, "application/octet-stream", rec.Headers.Get("Content-Type"))
}

// TestRemoveAttachment_HappyPath pins that RemoveAttachment is a
// thin wrapper over UpdateCard with removeAttachments populated.
func TestRemoveAttachment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/cards/ci-1", rec.Path)
		require.JSONEq(t, `{"removeAttachments":["note.txt"]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.RemoveAttachment(context.Background(), "ci-1", "note.txt")
	require.NoError(t, err)
}

func TestRemoveAttachment_EmptyFilename_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.RemoveAttachment(context.Background(), "ci-1", "")
	require.ErrorIs(t, err, errMissingFilename)
	require.Empty(t, h.seen())
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
