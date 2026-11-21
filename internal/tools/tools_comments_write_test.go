package tools

import (
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
)

func TestMCP_CreateComment_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("create_comment must POST; got %s", r.Method)
		}
		if r.URL.Path != "/comments" {
			t.Errorf("expected /comments; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"hello"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      createCommentToolName,
		Arguments: map[string]any{"card_common_id": "cc-1", "comment": "hello"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Comment]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	if got := out.Result.CommentID; got != "cm-1" {
		t.Errorf("out.Result.CommentID = %v, want %v", got, "cm-1")
	}
}

func TestMCP_CreateComment_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: createCommentToolName,
		Arguments: map[string]any{
			"card_common_id": "cc-1",
			"comment":        "hello",
			"dry_run":        true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Comment]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if got := out.WouldCall.Method; got != http.MethodPost {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodPost)
	}
	if !strings.Contains(out.WouldCall.URL, "/comments") {
		t.Errorf("out.WouldCall.URL does not contain %q", "/comments")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
}

func TestMCP_CreateComment_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		field string
	}{
		{"missing card_common_id", "card_common_id"},
		{"missing comment", "comment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, createCommentToolName, tc.field)
		})
	}
}

func TestMCP_UpdateComment_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: updateCommentToolName,
		Arguments: map[string]any{
			"comment_id": "cm-1",
			"comment":    "edited",
			"dry_run":    true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Comment]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if got := out.WouldCall.Method; got != http.MethodPut {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodPut)
	}
	if !strings.Contains(out.WouldCall.URL, "/comments/cm-1") {
		t.Errorf("out.WouldCall.URL does not contain %q", "/comments/cm-1")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
}

func TestMCP_UpdateComment_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("update_comment must PUT; got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/comments/") {
			t.Errorf("expected /comments/{id}; got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commentId":"cm-1","cardCommonId":"cc-1","userId":"u-1","comment":"edited"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      updateCommentToolName,
		Arguments: map[string]any{"comment_id": "cm-1", "comment": "edited"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[favro.Comment]](t, res)
	if got := out.Result.Body; got != "edited" {
		t.Errorf("out.Result.Body = %v, want %v", got, "edited")
	}
}

func TestMCP_DeleteComment_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("delete_comment must DELETE; got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      deleteCommentToolName,
		Arguments: map[string]any{"comment_id": "cm-1", "confirm": true},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[struct{}]](t, res)
	if out.DryRun {
		t.Error("out.DryRun = true, want false")
	}
	if out.Result == nil {
		t.Fatal("out.Result is nil")
	}
}

func TestMCP_DeleteComment_DryRun(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: deleteCommentToolName,
		Arguments: map[string]any{
			"comment_id": "cm-1",
			"dry_run":    true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	out := decodeStructured[writeOutput[struct{}]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if got := out.WouldCall.Method; got != http.MethodDelete {
		t.Errorf("out.WouldCall.Method = %v, want %v", got, http.MethodDelete)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls.Load() = %v, want %v", got, 0)
	}
}

func TestMCP_DeleteComment_MissingCommentID(t *testing.T) {
	t.Parallel()
	assertMissingRequiredFieldFails(t, deleteCommentToolName, "comment_id")
}

// A reply Favro files in another thread is still a comment it wrote, so
// it is not an error; the note says where it went (D-24).
func TestMCP_CreateComment_ReplyFiledElsewhereIsNoted(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, answered string
		note           string // a substring the note holds, or "" for no note
	}{
		{"filed in the thread asked for", "th-1", ""},
		{"filed in another thread", "th-new", `not the requested "th-1"`},
		{"an answer naming no thread", "", `is unconfirmed`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"commentId":"cm-2","cardCommonId":"cc-1","threadId":"` + tc.answered + `","comment":"reply"}`))
			}))
			res, err := connectInMemoryWith(t, c).CallTool(t.Context(), &mcp.CallToolParams{
				Name:      createCommentToolName,
				Arguments: map[string]any{"card_common_id": "cc-1", "comment": "reply", "thread_id": "th-1"},
			})
			if err != nil || res.IsError {
				t.Fatalf("err = %v, IsError = %v", err, res != nil && res.IsError)
			}
			out := decodeStructured[writeOutput[favro.Comment]](t, res)
			noted := tc.note != "" && slices.ContainsFunc(out.Notes, func(n string) bool { return strings.Contains(n, tc.note) })
			if noted != (tc.note != "") || (tc.note == "" && len(out.Notes) > 0) {
				t.Errorf("notes = %q, want one holding %q", out.Notes, tc.note)
			}
		})
	}
}
