package tools

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// writeTempFile drops `content` into a tmp file under t.TempDir and
// returns its absolute path. Used by every upload test.
func writeTempFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(path, content, 0o600): %v", err)
	}
	return path
}

func TestMCP_UploadAttachment_HappyPath(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST; got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/attachment") {
			t.Errorf("expected /attachment suffix; got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("filename"); got != "note.txt" {
			t.Errorf("expected filename=note.txt; got %q", got)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/octet-stream" {
			t.Errorf("expected Content-Type=application/octet-stream; got %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if string(body) != "raw bytes" {
			t.Errorf("expected raw body; got %q", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"note.txt","fileURL":"https://favro.invalid/a/note.txt"}`))
	}))

	path := writeTempFile(t, "note.txt", []byte("raw bytes"))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: uploadAttachmentToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"file_path": path,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Errorf("tool error: %s", serializedResponseString(t, res))
	}

	out := decodeStructured[writeOutput[favro.CardAttachment]](t, res)
	if got := out.Result.Name; got != "note.txt" {
		t.Errorf("Favro returns the attachment object, not the Card — verified live Phase 7.1: got %v, want %v", got, "note.txt")
	}
	if got := out.Result.FileURL; got != "https://favro.invalid/a/note.txt" {
		t.Errorf("out.Result.FileURL = %v, want %v", got, "https://favro.invalid/a/note.txt")
	}
}

// TestMCP_UploadAttachment_FilenameOverride pins that an explicit
// filename takes precedence over the file's basename — useful when
// the LLM wants the display name to differ from the on-disk name.
func TestMCP_UploadAttachment_FilenameOverride(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("filename"); got != "renamed.txt" {
			t.Errorf("expected filename override; got %q", got)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"renamed.txt","fileURL":"https://favro.invalid/a/x.txt"}`))
	}))

	path := writeTempFile(t, "ondisk.txt", []byte("data"))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: uploadAttachmentToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"file_path": path,
			"filename":  "renamed.txt",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}
}

func TestMCP_UploadAttachment_DryRun(t *testing.T) {
	t.Parallel()

	var posts atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
	}))

	path := writeTempFile(t, "preview.txt", []byte("preview"))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: uploadAttachmentToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"file_path": path,
			"dry_run":   true,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := decodeStructured[writeOutput[favro.CardAttachment]](t, res)
	if !out.DryRun {
		t.Error("out.DryRun = false, want true")
	}
	if !strings.Contains(out.PredictedStateDiff, "preview.txt") {
		t.Errorf("out.PredictedStateDiff does not contain %q", "preview.txt")
	}
	if got := posts.Load(); got != 0 {
		t.Errorf("posts.Load() = %v, want %v", got, 0)
	}
}

// TestMCP_UploadAttachment_PathNotAFile pins the path-must-be-regular
// guard so directories / FIFOs / device nodes don't get accidentally
// streamed up.
func TestMCP_UploadAttachment_PathNotAFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	cs := connectInMemoryWith(t, favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: uploadAttachmentToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"file_path": dir,
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	if !strings.Contains(strings.ToLower(serializedResponseString(t, res)), "regular file") {
		t.Errorf("strings.ToLower(serializedResponseString(t, res)) does not contain %q", "regular file")
	}
}

func TestMCP_UploadAttachment_PathDoesNotExist(t *testing.T) {
	t.Parallel()

	cs := connectInMemoryWith(t, favroFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: uploadAttachmentToolName,
		Arguments: map[string]any{
			"card_id":   "ci-1",
			"file_path": "/this/path/does/not/exist.txt",
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
}

func TestMCP_UploadAttachment_MissingRequiredFields(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"card_id", "file_path"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			assertMissingRequiredFieldFails(t, uploadAttachmentToolName, field)
		})
	}
}

// favro_remove_attachment is intentionally NOT registered (Favro
// silently no-ops `removeAttachments` on PUT /cards — verified live
// Phase 7.1). MCP-layer tests for it are gated until the right wire
// shape is found; see favro.RemoveAttachment for the favro-layer
// stub kept for future investigation.
