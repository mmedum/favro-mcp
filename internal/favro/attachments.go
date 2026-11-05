package favro

import "strings"

// CanonicalAttachmentURL strips the query string from an attachment
// URL, leaving the stable S3 object URL.
//
// This matters because Favro hands back a *presigned* fileURL, minted
// per request: two reads of the same attachment return the same object
// key with different X-Amz-Date and X-Amz-Signature values. So the fileURL a
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
