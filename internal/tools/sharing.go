package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mmedum/favro-mcp/v4/internal/favro"
	"github.com/mmedum/favro-mcp/v4/internal/render"
	"github.com/mmedum/favro-mcp/v4/internal/service"
)

// Sharing inside the organization (§9.2). A client remembers an
// approval per tool, never per argument, so a wide share carried as an
// argument rides along unseen once the tool is always allowed. The
// ordinary writes therefore give access only to people already in the
// organization, named by userId, and to its groups, and make nobody a
// new owner. Opening a collection to the whole organization or to anyone
// with its link is favro_widen_collection_sharing, behind the
// destructive flag. Invitations by email, admins, and guests made a
// widget's owners or editors have no tool here at all; the person does
// those in Favro.

// insideRoles are the organization roles of the people inside it. Any
// other role, and a role Favro adds later, is someone this server does
// not share with: a guest, an external member, a disabled account.
var insideRoles = map[string]bool{"administrator": true, "fullMember": true}

// grant is one entry of a write that gives access: a person or group
// and the role it would hold.
type grant struct {
	// at is where the entry sits in the call, for the refusal:
	// "share_to_users[0]".
	at                         string
	typ, userID, groupID, mail string
	role                       string
	// remove is an entry that takes access away, which widens nothing.
	remove bool
}

// isGroup is a grant that names a group rather than a person.
func (g grant) isGroup() bool { return strings.EqualFold(g.typ, "group") }

// key is who the grant names, as held keys it.
func (g grant) key() string {
	if g.isGroup() {
		return "group:" + g.groupID
	}
	return "user:" + g.userID
}

// sharedGrants are a collection's share entries as grants.
func sharedGrants(field string, users []favro.SharedUser) []grant {
	out := make([]grant, len(users))
	for i, u := range users {
		out[i] = grant{
			at: fmt.Sprintf("%s[%d]", field, i), typ: u.Type, userID: u.UserID, groupID: u.GroupID,
			mail: u.Email, role: u.Role, remove: u.Delete != nil && *u.Delete,
		}
	}
	return out
}

// memberGrants are a group's member entries as grants.
func memberGrants(members []favro.GroupMember) []grant {
	out := make([]grant, len(members))
	for i, m := range members {
		out[i] = grant{
			at: fmt.Sprintf("members[%d]", i), userID: m.UserID, mail: m.Email, role: m.Role,
			remove: m.Delete != nil && *m.Delete,
		}
	}
	return out
}

// heldBy is who already has access, read from Favro as grants, by
// grant key, with their role.
func heldBy(grants []grant) map[string]string {
	held := make(map[string]string, len(grants))
	for _, g := range grants {
		held[g.key()] = g.role
	}
	return held
}

// heldFunc reads who already has access, by grant key, with their role.
type heldFunc func() (map[string]string, error)

// noneHeld is what a create holds: nobody.
func noneHeld() (map[string]string, error) { return nil, nil }

// checkGrants refuses the grants of an ordinary write that reach past
// the organization or make a new owner. owner is the role that makes
// one: "admin" on a collection, "administrator" on a group. held reads
// who already has access, from Favro for an update: an entry repeating
// what someone already holds widens nothing, which matters for
// favro_update_group, whose member list replaces the whole one. The
// entries' own shape is checked first, so such a refusal costs no
// request, and nothing is read for a call that only removes.
func checkGrants(ctx context.Context, r *service.Resolver, tool, owner string, held heldFunc, grants []grant) error {
	var given []grant
	for _, g := range grants {
		if g.remove {
			continue
		}
		if err := checkShape(tool, g); err != nil {
			return err
		}
		given = append(given, g)
	}
	if len(given) == 0 {
		return nil
	}
	had, err := held()
	if err != nil {
		return err
	}
	newcomers, err := checkOwners(tool, owner, had, given)
	if err != nil || len(newcomers) == 0 {
		return err
	}
	return checkMembers(ctx, r, tool, newcomers)
}

// checkOwners refuses a grant that makes a new owner, and returns the
// people the grants name who hold nothing yet.
func checkOwners(tool, owner string, had map[string]string, given []grant) ([]grant, error) {
	var newcomers []grant
	for _, g := range given {
		if strings.EqualFold(strings.TrimSpace(g.role), owner) && !strings.EqualFold(had[g.key()], owner) {
			return nil, render.Errorf(render.ClassBlocked, "%s: %s would make a new %s, who can change who else has "+
				"access; nothing was changed. This server makes nobody a new %s; the person can do that in Favro",
				tool, g.at, owner, owner)
		}
		if _, ok := had[g.key()]; !ok && !g.isGroup() {
			newcomers = append(newcomers, g)
		}
	}
	return newcomers, nil
}

// checkMembers refuses a newcomer the organization does not list as a
// member.
func checkMembers(ctx context.Context, r *service.Resolver, tool string, newcomers []grant) error {
	ids := make([]string, len(newcomers))
	for i, g := range newcomers {
		ids[i] = g.userID
	}
	roles, err := r.OrganizationRoles(ctx, ids)
	if err != nil {
		return err
	}
	for _, g := range newcomers {
		role, ok := roles[g.userID]
		switch {
		case !ok:
			return render.Errorf(render.ClassBlocked, "%s: %s names a userId that is not in this organization; "+
				"nothing was changed. Name people already in it by userId, which favro_resolve_user finds", tool, g.at)
		case !insideRoles[role]:
			return render.Errorf(render.ClassBlocked, "%s: %s names someone whose organization role is %q, so they "+
				"are not a member of the organization; nothing was changed. This server shares only with members; "+
				"the person can share with anyone else in Favro", tool, g.at, role)
		}
	}
	return nil
}

// checkShape refuses an entry by its shape alone: whom it names and how,
// before anything is read.
func checkShape(tool string, g grant) error {
	switch {
	case strings.EqualFold(g.typ, "organization"):
		return render.Errorf(render.ClassBlocked, "%s: %s shares with the whole organization; nothing was changed. "+
			"favro_widen_collection_sharing with public_sharing: 'organization' does that, and is registered only "+
			"with FAVRO_ENABLE_DESTRUCTIVE=true", tool, g.at)
	case g.typ != "" && !strings.EqualFold(g.typ, "user") && !g.isGroup():
		return render.Errorf(render.ClassBlocked, "%s: %s has type %q; this server shares only with a user or a "+
			"group. Nothing was changed", tool, g.at, g.typ)
	case g.mail != "":
		return render.Errorf(render.ClassBlocked, "%s: %s names someone by email, which can invite a person from "+
			"outside the organization; nothing was changed. Name people already in it by userId, which "+
			"favro_resolve_user finds. This server sends no invitations; the person can invite anyone else in Favro",
			tool, g.at)
	case g.isGroup() && g.groupID == "":
		return fmt.Errorf("%s: %s is a group and names no groupId", tool, g.at)
	case !g.isGroup() && g.userID == "":
		return fmt.Errorf("%s: %s names no userId; name people already in the organization by userId, which "+
			"favro_resolve_user finds", tool, g.at)
	}
	return nil
}

// errOwnerRoleGuests is a widget write that would make every guest on
// the widget one of its owners.
var errOwnerRoleGuests = render.Sentinel(render.ClassBlocked,
	"owner_role 'guests' makes every guest on the widget an owner, people outside the organization among them; "+
		"nothing was changed. This server makes no guest an owner; the person can do that in Favro. Omit "+
		"owner_role to keep the current one")

// errEditRoleGuests is a widget write that would let every guest on the
// widget edit it.
var errEditRoleGuests = render.Sentinel(render.ClassBlocked,
	"edit_role 'guests' lets every guest on the widget edit it, people outside the organization among them; "+
		"nothing was changed. This server gives no guest edit rights; the person can do that in Favro. Omit "+
		"edit_role to keep the current one")

// checkWidgetRoles refuses owner_role or edit_role 'guests'.
func checkWidgetRoles(ownerRole, editRole string) error {
	if strings.EqualFold(strings.TrimSpace(ownerRole), "guests") {
		return errOwnerRoleGuests
	}
	if strings.EqualFold(strings.TrimSpace(editRole), "guests") {
		return errEditRoleGuests
	}
	return nil
}

// The collection sharing modes, as Favro spells them.
const (
	sharingUsers        = "users"
	sharingOrganization = "organization"
	sharingPublic       = "public"
)

// sharingMode is a public_sharing value as Favro spells the modes.
func sharingMode(mode string) string { return strings.ToLower(strings.TrimSpace(mode)) }

// checkPublicSharing refuses a mode the ordinary collection writes do
// not set: anything but 'users', which is the default and the narrowest.
func checkPublicSharing(tool, mode string) error {
	switch sharingMode(mode) {
	case "", sharingUsers:
		return nil
	case sharingOrganization, sharingPublic:
		return render.Errorf(render.ClassBlocked, "%s: public_sharing %q opens the collection past the people "+
			"it is shared with; nothing was changed. favro_widen_collection_sharing does that, and is registered "+
			"only with FAVRO_ENABLE_DESTRUCTIVE=true", tool, mode)
	}
	return render.Errorf(render.ClassBlocked, "%s: public_sharing %q is not a mode this tool sets; it sets only "+
		"'users'. Nothing was changed", tool, mode)
}
