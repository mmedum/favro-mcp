package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v3/internal/favro"
	"github.com/mmedum/favro-mcp/v3/internal/favroapi"
	"github.com/mmedum/favro-mcp/v3/internal/render"
	"github.com/mmedum/favro-mcp/v3/internal/service"
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
var errAttachmentPathNotAFile = render.Sentinel(render.ClassInvalid, "favro: file_path must point at a regular file")

// errAttachmentOutsideDir is a file_path that leaves FAVRO_UPLOAD_DIR:
// an absolute path elsewhere, a "..", or a link out of it.
var errAttachmentOutsideDir = render.Sentinel(render.ClassInvalid,
	"favro: file_path must name a file inside FAVRO_UPLOAD_DIR, as a path relative to it or an absolute path within it; nothing outside it is read")

// errAttachmentMissing is a file_path with no file behind it.
var errAttachmentMissing = render.Sentinel(render.ClassNotFound, "favro: there is no file at file_path inside FAVRO_UPLOAD_DIR")

// uploadAttachmentInput is the input for favro_upload_attachment.
// v0.1 supports local file paths only — the tool reads from disk
// and uploads raw bytes. Base64-inline body is deferred per plan
// §1's attachment-input scope decision.
type uploadAttachmentInput struct {
	dryRunInput
	CardID   string `json:"card_id" jsonschema:"the per-widget cardId to attach the file to"`
	FilePath string `json:"file_path" jsonschema:"the file to upload, as a path inside the directory FAVRO_UPLOAD_DIR names: relative to it, or absolute within it. Nothing outside that directory is read, and a link out of it is refused."`
	Filename string `json:"filename,omitempty" jsonschema:"display name on the card; defaults to the file's basename when omitted"`
	MimeType string `json:"mime_type,omitempty" jsonschema:"optional MIME type. Omit to let Favro infer it from the filename extension."`
}

// uploadCommentAttachmentInput is the input for
// favro_upload_comment_attachment — same contract as the card
// upload, addressed by commentId instead.
type uploadCommentAttachmentInput struct {
	dryRunInput
	CommentID string `json:"comment_id" jsonschema:"the Favro commentId to attach the file to"`
	FilePath  string `json:"file_path" jsonschema:"the file to upload, as a path inside the directory FAVRO_UPLOAD_DIR names: relative to it, or absolute within it. Nothing outside that directory is read, and a link out of it is refused."`
	Filename  string `json:"filename,omitempty" jsonschema:"display name on the comment; defaults to the file's basename when omitted"`
	MimeType  string `json:"mime_type,omitempty" jsonschema:"optional MIME type. Omit to let Favro infer it from the filename extension."`
}

// removeAttachmentInput is the input for favro_remove_attachment.
type removeAttachmentInput struct {
	dryRunInput
	CardID   string   `json:"card_id" jsonschema:"the per-widget cardId to detach files from"`
	FileURLs []string `json:"file_urls" jsonschema:"the attachment fileURL values to detach, exactly as favro_get_card_full or favro_upload_attachment returned them. Favro matches on the URL, not the display name; the presigned query string is stripped for you."`
}

func registerUploadAttachment(reg *registry, r *service.Resolver) {
	if reg.uploadDir == "" {
		return
	}
	addAsking(reg, &mcp.Tool{
		Name: uploadAttachmentToolName,
		Description: "Upload a local file as an attachment on a Favro card via raw-bytes POST. " +
			"Reads `file_path` from the directory FAVRO_UPLOAD_DIR names, and from nowhere else; " +
			"this tool exists only when that is set. Then POSTs to `/cards/{cardId}/attachment` with " +
			"`Content-Type: application/octet-stream` and the filename in the query string. " +
			"`filename` defaults to the file's basename if omitted. Cap is 8 MiB per upload — " +
			"larger files surface a typed error before any HTTP work. Returns the created " +
			"attachment object `{name, fileURL}` (Favro echoes the attachment, NOT the updated " +
			"Card — verified live). Successful live writes invalidate the search-cards cache " +
			"(cached card payloads carry stale attachment lists otherwise). Pass `dry_run: true` " +
			"to preview without contacting Favro. Use favro_remove_attachment to detach a file." + asksFirst,
		Annotations: mutating("Upload Favro attachment", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in uploadAttachmentInput) (*mcp.CallToolResult, writeOutput[favro.CardAttachment], error) {
		content, up, err := prepareUpload(reg.uploadDir, uploadAttachmentToolName, in.FilePath, in.Filename, in.CardID)
		if err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		filename := up.Filename
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		if err := confirmFirst(ctx, r.Client().DryRun(writeCtx), func() (render.Question, error) {
			card, err := r.Client().GetCard(ctx, in.CardID)
			up.Card = card.Name
			return render.AskUpload(up), err
		}); err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		out, err := runWrite(
			func() (favro.CardAttachment, error) {
				return r.Client().UploadAttachment(writeCtx, in.CardID, filename, in.MimeType, content)
			},
			func() string {
				return fmt.Sprintf("would upload %d-byte file %q to card %q", len(content), filename, in.CardID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		if !out.DryRun {
			r.InvalidateSearchCardCache()
		}
		return nil, out, nil
	})
}

// prepareUpload reads the file both upload tools send, names it, and
// fills what their question shares; each adds its own target.
func prepareUpload(dir, tool, filePath, filename, targetID string) ([]byte, render.Upload, error) {
	content, rel, err := readAttachmentFile(dir, filePath)
	if err != nil {
		return nil, render.Upload{}, err
	}
	if filename == "" {
		filename = path.Base(rel)
	}
	return content, render.Upload{
		Tool: tool, Path: rel, Size: int64(len(content)), Sum: render.SumBytes(content),
		Filename: filename, TargetID: targetID,
	}, nil
}

// readAttachmentFile reads the file at name inside dir, and nothing
// outside it: name is relative to dir or absolute within it, and the
// read goes through an os.Root, which refuses a ".." or a link that
// leaves the directory. It returns the content and the path relative to
// dir, slash-separated. The upload cap is checked before anything is
// read, and only a regular file is opened: stat first, since opening a
// named pipe would wait for a writer.
//
// Errors name file_path, not the path itself: the path is what a
// card's text may have suggested, and the error goes back to the model.
func readAttachmentFile(dir, name string) ([]byte, string, error) {
	rel, err := uploadPath(dir, name)
	if err != nil {
		return nil, "", err
	}
	content, err := readInRoot(dir, rel)
	if err != nil {
		return nil, "", err
	}
	return content, filepath.ToSlash(rel), nil
}

// uploadPath is name relative to dir, refused when it is not inside it
// by its spelling alone. A link out of dir is caught by the os.Root
// that opens it.
func uploadPath(dir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("favro: file_path is required")
	}
	rel := name
	if filepath.IsAbs(name) {
		r, err := filepath.Rel(dir, filepath.Clean(name))
		if err != nil {
			return "", errAttachmentOutsideDir
		}
		rel = r
	}
	if !filepath.IsLocal(rel) {
		return "", errAttachmentOutsideDir
	}
	return rel, nil
}

// statUpload refuses what readInRoot must not open: nothing there, a
// link out of the root, anything but a regular file, or a file over
// the cap.
func statUpload(root *os.Root, rel string) error {
	info, err := root.Stat(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errAttachmentMissing
	case err != nil:
		return errAttachmentOutsideDir
	case !info.Mode().IsRegular():
		return errAttachmentPathNotAFile
	case info.Size() > favroapi.UploadAttachmentMaxBytes:
		return fmt.Errorf("favro: the file is %d bytes, over the %d-byte cap", info.Size(), favroapi.UploadAttachmentMaxBytes)
	}
	return nil
}

// readInRoot reads rel inside dir through an os.Root.
func readInRoot(dir, rel string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, render.Errorf(render.ClassUnavailable, "favro: FAVRO_UPLOAD_DIR could not be opened")
	}
	defer func() { _ = root.Close() }()
	if err := statUpload(root, rel); err != nil {
		return nil, err
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, errAttachmentOutsideDir
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, errAttachmentPathNotAFile
	}
	content, err := io.ReadAll(io.LimitReader(f, favroapi.UploadAttachmentMaxBytes+1))
	switch {
	case err != nil:
		return nil, render.Errorf(render.ClassUnavailable, "favro: the file could not be read whole")
	case int64(len(content)) > favroapi.UploadAttachmentMaxBytes:
		return nil, fmt.Errorf("favro: the file grew past the %d-byte cap while it was read", favroapi.UploadAttachmentMaxBytes)
	}
	return content, nil
}

func registerUploadCommentAttachment(reg *registry, r *service.Resolver) {
	if reg.uploadDir == "" {
		return
	}
	addAsking(reg, &mcp.Tool{
		Name: uploadCommentAttachmentToolName,
		Description: "Upload a local file as an attachment on a Favro comment via raw-bytes " +
			"POST to `/comments/{commentId}/attachment`. Same contract as " +
			"favro_upload_attachment, but the file lands on a comment rather than on the " +
			"card itself: reads `file_path` from FAVRO_UPLOAD_DIR only, `filename` defaults to the " +
			"basename, 8 MiB cap enforced before any HTTP work, returns the created " +
			"attachment object `{name, fileURL}`. Pass `dry_run: true` to preview." + asksFirst,
		Annotations: mutating("Upload Favro comment attachment", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in uploadCommentAttachmentInput) (*mcp.CallToolResult, writeOutput[favro.CardAttachment], error) {
		content, up, err := prepareUpload(reg.uploadDir, uploadCommentAttachmentToolName, in.FilePath, in.Filename, in.CommentID)
		if err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		filename := up.Filename
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		if err := confirmFirst(ctx, r.Client().DryRun(writeCtx), func() (render.Question, error) {
			comment, err := r.Client().GetComment(ctx, in.CommentID)
			up.OnComment, up.Comment = true, comment.Body
			return render.AskUpload(up), err
		}); err != nil {
			return nil, writeOutput[favro.CardAttachment]{}, err
		}
		out, err := runWrite(
			func() (favro.CardAttachment, error) {
				return r.Client().UploadCommentAttachment(writeCtx, in.CommentID, filename, in.MimeType, content)
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

func registerRemoveAttachment(reg *registry, r *service.Resolver) {
	addTool(reg, &mcp.Tool{
		Name: removeAttachmentToolName,
		Description: "Detach one or more files from a Favro card. Favro has no per-attachment " +
			"DELETE — removal rides on `removeAttachments` in PUT /cards/{cardId}, matched by " +
			"attachment URL rather than display name. Pass the `fileURL` values from " +
			"favro_get_card_full or favro_upload_attachment as-is: those URLs are presigned " +
			"and re-minted on every read, so the tool strips the query string down to the " +
			"stable object URL before sending. Favro returns HTTP 200 whether or not anything " +
			"matched, so verify by re-reading the card. Successful live writes invalidate the " +
			"search-cards cache. Pass `dry_run: true` to preview.",
		Annotations: mutating("Remove Favro attachment", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in removeAttachmentInput) (*mcp.CallToolResult, writeOutput[favro.Card], error) {
		writeCtx := ctx
		if in.DryRun {
			writeCtx = favroapi.WithDryRun(ctx)
		}
		out, err := runWrite(
			func() (favro.Card, error) {
				return r.Client().RemoveAttachment(writeCtx, in.CardID, in.FileURLs...)
			},
			func() string {
				return fmt.Sprintf("would detach %d attachment(s) from card %q", len(in.FileURLs), in.CardID)
			},
		)
		if err != nil {
			return nil, writeOutput[favro.Card]{}, err
		}
		if !out.DryRun {
			r.InvalidateSearchCardCache()
		}
		return nil, out, nil
	})
}
