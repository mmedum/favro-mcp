package favro

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// errMissingFilename is returned by UploadAttachment when the
// caller passes an empty filename. Favro requires the filename in
// the query string to determine the attachment's display name; an
// empty filename would 400 server-side or, worse, silently land an
// untitled file.
var errMissingFilename = fmt.Errorf("favro: filename is required for attachment upload")

// UploadAttachmentMaxBytes caps the in-memory size of a single
// attachment upload. Favro's documented limit is 10 MiB; the cap
// here is 8 MiB to leave headroom for HTTP framing and other
// concurrent in-flight bodies. The MCP tool surfaces a typed error
// rather than streaming a too-large file blindly.
const UploadAttachmentMaxBytes = 8 * 1024 * 1024

// UploadAttachment posts raw bytes to POST /cards/{cardId}/attachment
// with the filename in the query string. Returns the created
// CardAttachment ({name, fileURL}) — verified live Phase 7.1 that
// Favro echoes the *attachment object*, NOT the updated Card the
// docs hint at. Decoding into Card silently fills only `Name` and
// leaves the rest zero, which masks the contract; CardAttachment
// matches what Favro actually returns.
//
// mimeType is optional; pass "" to let Favro infer the kind from the
// filename extension. Empty cardID short-circuits with errMissingID;
// empty filename with errMissingFilename. Honors WithDryRun /
// ForceDryRun via the wrapped Do.
func (c *Client) UploadAttachment(ctx context.Context, cardID, filename, mimeType string, content []byte) (CardAttachment, error) {
	if cardID == "" {
		return CardAttachment{}, errMissingID
	}
	return c.uploadAttachment(ctx, "/cards/"+url.PathEscape(cardID)+"/attachment", filename, mimeType, content)
}

// UploadCommentAttachment posts raw bytes to
// POST /comments/{commentId}/attachment. Same contract as
// UploadAttachment, but the file lands on a comment rather than on
// the card itself.
func (c *Client) UploadCommentAttachment(ctx context.Context, commentID, filename, mimeType string, content []byte) (CardAttachment, error) {
	if commentID == "" {
		return CardAttachment{}, errMissingID
	}
	return c.uploadAttachment(ctx, "/comments/"+url.PathEscape(commentID)+"/attachment", filename, mimeType, content)
}

// uploadAttachment is the shared body of the card and comment upload
// paths — they differ only in the endpoint.
//
// Content-Type defaults to application/octet-stream so Favro infers
// the kind from the filename extension rather than tripping its JSON
// parser. Uses c.Do directly because the PostJSON wrapper doesn't
// accept query parameters — the filename has to ride on the URL.
// encodeBody's []byte shortcut means the raw bytes pass through
// unmodified.
func (c *Client) uploadAttachment(ctx context.Context, path, filename, mimeType string, content []byte) (CardAttachment, error) {
	if filename == "" {
		return CardAttachment{}, errMissingFilename
	}
	if int64(len(content)) > UploadAttachmentMaxBytes {
		return CardAttachment{}, fmt.Errorf("favro: attachment exceeds %d-byte cap (%d bytes)", UploadAttachmentMaxBytes, len(content))
	}
	q := url.Values{}
	q.Set("filename", filename)
	if mimeType != "" {
		q.Set("mimeType", mimeType)
	}
	resp, err := c.Do(
		ctx,
		http.MethodPost,
		path,
		q,
		content,
		WithHeader("Content-Type", "application/octet-stream"),
	)
	if err != nil {
		return CardAttachment{}, err
	}
	defer drainAndClose(resp)
	var out CardAttachment
	if err := decodeJSONLenient(resp, &out); err != nil {
		return CardAttachment{}, err
	}
	return out, nil
}

// CanonicalAttachmentURL strips the query string from an attachment
// URL, leaving the stable S3 object URL.
//
// This matters because Favro hands back a *presigned* fileURL, minted
// per request. Two reads of the same attachment, 56 minutes apart,
// returned the same object key with different X-Amz-Date and
// X-Amz-Signature values (verified live 2026-08-26). So the fileURL a
// caller reads back is never byte-equal to anything Favro could have
// stored, and matching on it cannot work. Everything up to the "?" is
// stable; everything after it is a signature with a 24-hour expiry.
//
// Passing an already-stripped URL is a no-op.
func CanonicalAttachmentURL(fileURL string) string {
	if i := strings.IndexByte(fileURL, '?'); i >= 0 {
		return fileURL[:i]
	}
	return fileURL
}

// RemoveAttachment detaches files from a card by their fileURL.
//
// Favro documents removeAttachments as "the list of attachments
// URLs". Pass CardAttachment.FileURL as read — the query string is
// stripped for you, see CanonicalAttachmentURL for why that is
// required rather than cosmetic.
//
// Two earlier attempts at this failed: Phase 7.1 passed the display
// name and the bare S3 object name, and got HTTP 200 with no removal
// both times; v1.1.0 passed the presigned URL whole, which cannot
// match for the reason above.
//
// Verified live 2026-08-26: the stripped form does remove the
// attachment. Favro still returns 200 whether or not anything matched,
// so confirm by re-reading the card.
func (c *Client) RemoveAttachment(ctx context.Context, cardID string, fileURLs ...string) (Card, error) {
	if cardID == "" {
		return Card{}, errMissingID
	}
	if len(fileURLs) == 0 {
		return Card{}, fmt.Errorf("favro: at least one attachment fileURL is required")
	}
	canonical := make([]string, len(fileURLs))
	for i, u := range fileURLs {
		if u == "" {
			return Card{}, fmt.Errorf("favro: attachment fileURL must not be empty")
		}
		canonical[i] = CanonicalAttachmentURL(u)
	}
	return c.UpdateCard(ctx, cardID, UpdateCardRequest{
		RemoveAttachments: canonical,
	})
}
