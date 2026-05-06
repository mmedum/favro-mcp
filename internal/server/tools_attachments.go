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
	uploadAttachmentToolName = "favro_upload_attachment"
)

// errAttachmentPathNotAFile is returned when file_path resolves to a
// directory, symlink loop, or other non-regular file. The upload
// path expects raw bytes from a single file; surfacing the error
// locally avoids reading device nodes or large directory listings
// over the wire.
var errAttachmentPathNotAFile = errors.New("favro_upload_attachment: file_path must point at a regular file")

// uploadAttachmentInput is the input for favro_upload_attachment.
// v0.1 supports local file paths only — the tool reads from disk
// and uploads raw bytes. Base64-inline body is deferred per plan
// §1's attachment-input scope decision.
type uploadAttachmentInput struct {
	dryRunInput
	CardID   string `json:"card_id" jsonschema:"the per-widget cardId to attach the file to"`
	FilePath string `json:"file_path" jsonschema:"absolute path to the local file to upload. v0.1 supports local file paths only."`
	Filename string `json:"filename,omitempty" jsonschema:"display name on the card; defaults to the file's basename when omitted"`
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
			"to preview without contacting Favro. NOTE: there is no companion remove tool yet — " +
			"`removeAttachments` on PUT /cards is silently no-op'd by Favro; investigate the " +
			"correct wire shape before adding favro_remove_attachment.",
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
				return r.client.UploadAttachment(writeCtx, in.CardID, filename, content)
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
		return nil, fmt.Errorf("favro_upload_attachment: file_path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("favro_upload_attachment: stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %q", errAttachmentPathNotAFile, path)
	}
	if info.Size() > favro.UploadAttachmentMaxBytes {
		return nil, fmt.Errorf("favro_upload_attachment: file %q is %d bytes, exceeds the %d-byte cap", path, info.Size(), favro.UploadAttachmentMaxBytes)
	}
	content, err := os.ReadFile(path) //nolint:gosec // G304: file_path is the documented input — this tool reads what the LLM tells it to.
	if err != nil {
		return nil, fmt.Errorf("favro_upload_attachment: read %q: %w", path, err)
	}
	return content, nil
}
