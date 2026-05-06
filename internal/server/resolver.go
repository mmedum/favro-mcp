package server

import (
	"cmp"
	"context"
	"errors"
	"net/url"
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
// One Resolver per *mcp.Server so the cache state is process-wide;
// ad-hoc resolvers per-tool would each maintain their own cache
// and burn the budget on parallel cold-start fetches.
type Resolver struct {
	client *favro.Client

	// Cache fields. The cache key is "<resource>" for the org-global
	// caches and "<resource>:<scope-id>" where the underlying API
	// requires scoping (columns require widgetCommonId; widget
	// caches the unfiltered org-wide list and filters in-memory).
	tagCache         cache.TTL[[]favro.Tag]
	userCache        cache.TTL[[]favro.User]
	collectionCache  cache.TTL[[]favro.Collection]
	widgetCache      cache.TTL[[]favro.Widget]
	columnCache      cache.TTL[[]favro.Column]
	customFieldCache cache.TTL[[]favro.CustomField]
	groupCache       cache.TTL[[]favro.Group]
	// searchCardCache stores the pre-stripped, pre-lowercased corpus
	// per (scope, scopeID, includeArchived) so repeated
	// favro_search_cards queries against the same scope skip both the
	// HTTP fetch and the per-card markdown sweep + ToLower. Plan §7's
	// 60s scoped TTL. Keys are namespaced under "search:" so a single
	// InvalidatePrefix call drops them all on a card mutation.
	searchCardCache cache.TTL[scopedCorpus]
}

// Cache keys + TTLs. Per-resource sentinels (rather than a single
// "" key) so a future multi-org keying scheme can prefix without
// rewriting every caller.
const (
	tagCacheKey         = "tags"
	userCacheKey        = "users"
	collectionCacheKey  = "collections"
	widgetCacheKey      = "widgets"
	customFieldCacheKey = "customfields"
	groupCacheKey       = "groups"
	// columnCacheKeyPrefix is composed with the widgetCommonID since
	// Favro's /columns endpoint requires that filter on every call.
	columnCacheKeyPrefix = "columns:"

	// Slow-changing org metadata.
	tagCacheTTL         = 5 * time.Minute
	userCacheTTL        = 5 * time.Minute
	customFieldCacheTTL = 5 * time.Minute
	groupCacheTTL       = 5 * time.Minute

	// Collection-scoped lists. Shorter TTL because users add /
	// rename / archive these mid-session.
	collectionCacheTTL = 60 * time.Second
	widgetCacheTTL     = 60 * time.Second
	columnCacheTTL     = 60 * time.Second
)

// NewResolver builds a Resolver wrapping the given client. Caches
// start empty.
func NewResolver(client *favro.Client) *Resolver {
	return &Resolver{client: client}
}

// listAllCached fetches every page of T at path, caching the
// concatenated result under key for ttl. Pass forceRefresh=true
// to bypass the cache; the returned `cached` flag is true when
// the result came from the cache and Favro was not contacted.
// The page walk delegates to favro.Paginate so the requestID
// round-trip protocol stays in one place.
func listAllCached[T any](
	ctx context.Context,
	client *favro.Client,
	cacheStore *cache.TTL[[]T],
	key string,
	ttl time.Duration,
	path string,
	query url.Values,
	forceRefresh bool,
) (items []T, cached bool, err error) {
	if !forceRefresh {
		if hit, ok := cacheStore.Get(key); ok {
			return hit, true, nil
		}
	}
	var all []T
	if err := favro.Paginate(ctx, client, path, query, func(env favro.PageEnvelope[T]) error {
		all = append(all, env.Entities...)
		return nil
	}); err != nil {
		return nil, false, err
	}
	cacheStore.Set(key, all, ttl)
	return all, false, nil
}

// scoreLowered returns the match score given an already-lowercased
// query and an already-lowercased candidate. Callers lowercase once
// per call rather than once per candidate. The score scale is the
// one published to LLMs as resolveScoreScaleDoc; the literal returns
// below are its single source of truth, and 0 means "filter out".
// Empty-query handling lives in rankByName (the only caller), which
// short-circuits before scoring; this function never sees an empty
// lowerQuery in practice.
//
// The simple ranking is intentional. Resource names are short
// human-typed strings; an LLM gets more leverage from a transparent
// exact / prefix / substring tier ladder than from a fancier
// algorithm that makes the disambiguation opaque.
func scoreLowered(lowerQuery, lowerName string) float64 {
	switch {
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

// clampLimit normalizes the resolver `limit` parameter to plan §6a
// bounds: limit <= 0 -> default of 10; limit > 50 -> capped at 50.
// Centralized so the bounds live in one place.
func clampLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 50 {
		return 50
	}
	return limit
}

// rankByName centralises the score → filter → sort → cap pattern
// every Resolve<X> follows. T is the source type (favro.Tag,
// favro.User, …); R is the resolver's per-resource output type
// (ResolvedTag, ResolvedUser, …).
//
// scoreItem is given the already-lowercased query and one item; it
// returns the item's match score and the display name used for the
// score-tie tie-break. Returning score == 0 filters the item out,
// which callers can exploit for pre-filtering or cross-field
// scoring (per-resource specifics live on each Resolve<X>).
//
// convert projects (item, score) into the per-resource output
// struct after sort + cap have applied.
//
// Empty name returns an empty slice — empty query is not a
// wildcard. Sort is score desc, then name asc (stable).
func rankByName[T, R any](
	items []T,
	name string,
	limit int,
	scoreItem func(lowerQuery string, item T) (score float64, displayName string),
	convert func(item T, score float64) R,
) []R {
	limit = clampLimit(limit)
	out := make([]R, 0, limit)
	if name == "" {
		return out
	}
	type pick struct {
		idx   int
		name  string
		score float64
	}
	lq := strings.ToLower(name)
	// Pre-size for the case where most items match the query —
	// avoids the append-doubling cascade for org-sized inputs.
	// Convert work later is bounded by limit (cap-then-convert),
	// so this overallocates only the small pick struct, never R.
	picks := make([]pick, 0, len(items))
	for i, t := range items {
		s, n := scoreItem(lq, t)
		if s == 0 {
			continue
		}
		picks = append(picks, pick{idx: i, name: n, score: s})
	}
	slices.SortStableFunc(picks, func(a, b pick) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		return cmp.Compare(a.name, b.name)
	})
	if len(picks) > limit {
		picks = picks[:limit]
	}
	for _, p := range picks {
		out = append(out, convert(items[p.idx], p.score))
	}
	return out
}

// ============================================================
// Output types — one Resolved<X> per resource.
// All carry a Score in [0, 1]; field projections vary by resource.
// ============================================================

// ResolvedTag is one ranked tag candidate.
type ResolvedTag struct {
	TagID string  `json:"tag_id"`
	Name  string  `json:"name"`
	Color string  `json:"color,omitempty"`
	Score float64 `json:"score"`
}

// ResolvedUser is one ranked user candidate. Email is included
// because two users may share a display-name prefix and the email
// is the disambiguator humans actually use.
type ResolvedUser struct {
	UserID string  `json:"user_id"`
	Name   string  `json:"name"`
	Email  string  `json:"email,omitempty"`
	Score  float64 `json:"score"`
}

// ResolvedCollection is one ranked collection candidate.
type ResolvedCollection struct {
	CollectionID string  `json:"collection_id"`
	Name         string  `json:"name"`
	Score        float64 `json:"score"`
}

// ResolvedWidget is one ranked widget candidate. Type and
// CollectionIDs help the LLM disambiguate between widgets sharing
// a name (e.g. multiple "Sprint Board" widgets in different
// collections).
type ResolvedWidget struct {
	WidgetCommonID string   `json:"widget_common_id"`
	Name           string   `json:"name"`
	Type           string   `json:"type,omitempty"`
	CollectionIDs  []string `json:"collection_ids,omitempty"`
	Score          float64  `json:"score"`
}

// ResolvedColumn is one ranked column candidate. Position is
// included because column names ("Doing", "Done") repeat across
// widgets and users often refer to "the second column".
type ResolvedColumn struct {
	ColumnID       string  `json:"column_id"`
	WidgetCommonID string  `json:"widget_common_id"`
	Name           string  `json:"name"`
	Position       int     `json:"position"`
	Score          float64 `json:"score"`
}

// ResolvedCustomField is one ranked custom-field candidate. Type
// is the user-facing display label ("Single select", "Date", etc.)
// useful for disambiguation when an org has many fields named
// "Priority" of different kinds.
type ResolvedCustomField struct {
	CustomFieldID string  `json:"custom_field_id"`
	Name          string  `json:"name"`
	Type          string  `json:"type,omitempty"`
	Score         float64 `json:"score"`
}

// ResolvedGroup is one ranked group candidate.
type ResolvedGroup struct {
	GroupID string  `json:"group_id"`
	Name    string  `json:"name"`
	Score   float64 `json:"score"`
}

// ============================================================
// Tag resolver
// ============================================================

func (r *Resolver) listAllTags(ctx context.Context, forceRefresh bool) ([]favro.Tag, bool, error) {
	return listAllCached(ctx, r.client, &r.tagCache, tagCacheKey, tagCacheTTL, "/tags", nil, forceRefresh)
}

func (r *Resolver) invalidateTagCache() {
	r.tagCache.Invalidate(tagCacheKey)
}

// ResolveTag matches name against the cached tag list and returns
// up to limit candidates ranked by score descending, then name
// ascending (for stable ordering when scores tie).
func (r *Resolver) ResolveTag(ctx context.Context, name string, limit int, forceRefresh bool) (matches []ResolvedTag, cached bool, err error) {
	tags, cached, err := r.listAllTags(ctx, forceRefresh)
	if err != nil {
		return nil, cached, err
	}
	matches = rankByName(tags, name, limit,
		func(lq string, t favro.Tag) (float64, string) {
			return scoreLowered(lq, strings.ToLower(t.Name)), t.Name
		},
		func(t favro.Tag, s float64) ResolvedTag {
			return ResolvedTag{TagID: t.TagID, Name: t.Name, Color: t.Color, Score: s}
		},
	)
	return matches, cached, nil
}

// ============================================================
// User resolver
// ============================================================

func (r *Resolver) listAllUsers(ctx context.Context, forceRefresh bool) ([]favro.User, bool, error) {
	return listAllCached(ctx, r.client, &r.userCache, userCacheKey, userCacheTTL, "/users", nil, forceRefresh)
}

func (r *Resolver) invalidateUserCache() {
	r.userCache.Invalidate(userCacheKey)
}

// ResolveUser matches name against display-name OR email so an LLM
// asking for "casper@example.com" or "Casper" both work. Email
// matches use the same score scale as name matches; the per-item
// score is the better of the two. Tie-break is by display name
// ascending (so an LLM searching by display-name sees the
// expected ordering when multiple users have similar emails).
func (r *Resolver) ResolveUser(ctx context.Context, name string, limit int, forceRefresh bool) (matches []ResolvedUser, cached bool, err error) {
	users, cached, err := r.listAllUsers(ctx, forceRefresh)
	if err != nil {
		return nil, cached, err
	}
	matches = rankByName(users, name, limit,
		func(lq string, u favro.User) (float64, string) {
			s := scoreLowered(lq, strings.ToLower(u.Name))
			if e := scoreLowered(lq, strings.ToLower(u.Email)); e > s {
				s = e
			}
			return s, u.Name
		},
		func(u favro.User, s float64) ResolvedUser {
			return ResolvedUser{UserID: u.UserID, Name: u.Name, Email: u.Email, Score: s}
		},
	)
	return matches, cached, nil
}

// ============================================================
// Collection resolver
// ============================================================

func (r *Resolver) listAllCollections(ctx context.Context, forceRefresh bool) ([]favro.Collection, bool, error) {
	return listAllCached(ctx, r.client, &r.collectionCache, collectionCacheKey, collectionCacheTTL, "/collections", nil, forceRefresh)
}

func (r *Resolver) invalidateCollectionCache() {
	r.collectionCache.Invalidate(collectionCacheKey)
}

// ResolveCollection matches name against the cached collection list.
func (r *Resolver) ResolveCollection(ctx context.Context, name string, limit int, forceRefresh bool) (matches []ResolvedCollection, cached bool, err error) {
	collections, cached, err := r.listAllCollections(ctx, forceRefresh)
	if err != nil {
		return nil, cached, err
	}
	matches = rankByName(collections, name, limit,
		func(lq string, c favro.Collection) (float64, string) {
			return scoreLowered(lq, strings.ToLower(c.Name)), c.Name
		},
		func(c favro.Collection, s float64) ResolvedCollection {
			return ResolvedCollection{CollectionID: c.CollectionID, Name: c.Name, Score: s}
		},
	)
	return matches, cached, nil
}

// ============================================================
// Widget resolver
// ============================================================

func (r *Resolver) listAllWidgets(ctx context.Context, forceRefresh bool) ([]favro.Widget, bool, error) {
	return listAllCached(ctx, r.client, &r.widgetCache, widgetCacheKey, widgetCacheTTL, "/widgets", nil, forceRefresh)
}

func (r *Resolver) invalidateWidgetCache() {
	r.widgetCache.Invalidate(widgetCacheKey)
}

// ResolveWidget matches name against the cached widget list. If
// collectionID is non-empty, the candidate set is restricted to
// widgets that include that collection in their CollectionIDs —
// applied client-side via the score callback so we avoid a
// per-collection cache key with redundant data.
func (r *Resolver) ResolveWidget(ctx context.Context, name, collectionID string, limit int, forceRefresh bool) (matches []ResolvedWidget, cached bool, err error) {
	widgets, cached, err := r.listAllWidgets(ctx, forceRefresh)
	if err != nil {
		return nil, cached, err
	}
	matches = rankByName(widgets, name, limit,
		func(lq string, w favro.Widget) (float64, string) {
			if collectionID != "" && !slices.Contains(w.CollectionIDs, collectionID) {
				return 0, ""
			}
			return scoreLowered(lq, strings.ToLower(w.Name)), w.Name
		},
		func(w favro.Widget, s float64) ResolvedWidget {
			return ResolvedWidget{
				WidgetCommonID: w.WidgetCommonID,
				Name:           w.Name,
				Type:           w.Type,
				CollectionIDs:  w.CollectionIDs,
				Score:          s,
			}
		},
	)
	return matches, cached, nil
}

// ============================================================
// Column resolver — widget-scoped (Favro's /columns requires
// widgetCommonId, so the resolver does too; cache keyed per widget).
// ============================================================

// errMissingResolveWidgetCommonID is returned by ResolveColumn when
// widgetCommonID is empty. Column names ("Doing", "Done") repeat
// across widgets, so an org-wide column resolver would always
// return ambiguous garbage; the explicit error tells the LLM to
// resolve a widget first. Matches the codebase's other
// errMissing<X> sentinels (errMissingID, errMissingWidgetCommonID,
// errMissingCardCommonID).
var errMissingResolveWidgetCommonID = errors.New("favro: widget_common_id is required for resolving columns")

func (r *Resolver) listColumnsForWidget(ctx context.Context, widgetCommonID string, forceRefresh bool) ([]favro.Column, bool, error) {
	q := url.Values{}
	q.Set("widgetCommonId", widgetCommonID)
	return listAllCached(ctx, r.client, &r.columnCache, columnCacheKeyPrefix+widgetCommonID, columnCacheTTL, "/columns", q, forceRefresh)
}

func (r *Resolver) invalidateColumnCache(widgetCommonID string) {
	r.columnCache.Invalidate(columnCacheKeyPrefix + widgetCommonID)
}

// invalidateAllColumnCaches drops every per-widget column cache
// entry in one prefix sweep. Used by widget delete (which cascades
// into all of the widget's columns) and by column delete tools that
// don't know the parent widgetCommonID upfront.
func (r *Resolver) invalidateAllColumnCaches() {
	r.columnCache.InvalidatePrefix(columnCacheKeyPrefix)
}

// ResolveColumn matches name against columns scoped to a single
// widget. widgetCommonID is mandatory — column names repeat across
// widgets and an org-wide match would be meaningless ambiguity.
func (r *Resolver) ResolveColumn(ctx context.Context, widgetCommonID, name string, limit int, forceRefresh bool) (matches []ResolvedColumn, cached bool, err error) {
	if widgetCommonID == "" {
		return nil, false, errMissingResolveWidgetCommonID
	}
	columns, cached, err := r.listColumnsForWidget(ctx, widgetCommonID, forceRefresh)
	if err != nil {
		return nil, cached, err
	}
	matches = rankByName(columns, name, limit,
		func(lq string, c favro.Column) (float64, string) {
			return scoreLowered(lq, strings.ToLower(c.Name)), c.Name
		},
		func(c favro.Column, s float64) ResolvedColumn {
			return ResolvedColumn{
				ColumnID:       c.ColumnID,
				WidgetCommonID: c.WidgetCommonID,
				Name:           c.Name,
				Position:       c.Position,
				Score:          s,
			}
		},
	)
	return matches, cached, nil
}

// ============================================================
// Custom field resolver
// ============================================================

func (r *Resolver) listAllCustomFields(ctx context.Context, forceRefresh bool) ([]favro.CustomField, bool, error) {
	return listAllCached(ctx, r.client, &r.customFieldCache, customFieldCacheKey, customFieldCacheTTL, "/customfields", nil, forceRefresh)
}

func (r *Resolver) invalidateCustomFieldCache() {
	r.customFieldCache.Invalidate(customFieldCacheKey)
}

// ResolveCustomField matches name against the cached custom-field
// list. Type is included in the response because orgs frequently
// have multiple "Priority" / "Status" / "Estimation" fields of
// different kinds.
func (r *Resolver) ResolveCustomField(ctx context.Context, name string, limit int, forceRefresh bool) (matches []ResolvedCustomField, cached bool, err error) {
	fields, cached, err := r.listAllCustomFields(ctx, forceRefresh)
	if err != nil {
		return nil, cached, err
	}
	matches = rankByName(fields, name, limit,
		func(lq string, f favro.CustomField) (float64, string) {
			return scoreLowered(lq, strings.ToLower(f.Name)), f.Name
		},
		func(f favro.CustomField, s float64) ResolvedCustomField {
			return ResolvedCustomField{
				CustomFieldID: f.CustomFieldID,
				Name:          f.Name,
				Type:          f.Type,
				Score:         s,
			}
		},
	)
	return matches, cached, nil
}

// ============================================================
// Group resolver
// ============================================================

func (r *Resolver) listAllGroups(ctx context.Context, forceRefresh bool) ([]favro.Group, bool, error) {
	return listAllCached(ctx, r.client, &r.groupCache, groupCacheKey, groupCacheTTL, "/groups", nil, forceRefresh)
}

func (r *Resolver) invalidateGroupCache() {
	r.groupCache.Invalidate(groupCacheKey)
}

// ResolveGroup matches name against the cached group list.
func (r *Resolver) ResolveGroup(ctx context.Context, name string, limit int, forceRefresh bool) (matches []ResolvedGroup, cached bool, err error) {
	groups, cached, err := r.listAllGroups(ctx, forceRefresh)
	if err != nil {
		return nil, cached, err
	}
	matches = rankByName(groups, name, limit,
		func(lq string, g favro.Group) (float64, string) {
			return scoreLowered(lq, strings.ToLower(g.Name)), g.Name
		},
		func(g favro.Group, s float64) ResolvedGroup {
			return ResolvedGroup{GroupID: g.GroupID, Name: g.Name, Score: s}
		},
	)
	return matches, cached, nil
}
