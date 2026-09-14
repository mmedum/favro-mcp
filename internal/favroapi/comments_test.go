package favroapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListComments_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Comment]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-cm",
			Entities: []favro.Comment{
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-cm" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-cm")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Body; got != "initial thought" {
		t.Errorf("env.Entities[0].Body = %v, want %v", got, "initial thought")
	}
	if got := env.Entities[1].Body; got != "follow-up" {
		t.Errorf("env.Entities[1].Body = %v, want %v", got, "follow-up")
	}
	if got := env.Entities[1].LastUpdated; got != "2026-01-02T04:30:00.000Z" {
		t.Errorf("env.Entities[1].LastUpdated = %v, want %v", got, "2026-01-02T04:30:00.000Z")
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/comments" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/comments")
	}
	if got := rec[0].Query.Get("cardCommonId"); got != "card-c-1" {
		t.Errorf("rec[0].Query.Get(\"cardCommonId\") = %v, want %v", got, "card-c-1")
	}
	if len(rec[0].Query.Get("page")) != 0 {
		t.Errorf("page=0 must NOT add ?page=: got %v", rec[0].Query.Get("page"))
	}
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
	if !errors.Is(err, errMissingCardCommonID) {
		t.Fatalf("got %v, want errMissingCardCommonID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("no HTTP call must be made for empty cardCommonID: got %v", h.seen())
	}
}

func TestListComments_WithPageForwardsRequestIDAndFilter(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Comment]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListComments(context.Background(), 2, "req-prior", "card-c-1")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("page"); got != "2" {
		t.Errorf("rec[0].Query.Get(\"page\") = %v, want %v", got, "2")
	}
	if got := rec[0].Query.Get("cardCommonId"); got != "card-c-1" {
		t.Errorf("cardCommonID must be re-sent on every paginated page: got %v, want %v", got, "card-c-1")
	}
	if got := rec[0].Headers.Get(headerRequestID); got != "req-prior" {
		t.Errorf("rec[0].Headers.Get(headerRequestID) = %v, want %v", got, "req-prior")
	}
}

func TestGetComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Comment{
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := cm.CommentID; got != "cm-zzz" {
		t.Errorf("cm.CommentID = %v, want %v", got, "cm-zzz")
	}
	if got := cm.Body; got != "looked up" {
		t.Errorf("cm.Body = %v, want %v", got, "looked up")
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/comments/cm-zzz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/comments/cm-zzz")
	}
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
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
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
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestListComments_AttachmentsDecoding pins decode for the
// Phase 4.5 addition of favro.Comment.Attachments.
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
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(env.Entities) != 1 {
		t.Fatalf("len(env.Entities) = %d, want 1", len(env.Entities))
	}
	if len(env.Entities[0].Attachments) != 1 {
		t.Fatalf("len(env.Entities[0].Attachments) = %d, want 1", len(env.Entities[0].Attachments))
	}
	if got := env.Entities[0].Attachments[0].Name; got != "diagram.png" {
		t.Errorf("env.Entities[0].Attachments[0].Name = %v, want %v", got, "diagram.png")
	}
}

// TestCreateComment_HappyPath pins POST /comments → favro.Comment back.
func TestCreateComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/comments" {
			t.Errorf("rec.Path = %v, want %v", got, "/comments")
		}
		requireJSONEq(t, `{"cardCommonId":"cc-1","comment":"first"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"first"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateComment(context.Background(), favro.CreateCommentRequest{CardCommonID: "cc-1", Comment: "first"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.CommentID; got != "cm-1" {
		t.Errorf("got.CommentID = %v, want %v", got, "cm-1")
	}
}

// TestCreateComment_RequiredFields short-circuits before any HTTP
// call when cardCommonId or comment is empty.
func TestCreateComment_RequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  favro.CreateCommentRequest
	}{
		{"empty card_common_id", favro.CreateCommentRequest{Comment: "x"}},
		{"empty comment", favro.CreateCommentRequest{CardCommonID: "cc-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			c := newTestClient(srv)

			_, err := c.CreateComment(context.Background(), tc.req)
			if err == nil {
				t.Fatal("err should have failed")
			}
			if len(h.seen()) != 0 {
				t.Errorf("h.seen() = %v, want empty", h.seen())
			}
		})
	}
}

// TestCreateComment_DryRun_ReturnsRecord pins the dry-run contract.
func TestCreateComment_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateComment(WithDryRun(context.Background()), favro.CreateCommentRequest{CardCommonID: "cc-1", Comment: "x"})
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
	if !strings.Contains(rec.URL, "/comments") {
		t.Errorf("rec.URL does not contain %q", "/comments")
	}
}

// TestUpdateComment_HappyPath pins PUT /comments/{id}.
func TestUpdateComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/comments/cm-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/comments/cm-1")
		}
		requireJSONEq(t, `{"comment":"edited"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"edited","lastUpdated":"2026-05-05T10:00:00Z"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateComment(context.Background(), "cm-1", favro.UpdateCommentRequest{Comment: "edited"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Body; got != "edited" {
		t.Errorf("got.Body = %v, want %v", got, "edited")
	}
	if len(got.LastUpdated) == 0 {
		t.Fatal("got.LastUpdated is empty")
	}
}

// TestUpdateComment_EmptyID_NoNetworkCall pins the empty-id guard.
func TestUpdateComment_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateComment(context.Background(), "", favro.UpdateCommentRequest{Comment: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestDeleteComment_HappyPath pins DELETE /comments/{id} → 204.
func TestDeleteComment_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/comments/cm-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/comments/cm-1")
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteComment(context.Background(), "cm-1"); err != nil {
		t.Fatalf("c.DeleteComment(context.Background(), \"cm-1\"): %v", err)
	}
}

// TestDeleteComment_EmptyID_NoNetworkCall pins the empty-id guard.
func TestDeleteComment_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if !errors.Is(c.DeleteComment(context.Background(), ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteComment(context.Background(), ""))
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// favro.Comment attachments carry the same presigned fileURL as card
// attachments, so UpdateComment strips the query too.
func TestUpdateComment_StripsPresignedAttachmentQuery(t *testing.T) {
	t.Parallel()

	const objectURL = "https://favro.s3.eu-central-1.amazonaws.com/11111111-1111-1111-1111-111111111111.gif"

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		requireJSONEq(t, `{"comment":"edited","removeAttachments":["`+objectURL+`"]}`, rec.Body)
		if strings.Contains(rec.Body, "X-Amz-Signature") {
			t.Errorf("rec.Body unexpectedly contains %q", "X-Amz-Signature")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","comment":"edited"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateComment(context.Background(), "cm-1", favro.UpdateCommentRequest{
		Comment:           "edited",
		RemoveAttachments: []string{objectURL + "?X-Amz-Signature=deadbeef&X-Amz-Expires=86400"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}
