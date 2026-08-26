package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListComments_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Comment]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cm",
			Entities: []Comment{
				{
					CommentID:    "cm-1",
					CardCommonID: "card-c-1",
					UserID:       "u-1",
					Body:         "initial thought",
					Created:      "2026-01-02T03:04:05.000Z",
				},
				{
					CommentID:    "cm-2",
					CardCommonID: "card-c-1",
					UserID:       "u-2",
					Body:         "follow-up",
					Created:      "2026-01-02T04:00:00.000Z",
					LastUpdated:  "2026-01-02T04:30:00.000Z",
				},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListComments(context.Background(), 0, "", "card-c-1")
	require.NoError(t, err)
	require.Equal(t, "req-cm", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "initial thought", env.Entities[0].Body)
	require.Equal(t, "follow-up", env.Entities[1].Body)
	require.Equal(t, "2026-01-02T04:30:00.000Z", env.Entities[1].LastUpdated)

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/comments", rec[0].Path)
	require.Equal(t, "card-c-1", rec[0].Query.Get("cardCommonId"))
	require.Empty(t, rec[0].Query.Get("page"), "page=0 must NOT add ?page=")
}

func TestListComments_EmptyCardCommonID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListComments(context.Background(), 0, "", "")
	require.ErrorIs(t, err, errMissingCardCommonID)
	require.Empty(t, h.seen(), "no HTTP call must be made for empty cardCommonID")
}

func TestListComments_WithPageForwardsRequestIDAndFilter(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Comment]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListComments(context.Background(), 2, "req-prior", "card-c-1")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "card-c-1", rec[0].Query.Get("cardCommonId"),
		"cardCommonID must be re-sent on every paginated page")
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Comment{
			CommentID:    "cm-zzz",
			CardCommonID: "card-c-1",
			UserID:       "u-1",
			Body:         "looked up",
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	cm, err := c.GetComment(context.Background(), "cm-zzz")
	require.NoError(t, err)
	require.Equal(t, "cm-zzz", cm.CommentID)
	require.Equal(t, "looked up", cm.Body)

	rec := h.seen()
	require.Equal(t, "/comments/cm-zzz", rec[0].Path)
}

func TestGetComment_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetComment(context.Background(), "")
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestGetComment_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetComment(context.Background(), "missing")
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}

// TestListComments_AttachmentsDecoding pins decode for the
// Phase 4.5 addition of Comment.Attachments.
func TestListComments_AttachmentsDecoding(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":0,"pages":1,"requestId":"r","entities":[{
			"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"x",
			"attachments":[
				{"name":"diagram.png","fileURL":"https://favro.invalid/c/diagram.png"}
			]
		}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListComments(context.Background(), 0, "", "cc-1")
	require.NoError(t, err)
	require.Len(t, env.Entities, 1)
	require.Len(t, env.Entities[0].Attachments, 1)
	require.Equal(t, "diagram.png", env.Entities[0].Attachments[0].Name)
}

// TestCreateComment_HappyPath pins POST /comments → Comment back.
func TestCreateComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/comments", rec.Path)
		require.JSONEq(t, `{"cardCommonId":"cc-1","comment":"first"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"first"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateComment(context.Background(), CreateCommentRequest{CardCommonID: "cc-1", Comment: "first"})
	require.NoError(t, err)
	require.Equal(t, "cm-1", got.CommentID)
}

// TestCreateComment_RequiredFields short-circuits before any HTTP
// call when cardCommonId or comment is empty.
func TestCreateComment_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  CreateCommentRequest
	}{
		{"empty card_common_id", CreateCommentRequest{Comment: "x"}},
		{"empty comment", CreateCommentRequest{CardCommonID: "cc-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			c := newTestClient(srv)

			_, err := c.CreateComment(context.Background(), tc.req)
			require.Error(t, err)
			require.Empty(t, h.seen())
		})
	}
}

// TestCreateComment_DryRun_ReturnsRecord pins the dry-run contract.
func TestCreateComment_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateComment(WithDryRun(context.Background()), CreateCommentRequest{CardCommonID: "cc-1", Comment: "x"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
	require.Contains(t, rec.URL, "/comments")
}

// TestUpdateComment_HappyPath pins PUT /comments/{id}.
func TestUpdateComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/comments/cm-1", rec.Path)
		require.JSONEq(t, `{"comment":"edited"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"edited","lastUpdated":"2026-05-05T10:00:00Z"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateComment(context.Background(), "cm-1", UpdateCommentRequest{Comment: "edited"})
	require.NoError(t, err)
	require.Equal(t, "edited", got.Body)
	require.NotEmpty(t, got.LastUpdated)
}

// TestUpdateComment_EmptyID_NoNetworkCall pins the empty-id guard.
func TestUpdateComment_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateComment(context.Background(), "", UpdateCommentRequest{Comment: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

// TestDeleteComment_HappyPath pins DELETE /comments/{id} → 204.
func TestDeleteComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		require.Equal(t, "/comments/cm-1", rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.NoError(t, c.DeleteComment(context.Background(), "cm-1"))
}

// TestDeleteComment_EmptyID_NoNetworkCall pins the empty-id guard.
func TestDeleteComment_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.ErrorIs(t, c.DeleteComment(context.Background(), ""), errMissingID)
	require.Empty(t, h.seen())
}

// Comment attachments carry the same presigned fileURL as card
// attachments, so UpdateComment strips the query too.
func TestUpdateComment_StripsPresignedAttachmentQuery(t *testing.T) {
	t.Parallel()

	const objectURL = "https://favro.s3.eu-central-1.amazonaws.com/11111111-1111-1111-1111-111111111111.gif"

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.JSONEq(t, `{"comment":"edited","removeAttachments":["`+objectURL+`"]}`, rec.Body)
		require.NotContains(t, rec.Body, "X-Amz-Signature")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","comment":"edited"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateComment(context.Background(), "cm-1", UpdateCommentRequest{
		Comment:           "edited",
		RemoveAttachments: []string{objectURL + "?X-Amz-Signature=deadbeef&X-Amz-Expires=86400"},
	})
	require.NoError(t, err)
}
