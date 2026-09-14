// Package favro is Favro's wire vocabulary: the request and response
// types the REST API speaks, and nothing else.
//
// It imports nothing from this module, which is the point. The client
// that calls Favro is internal/favroapi, the orchestration above it is
// internal/service, and the MCP surface above that is internal/tools —
// so a type can be shared by all three without any of them depending
// on each other.
//
// Reads are tolerant and writes are not. A response type accepts the
// documented shape and any shape previously observed from the live API,
// preferring the documented one — Card.CustomFields, Tasklist.Title and
// Activity.CommonID exist for exactly that. A request type sends the
// documented shape only.
package favro

// PageEnvelope is the response wrapper Favro returns around every
// paginated endpoint. Pagination state lives on the envelope; entities
// live in the typed Entities slice.
//
// Generic over the entity type so callers do
//
//	var resp favro.PageEnvelope[favro.Card]
//	json.Unmarshal(body, &resp)
//
// without any wrapper-type boilerplate per resource.
type PageEnvelope[T any] struct {
	Limit     int    `json:"limit"`
	Page      int    `json:"page"`
	Pages     int    `json:"pages"`
	RequestID string `json:"requestId"`
	Entities  []T    `json:"entities"`
}

// HasNextPage reports whether at least one more page exists after the
// one this envelope describes. Pages is 1-indexed in Favro responses.
func (p PageEnvelope[T]) HasNextPage() bool {
	return p.Page+1 < p.Pages
}
