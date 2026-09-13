package favroapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mmedum/favro-mcp/internal/favro"
)

// dependenciesPath builds /cards/{cardId}/dependencies.
func dependenciesPath(cardID string) string {
	return "/cards/" + url.PathEscape(cardID) + "/dependencies"
}

// ListDependencies returns every dependency of a card. Unlike most
// Favro list endpoints this one is not paginated — it returns a
// single object with the full list.
func (c *Client) ListDependencies(ctx context.Context, cardID string) (favro.CardDependencies, error) {
	if cardID == "" {
		return favro.CardDependencies{}, errMissingID
	}
	var out favro.CardDependencies
	if err := c.GetJSON(ctx, dependenciesPath(cardID), nil, &out); err != nil {
		return favro.CardDependencies{}, err
	}
	return out, nil
}

// dependenciesBody is the request wrapper both the create and the
// replace endpoints take.
type dependenciesBody struct {
	Dependencies []favro.CardDependencyOption `json:"dependencies"`
}

// CreateDependencies ADDS dependencies to a card, leaving existing
// ones in place. Returns the card's full dependency list afterwards.
func (c *Client) CreateDependencies(ctx context.Context, cardID string, deps []favro.CardDependencyOption) (favro.CardDependencies, error) {
	return c.writeDependencies(ctx, http.MethodPost, cardID, deps)
}

// ReplaceDependencies REPLACES a card's dependency list: every
// existing dependency is removed and the supplied set becomes the
// whole list. Use CreateDependencies to add without clearing.
func (c *Client) ReplaceDependencies(ctx context.Context, cardID string, deps []favro.CardDependencyOption) (favro.CardDependencies, error) {
	return c.writeDependencies(ctx, http.MethodPut, cardID, deps)
}

// writeDependencies is the shared body of the add (POST) and replace
// (PUT) paths — they differ only in method.
func (c *Client) writeDependencies(ctx context.Context, method, cardID string, deps []favro.CardDependencyOption) (favro.CardDependencies, error) {
	if cardID == "" {
		return favro.CardDependencies{}, errMissingID
	}
	if len(deps) == 0 {
		return favro.CardDependencies{}, fmt.Errorf("favro: at least one dependency is required")
	}
	for i, d := range deps {
		if d.CardID == "" {
			return favro.CardDependencies{}, fmt.Errorf("favro: dependency %d is missing cardId", i)
		}
	}
	var out favro.CardDependencies
	if err := c.doJSON(ctx, method, dependenciesPath(cardID), nil, dependenciesBody{Dependencies: deps}, &out); err != nil {
		return favro.CardDependencies{}, err
	}
	return out, nil
}

// UpdateDependency changes one dependency's direction. Returns the
// card's full dependency list afterwards.
func (c *Client) UpdateDependency(ctx context.Context, cardID, dependencyCardID string, req favro.UpdateDependencyRequest) (favro.CardDependencies, error) {
	if cardID == "" || dependencyCardID == "" {
		return favro.CardDependencies{}, errMissingID
	}
	var out favro.CardDependencies
	path := dependenciesPath(cardID) + "/" + url.PathEscape(dependencyCardID)
	if err := c.PatchJSON(ctx, path, req, &out); err != nil {
		return favro.CardDependencies{}, err
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
