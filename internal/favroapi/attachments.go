package favroapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
	"github.com/mmedum/favro-mcp/v4/internal/render"
)

// errMissingFilename is returned by UploadAttachment when the
// caller passes an empty filename. Favro requires the filename in
// the query string to determine the attachment's display name; an
// empty filename would 400 server-side or, worse, silently land an
// untitled file.
var errMissingFilename = render.Sentinel(render.ClassInvalid, "favro: filename is required for attachment upload")

// UploadAttachmentMaxBytes caps the in-memory size of a single
// attachment upload. Favro's reference gives the REST limit as
// "10mb"; the cap here is 8 MiB, under either reading of that, to
// leave headroom for HTTP framing and other concurrent in-flight
// bodies. The MCP tool surfaces a typed error rather than streaming a
// too-large file blindly.
// https://favro.com/developer/#upload-a-file-to-card
const UploadAttachmentMaxBytes = 8 * 1024 * 1024

// UploadAttachment posts raw bytes to POST /cards/{cardId}/attachment
// with the filename in the query string. Returns the created
// favro.CardAttachment ({name, fileURL}): Favro answers an upload with
// the attachment object, not the card, as its reference says and as a
// live organization answers, checked by hand. No live step uploads a
// file. Decoding into favro.Card would fill only `Name` and leave the
// rest zero.
// https://favro.com/developer/#upload-a-file-to-card
//
// mimeType is optional; pass "" to let Favro infer the kind from the
// filename extension. Empty cardID short-circuits with errMissingID;
// empty filename with errMissingFilename. Honors WithDryRun /
// ForceDryRun via the wrapped Do.
func (c *Client) UploadAttachment(ctx context.Context, cardID, filename, mimeType string, content []byte) (favro.CardAttachment, error) {
	if cardID == "" {
		return favro.CardAttachment{}, errMissingID
	}
	return c.uploadAttachment(ctx, "/cards/"+url.PathEscape(cardID)+"/attachment", filename, mimeType, content)
}

// UploadCommentAttachment posts raw bytes to
// POST /comments/{commentId}/attachment. Same contract as
// UploadAttachment, but the file lands on a comment rather than on
// the card itself.
func (c *Client) UploadCommentAttachment(ctx context.Context, commentID, filename, mimeType string, content []byte) (favro.CardAttachment, error) {
	if commentID == "" {
		return favro.CardAttachment{}, errMissingID
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
func (c *Client) uploadAttachment(ctx context.Context, path, filename, mimeType string, content []byte) (favro.CardAttachment, error) {
	if filename == "" {
		return favro.CardAttachment{}, errMissingFilename
	}
	if int64(len(content)) > UploadAttachmentMaxBytes {
		return favro.CardAttachment{}, fmt.Errorf("favro: attachment exceeds %d-byte cap (%d bytes)", UploadAttachmentMaxBytes, len(content))
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
		return favro.CardAttachment{}, err
	}
	defer drainAndClose(resp)
	var out favro.CardAttachment
	if err := decodeJSONLenient(resp, &out); err != nil {
		return favro.CardAttachment{}, err
	}
	return out, nil
}

// RemoveAttachment detaches files from a card by their fileURL.
//
// Favro documents removeAttachments as "the list of attachments
// URLs". Pass favro.CardAttachment.FileURL as read — the query string is
// stripped for you, see favro.CanonicalAttachmentURL for why the
// presigned URL whole cannot match. Favro answers the display name and
// the bare S3 object name with 200 and removes nothing.
//
// The stripped URL removes the attachment, checked by hand against a
// live organization. No live step removes one: the step "remove an
// attachment from a card by URL, dry run" only builds the body. Favro
// returns 200 whether or not anything matched, so confirm by
// re-reading the card.
// https://favro.com/developer/#update-a-card
func (c *Client) RemoveAttachment(ctx context.Context, cardID string, fileURLs ...string) (favro.Card, error) {
	if cardID == "" {
		return favro.Card{}, errMissingID
	}
	if len(fileURLs) == 0 {
		return favro.Card{}, fmt.Errorf("favro: at least one attachment fileURL is required")
	}
	canonical := make([]string, len(fileURLs))
	for i, u := range fileURLs {
		if u == "" {
			return favro.Card{}, fmt.Errorf("favro: attachment fileURL must not be empty")
		}
		canonical[i] = favro.CanonicalAttachmentURL(u)
	}
	return c.UpdateCard(ctx, cardID, favro.UpdateCardRequest{
		RemoveAttachments: canonical,
	})
}
