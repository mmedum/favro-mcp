package favro

import "net/url"

// Collection is a Favro collection — a named grouping of widgets
// (boards) inside an organization. Fields outside this struct are
// ignored on decode (forward-compatible).
type Collection struct {
	CollectionID   string       `json:"collectionId"`
	OrganizationID string       `json:"organizationId,omitempty"`
	Name           string       `json:"name"`
	Color          string       `json:"color,omitempty"`
	Background     string       `json:"background,omitempty"`
	SharedToUsers  []SharedUser `json:"sharedToUsers,omitempty"`
	// PublicSharing is one of "users", "organization", "public" per
	// Favro's docs. Left as a string because Favro could extend the
	// set without notice and a typed enum would silently mask new
	// values.
	PublicSharing string `json:"publicSharing,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
	// FullMembersCanAddWidgets is the documented field name, and the
	// key Favro sends.
	FullMembersCanAddWidgets bool `json:"fullMembersCanAddWidgets,omitempty"`
}

// ListCollectionsFilter bundles the documented query parameters for
// /collections. Only `archived` is documented; a zero filter
// requests the active collections.
type ListCollectionsFilter struct {
	// Archived, when true, includes archived collections beside the
	// active ones.
	Archived bool
}

// Values returns the filter as url.Values.
func (f ListCollectionsFilter) Values() url.Values {
	q := url.Values{}
	// Favro answers no archived parameter with active and archived
	// collections, archived=false with active ones only, and
	// archived=true with archived ones only. Its reference says only "If
	// true, return archived collections. Optional." Checked by hand
	// against a live organization; the live steps "list collections" and
	// "list collections, archived ones included" read each form.
	// https://favro.com/developer/#get-all-collections
	if !f.Archived {
		q.Set("archived", "false")
	}
	return q
}

// CreateCollectionRequest is the body for POST /collections. Name is
// required; the rest are optional.
//
// Note the asymmetry with the read shape: a Collection comes back
// with its members in `sharedToUsers`, but invites are *sent* in
// `shareToUsers` (no "d"). They are different wire keys for the same
// concept, and posting the read-shaped key silently drops the
// invites.
//
// Color and IconName are not in Favro's documented parameter list.
// Favro accepts a body carrying them; `color` is ignored on write, and
// Favro derives `background` instead.
type CreateCollectionRequest struct {
	Name                     string       `json:"name"`
	ShareToUsers             []SharedUser `json:"shareToUsers,omitempty"`
	PublicSharing            string       `json:"publicSharing,omitempty"`
	Background               string       `json:"background,omitempty"`
	StarPage                 *bool        `json:"starPage,omitempty"`
	Color                    string       `json:"color,omitempty"`
	IconName                 string       `json:"iconName,omitempty"`
	FullMembersCanAddWidgets *bool        `json:"fullMembersCanAddWidgets,omitempty"`
}

// UpdateCollectionRequest is the body for PUT /collections/{collectionId}.
// Every field is optional; absent ones are left untouched. Archive
// is *bool so &false (unarchive) is distinguishable from "don't touch".
//
// Membership changes travel through two distinct keys, matching
// Favro's documented parameter list: ShareToUsers *invites* people
// who aren't in the collection yet, while Members updates the role
// of — or, with Delete set, removes — someone already in it. Neither
// is the read-shaped `sharedToUsers` key, which Favro ignores here.
type UpdateCollectionRequest struct {
	Name                     string       `json:"name,omitempty"`
	PublicSharing            string       `json:"publicSharing,omitempty"`
	Background               string       `json:"background,omitempty"`
	StarPage                 *bool        `json:"starPage,omitempty"`
	Color                    string       `json:"color,omitempty"`
	IconName                 string       `json:"iconName,omitempty"`
	FullMembersCanAddWidgets *bool        `json:"fullMembersCanAddWidgets,omitempty"`
	ShareToUsers             []SharedUser `json:"shareToUsers,omitempty"`
	Members                  []SharedUser `json:"members,omitempty"`
	Archive                  *bool        `json:"archive,omitempty"`
}
