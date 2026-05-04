package server

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/mmedum/favro-mcp/internal/cache"
	"github.com/mmedum/favro-mcp/internal/favro"
)

// Resolver bridges name-based lookups to Favro's id-based world.
// LLM workflows generally start from a human reference ("the
// frontend tag", "Engineers group") and need a CardCommonID,
// TagID, etc. before they can call any read CRUD primitive.
//
// The resolver caches each org-global resource list in memory so
// repeated lookups don't burn the rate-limit budget on the same
// data. TTLs follow plan §7:
//   - 5 minutes for slow-changing org metadata: tags, users,
//     custom fields, groups.
//   - 60 seconds for collection-scoped lists: widgets, columns,
//     collections themselves.
//
// Phase 4.1 ships only the tag resolver as a vertical slice; the
// other six follow in Phase 4.2 with the same shape.
type Resolver struct {
	client *favro.Client

	// tagCache holds the full org-global tag list. The cache key is
	// always tagCacheKey for now (single-org-per-process), but the
	// indirection leaves room for a multi-org keying scheme later
	// without rewriting every caller.
	tagCache cache.TTL[[]favro.Tag]
}

const (
	tagCacheKey = "tags"
	tagCacheTTL = 5 * time.Minute
)

// NewResolver builds a Resolver wrapping the given client. Caches
// start empty.
func NewResolver(client *favro.Client) *Resolver {
	return &Resolver{client: client}
}

// listAllTags returns every tag in the org. Cached for tagCacheTTL.
// Pass forceRefresh=true to bypass the cache and re-fetch from
// Favro; on success the cache is repopulated. The returned
// `cached` is true when the result came from the cache and Favro
// was not contacted, false when this call hit Favro.
//
// Delegates the page walk to favro.Paginate so the requestID
// round-trip lives in one place; the list is small in practice
// (Favro tags are short org-global metadata) and amortized by
// the 5-minute TTL even if an org ever grows thousands of tags.
func (r *Resolver) listAllTags(ctx context.Context, forceRefresh bool) (tags []favro.Tag, cached bool, err error) {
	if !forceRefresh {
		if hit, ok := r.tagCache.Get(tagCacheKey); ok {
			return hit, true, nil
		}
	}
	var all []favro.Tag
	if err := favro.Paginate(ctx, r.client, "/tags", nil, func(env favro.PageEnvelope[favro.Tag]) error {
		all = append(all, env.Entities...)
		return nil
	}); err != nil {
		return nil, false, err
	}
	r.tagCache.Set(tagCacheKey, all, tagCacheTTL)
	return all, false, nil
}

// invalidateTagCache drops the cached tag list. Phase 6's
// favro_create_tag / favro_delete_tag / favro_update_tags will call
// this on any successful mutation so the next resolve sees fresh
// data.
func (r *Resolver) invalidateTagCache() {
	r.tagCache.Invalidate(tagCacheKey)
}

// ResolvedTag is one ranked tag candidate returned by ResolveTag.
type ResolvedTag struct {
	TagID string  `json:"tag_id"`
	Name  string  `json:"name"`
	Color string  `json:"color,omitempty"`
	Score float64 `json:"score"`
}

// ResolveTag matches name against the cached tag list and returns
// up to limit candidates ranked by score descending, then name
// ascending (for stable ordering when scores tie).
//
// limit <= 0 maps to the default of 10; limit > 50 is capped at 50
// to match plan §6a.
//
// Score semantics (in [0, 1]):
//   - 1.0: exact case-insensitive match.
//   - 0.7: case-insensitive prefix match.
//   - 0.4: case-insensitive substring match.
//   - 0:   no match (filtered out).
//
// The simple ranking is intentional. Tags are short human-typed
// strings; an LLM will get more leverage from "I asked for 'eng'
// and you returned ['Engineering' (0.7), 'Engineer-tools' (0.7),
// 'Re-engineered' (0.4)]" than from a fancier algorithm that
// makes the disambiguation opaque.
func (r *Resolver) ResolveTag(ctx context.Context, name string, limit int, forceRefresh bool) (matches []ResolvedTag, cached bool, err error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	tags, cached, err := r.listAllTags(ctx, forceRefresh)
	if err != nil {
		return nil, cached, err
	}
	matches = make([]ResolvedTag, 0, limit)
	if name == "" {
		return matches, cached, nil
	}
	// Lowercase the query once; scoreLowered is N times cheaper than
	// scoreNameMatch when N is the number of tags in the org.
	lowerQuery := strings.ToLower(name)
	for _, t := range tags {
		score := scoreLowered(lowerQuery, strings.ToLower(t.Name))
		if score == 0 {
			continue
		}
		matches = append(matches, ResolvedTag{
			TagID: t.TagID,
			Name:  t.Name,
			Color: t.Color,
			Score: score,
		})
	}
	slices.SortStableFunc(matches, func(a, b ResolvedTag) int {
		if a.Score != b.Score {
			// Higher score first.
			return cmp.Compare(b.Score, a.Score)
		}
		return cmp.Compare(a.Name, b.Name)
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, cached, nil
}

// scoreLowered returns the match score given an already-lowercased
// query and an already-lowercased candidate. Callers lowercase once
// per call rather than once per candidate. See ResolveTag for the
// score scale.
func scoreLowered(lowerQuery, lowerName string) float64 {
	switch {
	case lowerQuery == "":
		return 0
	case lowerName == lowerQuery:
		return 1.0
	case strings.HasPrefix(lowerName, lowerQuery):
		return 0.7
	case strings.Contains(lowerName, lowerQuery):
		return 0.4
	default:
		return 0
	}
}
