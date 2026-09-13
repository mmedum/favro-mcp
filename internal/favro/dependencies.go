package favro

// CardDependencies is the response shape of every
// /cards/{cardId}/dependencies endpoint: the card being described,
// plus its full dependency list. Favro returns the whole list after
// every mutation, so callers never need a follow-up read.
type CardDependencies struct {
	CardID         string           `json:"cardId,omitempty"`
	CardCommonID   string           `json:"cardCommonId,omitempty"`
	OrganizationID string           `json:"organizationId,omitempty"`
	Dependencies   []CardDependency `json:"dependencies,omitempty"`
}

// UpdateDependencyRequest is the body for
// PATCH /cards/{cardId}/dependencies/{dependencyCardId}. IsBefore is
// *bool so &false (flip the link to "after") is distinguishable from
// "don't touch".
type UpdateDependencyRequest struct {
	IsBefore *bool `json:"isBefore,omitempty"`
}
