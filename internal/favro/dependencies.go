package favro

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

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

// dependenciesPath builds /cards/{cardId}/dependencies.
func dependenciesPath(cardID string) string {
	return "/cards/" + url.PathEscape(cardID) + "/dependencies"
}

// ListDependencies returns every dependency of a card. Unlike most
// Favro list endpoints this one is not paginated — it returns a
// single object with the full list.
func (c *Client) ListDependencies(ctx context.Context, cardID string) (CardDependencies, error) {
	if cardID == "" {
		return CardDependencies{}, errMissingID
	}
	var out CardDependencies
	if err := c.GetJSON(ctx, dependenciesPath(cardID), nil, &out); err != nil {
		return CardDependencies{}, err
	}
	return out, nil
}

// dependenciesBody is the request wrapper both the create and the
// replace endpoints take.
type dependenciesBody struct {
	Dependencies []CardDependencyOption `json:"dependencies"`
}

// CreateDependencies ADDS dependencies to a card, leaving existing
// ones in place. Returns the card's full dependency list afterwards.
func (c *Client) CreateDependencies(ctx context.Context, cardID string, deps []CardDependencyOption) (CardDependencies, error) {
	return c.writeDependencies(ctx, http.MethodPost, cardID, deps)
}

// ReplaceDependencies REPLACES a card's dependency list: every
// existing dependency is removed and the supplied set becomes the
// whole list. Use CreateDependencies to add without clearing.
func (c *Client) ReplaceDependencies(ctx context.Context, cardID string, deps []CardDependencyOption) (CardDependencies, error) {
	return c.writeDependencies(ctx, http.MethodPut, cardID, deps)
}

// writeDependencies is the shared body of the add (POST) and replace
// (PUT) paths — they differ only in method.
func (c *Client) writeDependencies(ctx context.Context, method, cardID string, deps []CardDependencyOption) (CardDependencies, error) {
	if cardID == "" {
		return CardDependencies{}, errMissingID
	}
	if len(deps) == 0 {
		return CardDependencies{}, fmt.Errorf("favro: at least one dependency is required")
	}
	for i, d := range deps {
		if d.CardID == "" {
			return CardDependencies{}, fmt.Errorf("favro: dependency %d is missing cardId", i)
		}
	}
	var out CardDependencies
	if err := c.doJSON(ctx, method, dependenciesPath(cardID), nil, dependenciesBody{Dependencies: deps}, &out); err != nil {
		return CardDependencies{}, err
	}
	return out, nil
}

// UpdateDependencyRequest is the body for
// PATCH /cards/{cardId}/dependencies/{dependencyCardId}. IsBefore is
// *bool so &false (flip the link to "after") is distinguishable from
// "don't touch".
type UpdateDependencyRequest struct {
	IsBefore *bool `json:"isBefore,omitempty"`
}

// UpdateDependency changes one dependency's direction. Returns the
// card's full dependency list afterwards.
func (c *Client) UpdateDependency(ctx context.Context, cardID, dependencyCardID string, req UpdateDependencyRequest) (CardDependencies, error) {
	if cardID == "" || dependencyCardID == "" {
		return CardDependencies{}, errMissingID
	}
	var out CardDependencies
	path := dependenciesPath(cardID) + "/" + url.PathEscape(dependencyCardID)
	if err := c.PatchJSON(ctx, path, req, &out); err != nil {
		return CardDependencies{}, err
	}
	return out, nil
}

// DeleteDependency removes one dependency link from a card. Honors
// WithDryRun / ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteDependency(ctx context.Context, cardID, dependencyCardID string) error {
	if cardID == "" || dependencyCardID == "" {
		return errMissingID
	}
	return c.DeleteJSON(ctx, dependenciesPath(cardID)+"/"+url.PathEscape(dependencyCardID), nil)
}

// DeleteAllDependencies removes every dependency from a card.
// Honors WithDryRun / ForceDryRun via the wrapped DeleteJSON.
func (c *Client) DeleteAllDependencies(ctx context.Context, cardID string) error {
	if cardID == "" {
		return errMissingID
	}
	return c.DeleteJSON(ctx, dependenciesPath(cardID), nil)
}
