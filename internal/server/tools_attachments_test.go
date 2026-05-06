package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// writeTempFile drops `content` into a tmp file under t.TempDir and
// returns its absolute path. Used by every upload test.
func writeTempFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, content, 0o600))
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
	require.NoError(t, err)
	require.False(t, res.IsError, "tool error: %s", serializedResponseString(t, res))

	out := decodeStructured[writeOutput[favro.CardAttachment]](t, res)
	require.Equal(t, "note.txt", out.Result.Name,
		"Favro returns the attachment object, not the Card — verified live Phase 7.1")
	require.Equal(t, "https://favro.invalid/a/note.txt", out.Result.FileURL)
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
	require.NoError(t, err)
	require.False(t, res.IsError)
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
	require.NoError(t, err)

	out := decodeStructured[writeOutput[favro.CardAttachment]](t, res)
	require.True(t, out.DryRun)
	require.Contains(t, out.PredictedStateDiff, "preview.txt")
	require.EqualValues(t, 0, posts.Load())
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
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, strings.ToLower(serializedResponseString(t, res)), "regular file")
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
	require.NoError(t, err)
	require.True(t, res.IsError)
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
