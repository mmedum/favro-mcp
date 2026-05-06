package favro

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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
// Content-Type defaults to application/octet-stream so Favro infers
// the kind from the filename extension rather than tripping its
// JSON parser. Uses c.Do directly because the PostJSON wrapper
// doesn't accept a query parameter — the filename has to ride on
// the URL. encodeBody's []byte shortcut means the raw bytes pass
// through unmodified. Empty cardID short-circuits with errMissingID;
// empty filename with errMissingFilename. Honors WithDryRun /
// ForceDryRun via the wrapped Do.
func (c *Client) UploadAttachment(ctx context.Context, cardID, filename string, content []byte) (CardAttachment, error) {
	if cardID == "" {
		return CardAttachment{}, errMissingID
	}
	if filename == "" {
		return CardAttachment{}, errMissingFilename
	}
	if int64(len(content)) > UploadAttachmentMaxBytes {
		return CardAttachment{}, fmt.Errorf("favro: attachment exceeds %d-byte cap (%d bytes)", UploadAttachmentMaxBytes, len(content))
	}
	q := url.Values{}
	q.Set("filename", filename)
	resp, err := c.Do(
		ctx,
		http.MethodPost,
		"/cards/"+url.PathEscape(cardID)+"/attachment",
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

// RemoveAttachment is reserved for future use and is currently
// unexposed at the MCP layer.
//
// **Known not working** (verified live Phase 7.1): Favro accepts a
// PUT /cards/{cardId} body with `removeAttachments: ["filename"]`
// and returns 200, but the attachment is NOT actually removed —
// retried both with the display name and with the underlying S3
// object name. Favro appears to silently ignore unknown fields,
// suggesting the docs-hinted shape isn't what the API actually
// requires. The `RemoveAttachments` field on UpdateCardRequest is
// kept in case a future investigation finds the right wire shape;
// the favro_remove_attachment MCP tool is intentionally NOT
// registered until then.
func (c *Client) RemoveAttachment(ctx context.Context, cardID, filename string) (Card, error) {
	if cardID == "" {
		return Card{}, errMissingID
	}
	if filename == "" {
		return Card{}, errMissingFilename
	}
	return c.UpdateCard(ctx, cardID, UpdateCardRequest{
		RemoveAttachments: []string{filename},
	})
}
