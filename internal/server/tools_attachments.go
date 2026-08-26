package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favro"
)

const (
	uploadAttachmentToolName        = "favro_upload_attachment"
	uploadCommentAttachmentToolName = "favro_upload_comment_attachment"
	removeAttachmentToolName        = "favro_remove_attachment"
)

// errAttachmentPathNotAFile is returned when file_path resolves to a
// directory, symlink loop, or other non-regular file. Both upload
// paths expect raw bytes from a single file; surfacing the error
// locally avoids reading device nodes or large directory listings
// over the wire.
var errAttachmentPathNotAFile = errors.New("favro: file_path must point at a regular file")

// uploadAttachmentInput is the input for favro_upload_attachment.
// v0.1 supports local file paths only — the tool reads from disk
// and uploads raw bytes. Base64-inline body is deferred per plan
// §1's attachment-input scope decision.
type uploadAttachmentInput struct {
	dryRunInput
	CardID   string `json:"card_id" jsonschema:"the per-widget cardId to attach the file to"`
	FilePath string `json:"file_path" jsonschema:"absolute path to the local file to upload. Local file paths only."`
	Filename string `json:"filename,omitempty" jsonschema:"display name on the card; defaults to the file's basename when omitted"`
	MimeType string `json:"mime_type,omitempty" jsonschema:"optional MIME type. Omit to let Favro infer it from the filename extension."`
}

// uploadCommentAttachmentInput is the input for
// favro_upload_comment_attachment — same contract as the card
// upload, addressed by commentId instead.
type uploadCommentAttachmentInput struct {
	dryRunInput
	CommentID string `json:"comment_id" jsonschema:"the Favro commentId to attach the file to"`
	FilePath  string `json:"file_path" jsonschema:"absolute path to the local file to upload. Local file paths only."`
	Filename  string `json:"filename,omitempty" jsonschema:"display name on the comment; defaults to the file's basename when omitted"`
	MimeType  string `json:"mime_type,omitempty" jsonschema:"optional MIME type. Omit to let Favro infer it from the filename extension."`
}

// removeAttachmentInput is the input for favro_remove_attachment.
type removeAttachmentInput struct {
	dryRunInput
	CardID   string   `json:"card_id" jsonschema:"the per-widget cardId to detach files from"`
	FileURLs []string `json:"file_urls" jsonschema:"the attachment fileURL values to detach. Read them from favro_get_card_full or from a previous favro_upload_attachment response — Favro matches on the full URL, not the display name."`
}

func registerUploadAttachment(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: uploadAttachmentToolName,
		Description: "Upload a local file as an attachment on a Favro card via raw-bytes POST. " +
			"Reads from `file_path` (absolute), then POSTs to `/cards/{cardId}/attachment` with " +
			"`Content-Type: application/octet-stream` and the filename in the query string. " +
			"`filename` defaults to the file's basename if omitted. Cap is 8 MiB per upload — " +
			"larger files surface a typed error before any HTTP work. Returns the created " +
			"attachment object `{name, fileURL}` (Favro echoes the attachment, NOT the updated " +
			"Card — verified live). Successful live writes invalidate the search-cards cache " +
			"(cached card payloads carry stale attachment lists otherwise). Pass `dry_run: true` " +
			"to preview without contacting Favro. Use favro_remove_attachment to detach a file.",
		Annotations: mutating("Upload Favro attachment", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in uploadAttachmentInput) (*mcp.CallToolResult, writeOutput[favro.CardAttachment], error) {
		content, err := readAttachmentFile(in.FilePath)
		if err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		filename := in.Filename
		if filename == "" {
			filename = filepath.Base(in.FilePath)
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.CardAttachment, error) {
				return r.client.UploadAttachment(writeCtx, in.CardID, filename, in.MimeType, content)
			},
			func() string {
				return fmt.Sprintf("would upload %d-byte file %q to card %q", len(content), filename, in.CardID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		if !out.DryRun {
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

// readAttachmentFile reads the file at path with the upload cap
// enforced before allocating. Stats first so a multi-GiB file
// doesn't OOM the process during io.ReadAll. The cap also blocks
// directories / FIFOs / device nodes from being mistakenly read.
func readAttachmentFile(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("favro: file_path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("favro: stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q", errAttachmentPathNotAFile, path)
	}
	if info.Size() > favro.UploadAttachmentMaxBytes {
		return nil, fmt.Errorf("favro: file %q is %d bytes, exceeds the %d-byte cap", path, info.Size(), favro.UploadAttachmentMaxBytes)
	}
	content, err := os.ReadFile(path) //nolint:gosec // G304: file_path is the documented input — these tools read what the LLM tells them to.
	if err != nil {
		return nil, fmt.Errorf("favro: read %q: %w", path, err)
	}
	return content, nil
}

func registerUploadCommentAttachment(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: uploadCommentAttachmentToolName,
		Description: "Upload a local file as an attachment on a Favro comment via raw-bytes " +
			"POST to `/comments/{commentId}/attachment`. Same contract as " +
			"favro_upload_attachment, but the file lands on a comment rather than on the " +
			"card itself: reads from `file_path` (absolute), `filename` defaults to the " +
			"basename, 8 MiB cap enforced before any HTTP work, returns the created " +
			"attachment object `{name, fileURL}`. Pass `dry_run: true` to preview.",
		Annotations: mutating("Upload Favro comment attachment", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in uploadCommentAttachmentInput) (*mcp.CallToolResult, writeOutput[favro.CardAttachment], error) {
		content, err := readAttachmentFile(in.FilePath)
		if err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		filename := in.Filename
		if filename == "" {
			filename = filepath.Base(in.FilePath)
		}
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.CardAttachment, error) {
				return r.client.UploadCommentAttachment(writeCtx, in.CommentID, filename, in.MimeType, content)
			},
			func() string {
				return fmt.Sprintf("would upload %d-byte file %q to comment %q", len(content), filename, in.CommentID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		return nil, out, nil
	})
}

func registerRemoveAttachment(srv *mcp.Server, r *Resolver) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: removeAttachmentToolName,
		Description: "Detach one or more files from a Favro card. Favro has no per-attachment " +
			"DELETE — removal rides on `removeAttachments` in PUT /cards/{cardId}, and the " +
			"list is matched by attachment **URL**, not display name. Pass the `fileURL` " +
			"values from favro_get_card_full or from a favro_upload_attachment response. " +
			"Favro returns HTTP 200 whether or not anything matched, so verify by re-reading " +
			"the card: if the attachment survives, the URL didn't match. Successful live " +
			"writes invalidate the search-cards cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Remove Favro attachment", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in removeAttachmentInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favro.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) {
				return r.client.RemoveAttachment(writeCtx, in.CardID, in.FileURLs...)
			},
			func() string {
				return fmt.Sprintf("would detach %d attachment(s) from card %q", len(in.FileURLs), in.CardID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.invalidateSearchCardCache()
		}
		return nil, out, nil
	})
}
