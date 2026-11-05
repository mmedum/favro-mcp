package livecover

import (
	"fmt"
	"strings"
	"testing"
)

// A code comment cites a live step or a -writes check by its name, so
// every step and check has one and no two share it. A failure names each
// by the list it is written in and its index there.
func TestEveryStepAndCheckHasANameNoOtherHas(t *testing.T) {
	type named struct{ site, name string }
	var all []named
	for _, list := range []struct {
		name  string
		steps []Step
	}{
		{"SeedSteps", SeedSteps()},
		{"optionSteps", optionSteps()},
		{"extraSteps", extraSteps()},
	} {
		for i, s := range list.steps {
			all = append(all, named{fmt.Sprintf("%s()[%d] (%s)", list.name, i, s.Tool), s.Name})
		}
	}
	for c := range checkCount {
		all = append(all, named{fmt.Sprintf("checkNames[%d]", c), c.Name()})
	}
	taken := map[string]string{}
	for _, n := range all {
		if strings.TrimSpace(n.name) == "" {
			t.Errorf("%s has no name", n.site)
			continue
		}
		if first, ok := taken[n.name]; ok {
			t.Errorf("%s and %s are both named %q", first, n.site, n.name)
			continue
		}
		taken[n.name] = n.site
	}
	// The lists read here are every list Names gives.
	if got := len(Names()); len(all) != got {
		t.Errorf("read %d names, and Names gives %d", len(all), got)
	}
}

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
					t.Errorf("a %s step (%q) shares with %v and expects it to go through; name %s", s.Tool, s.Name, id, OwnUserID)
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
