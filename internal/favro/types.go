// Package favro is the Favro REST API client used by the MCP server.
// API-faithful: one Go method per Favro endpoint.
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
