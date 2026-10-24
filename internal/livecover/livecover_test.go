package livecover

import "testing"

// The server refuses a share with anyone it cannot find among the
// organization's members, so a share step expected to go through names
// the signed-in account, the one person certain to be there. A live run
// shared with AnyUserID, the first userId any result carried, and the
// server refused it as outside the organization. Each tool that shares
// also keeps a step expecting that refusal for OutsideUserID.
func TestShareStepsNameTheSignedInAccountAndSeeTheRefusal(t *testing.T) {
	shares, refuses := map[string]bool{}, map[string]bool{}
	for _, s := range Steps() {
		for _, id := range sharedUserIDs(s.Args) {
			switch {
			case s.ExpectError == "":
				shares[s.Tool] = true
				if id != OwnUserID {
					t.Errorf("a %s step (%q) shares with %v and expects it to go through; name %s", s.Tool, s.Why, id, OwnUserID)
				}
			case s.ExpectError == "blocked" && id == OutsideUserID:
				refuses[s.Tool] = true
			}
		}
	}
	// favro_create_collection, favro_update_collection,
	// favro_create_group and favro_update_group.
	const minShareTools = 4
	if len(shares) < minShareTools {
		t.Fatalf("found share steps for %d tools, expected at least %d", len(shares), minShareTools)
	}
	for tool := range shares {
		if !refuses[tool] {
			t.Errorf("%s has a share step and none that expects [blocked] for %s", tool, OutsideUserID)
		}
	}
}

// sharedUserIDs is every userId an entry in a step's arguments names, at
// any depth.
func sharedUserIDs(v any) []any {
	var ids []any
	switch t := v.(type) {
	case map[string]any:
		if id, ok := t["userId"]; ok {
			ids = append(ids, id)
		}
		for _, child := range t {
			ids = append(ids, sharedUserIDs(child)...)
		}
	case []any:
		for _, child := range t {
			ids = append(ids, sharedUserIDs(child)...)
		}
	}
	return ids
}
