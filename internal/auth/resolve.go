package auth

import (
	"context"
	"errors"
	"log/slog"
)

// ResolvedToken is a usable Token paired with the name of the Source
// that produced it.
type ResolvedToken struct {
	Token  Token
	Source string
}

// resolveToken iterates sources in order and returns the first usable
// Token. Sources reporting errNotConfigured are skipped silently. Any
// other error stops resolution immediately — the alternative (silent
// fallthrough) hides real failures like a corrupt keyring entry behind
// a less-informative "no credentials" error.
func resolveToken(ctx context.Context, sources []Source) (ResolvedToken, error) {
	for _, s := range sources {
		tok, err := s.Load(ctx)
		switch {
		case err == nil:
			slog.Debug("resolved Favro credentials", "source", s.Name())
			return ResolvedToken{Token: tok, Source: s.Name()}, nil
		case errors.Is(err, errNotConfigured):
			slog.Debug("auth source not configured", "source", s.Name())
			continue
		default:
			return ResolvedToken{}, err
		}
	}
	return ResolvedToken{}, errNoCredentials
}

// ResolveDefault is the production resolver — env first, then keyring.
// Wraps resolveToken with defaultSources so callers don't restate the
// pair at every call site.
func ResolveDefault(ctx context.Context) (ResolvedToken, error) {
	return resolveToken(ctx, defaultSources())
}

// defaultSources returns the production Source list. Env wins so a
// quick override during debugging works without `favro-mcp auth login`.
func defaultSources() []Source {
	return []Source{
		EnvSource{},
		KeyringSource{},
	}
}
