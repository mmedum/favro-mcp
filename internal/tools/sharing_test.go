package tools

import (
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sharingFavro is an organization with members, a guest and an external
// member, one collection and one group that already share with some of
// them, and a record of every write. Fixtures are generated: no id or
// name here belongs to anyone.
type sharingFavro struct {
	t *testing.T

	mu     sync.Mutex
	writes []string // "PUT /collections/c-1"
	body   string   // the last write's body
	reads  []string // "GET /users"
	// sharing is what GET /collections/c-1 reports as publicSharing; a
	// PUT that sets it changes it, unless ignore is set.
	sharing string
	ignore  bool
}

func newSharingFavro(t *testing.T) *sharingFavro {
	return &sharingFavro{t: t, sharing: "users"}
}

func (f *sharingFavro) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	call := r.Method + " " + r.URL.Path
	if r.Method != http.MethodGet {
		b, _ := io.ReadAll(r.Body)
		f.writes, f.body = append(f.writes, call), string(b)
		if call == "PUT /collections/c-1" && !f.ignore {
			if strings.Contains(f.body, `"publicSharing":"organization"`) {
				f.sharing = "organization"
			}
			if strings.Contains(f.body, `"publicSharing":"public"`) {
				f.sharing = "public"
			}
		}
	} else {
		f.reads = append(f.reads, call)
	}
	switch {
	case call == "GET /users":
		_, _ = w.Write([]byte(`{"limit":100,"page":0,"pages":1,"requestId":"r","entities":[` +
			`{"userId":"u-1","name":"Member One","organizationRole":"fullMember"},` +
			`{"userId":"u-2","name":"Member Two","organizationRole":"fullMember"},` +
			`{"userId":"u-3","name":"Member Three","organizationRole":"fullMember"},` +
			`{"userId":"u-admin","name":"Org Admin","organizationRole":"administrator"},` +
			`{"userId":"u-guest","name":"A Guest","organizationRole":"guest"},` +
			`{"userId":"u-ext","name":"An External","organizationRole":"externalMember"}]}`))
	case call == "GET /collections/c-1" || strings.HasSuffix(call, " /collections/c-1") || call == "POST /collections":
		_, _ = w.Write([]byte(`{"collectionId":"c-1","name":"Eng","publicSharing":"` + f.sharing + `",` +
			`"sharedToUsers":[{"userId":"u-1","role":"admin"},{"userId":"u-guest","role":"view"},` +
			`{"type":"group","groupId":"g-1","role":"edit"}]}`))
	case call == "GET /groups/g-1" || strings.HasSuffix(call, " /groups/g-1") || call == "POST /groups":
		_, _ = w.Write([]byte(`{"groupId":"g-1","name":"Team","members":[` +
			`{"userId":"u-admin","role":"administrator"},{"userId":"u-guest","role":"member"}]}`))
	case strings.HasPrefix(r.URL.Path, "/widgets"):
		_, _ = w.Write([]byte(`{"widgetCommonId":"w-1","name":"Board","collectionIds":["c-1"]}`))
	default:
		f.t.Errorf("sharingFavro has no answer for %s", call)
		http.Error(w, "{}", http.StatusInternalServerError)
	}
}

// made is every write and read the fake saw.
func (f *sharingFavro) made() (writes, reads []string, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...), append([]string(nil), f.reads...), f.body
}

// call makes one tool call against a fresh server over f.
func (f *sharingFavro) call(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	cs := connectInMemoryWith(t, favroFixture(t, f))
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return res
}

// resultText is the readable half of a result.
func resultText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// An ordinary write gives access only to members of the organization,
// named by userId, and to its groups, and makes nobody a new owner. Each
// row is refused with its class before anything is written; the rows
// that name only people refuse before the organization is read.
func TestOrdinaryWritesShareOnlyInsideTheOrganization(t *testing.T) {
	t.Parallel()
	share := func(entry map[string]any) []any { return []any{entry} }
	for name, tc := range map[string]struct {
		tool  string
		args  map[string]any
		class string
		// readsUsers is whether the refusal needed the organization's
		// member list.
		readsUsers bool
	}{
		"an invitation by email": {createCollectionToolName, map[string]any{
			"name": "x", "share_to_users": share(map[string]any{"email": "someone@example.invalid", "role": "view"}),
		}, "blocked", false},
		"an email beside a userId": {updateCollectionToolName, map[string]any{
			"collection_id": "c-1", "share_to_users": share(map[string]any{"userId": "u-3", "email": "x@example.invalid", "role": "view"}),
		}, "blocked", false},
		"the whole organization as an entry": {createCollectionToolName, map[string]any{
			"name": "x", "share_to_users": share(map[string]any{"type": "Organization", "role": "view"}),
		}, "blocked", false},
		"a type this server does not know": {createCollectionToolName, map[string]any{
			"name": "x", "share_to_users": share(map[string]any{"type": "everyone", "userId": "u-3", "role": "view"}),
		}, "blocked", false},
		"a new admin on create": {createCollectionToolName, map[string]any{
			"name": "x", "share_to_users": share(map[string]any{"userId": "u-3", "role": "admin"}),
		}, "blocked", false},
		"a member raised to admin": {updateCollectionToolName, map[string]any{
			"collection_id": "c-1", "members": share(map[string]any{"userId": "u-guest", "role": "Admin"}),
		}, "blocked", false},
		"a group made admin": {updateCollectionToolName, map[string]any{
			"collection_id": "c-1", "members": share(map[string]any{"type": "group", "groupId": "g-1", "role": "admin"}),
		}, "blocked", false},
		"a guest of the organization": {createCollectionToolName, map[string]any{
			"name": "x", "share_to_users": share(map[string]any{"userId": "u-guest", "role": "view"}),
		}, "blocked", true},
		"an external member": {updateCollectionToolName, map[string]any{
			"collection_id": "c-1", "share_to_users": share(map[string]any{"userId": "u-ext", "role": "view"}),
		}, "blocked", true},
		"a userId nobody in the organization has": {createGroupToolName, map[string]any{
			"name": "x", "members": share(map[string]any{"userId": "u-nobody", "role": "member"}),
		}, "blocked", true},
		"no userId at all": {createGroupToolName, map[string]any{
			"name": "x", "members": share(map[string]any{"role": "member"}),
		}, "invalid", false},
		"a group with no groupId": {createCollectionToolName, map[string]any{
			"name": "x", "share_to_users": share(map[string]any{"type": "group", "role": "view"}),
		}, "invalid", false},
		"a group member invited by email": {updateGroupToolName, map[string]any{
			"group_id": "g-1", "members": share(map[string]any{"email": "someone@example.invalid", "role": "member"}),
		}, "blocked", false},
		"a new group administrator": {createGroupToolName, map[string]any{
			"name": "x", "members": share(map[string]any{"userId": "u-3", "role": "administrator"}),
		}, "blocked", false},
		"a group member raised to administrator": {updateGroupToolName, map[string]any{
			"group_id": "g-1", "members": []any{
				map[string]any{"userId": "u-admin", "role": "administrator"},
				map[string]any{"userId": "u-guest", "role": "administrator"},
			},
		}, "blocked", false},
		"guests as a widget's owners on create": {createWidgetToolName, map[string]any{
			"collection_id": "c-1", "name": "x", "owner_role": "guests",
		}, "blocked", false},
		"guests as a widget's owners on update": {updateWidgetToolName, map[string]any{
			"widget_common_id": "w-1", "owner_role": " Guests ",
		}, "blocked", false},
		"guests as a widget's editors on create": {createWidgetToolName, map[string]any{
			"collection_id": "c-1", "name": "x", "owner_role": "owners", "edit_role": "guests",
		}, "blocked", false},
		"guests as a widget's editors on update": {updateWidgetToolName, map[string]any{
			"widget_common_id": "w-1", "edit_role": " Guests ",
		}, "blocked", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, dry := range []bool{false, true} {
				f := newSharingFavro(t)
				args := map[string]any{"dry_run": dry}
				maps.Copy(args, tc.args)
				res := f.call(t, tc.tool, args)
				text := resultText(res)
				if !res.IsError || !strings.HasPrefix(text, "["+tc.class+"] ") {
					t.Errorf("%s dry_run=%v: got %q, want a [%s] refusal", tc.tool, dry, text, tc.class)
				}
				// The organization has a tool of its own, and the refusal
				// names it; nothing else here does.
				if names := strings.Contains(text, widenCollectionSharingToolName); names != (name == "the whole organization as an entry") {
					t.Errorf("%s dry_run=%v: names %s: %v in %q", tc.tool, dry, widenCollectionSharingToolName, names, text)
				}
				writes, reads, _ := f.made()
				if len(writes) != 0 {
					t.Errorf("%s dry_run=%v: a refused call wrote %v", tc.tool, dry, writes)
				}
				usersRead := false
				for _, r := range reads {
					usersRead = usersRead || r == "GET /users"
				}
				if usersRead != tc.readsUsers {
					t.Errorf("%s dry_run=%v: read the organization's members: %v, want %v (reads %v)",
						tc.tool, dry, usersRead, tc.readsUsers, reads)
				}
			}
		})
	}
}

// Every ordinary write that takes a sharing argument refuses its
// widest value before anything is written. The tools and arguments come
// from the registered input schemas, so a write added later that takes
// one of these is held by taking it. The destructive tools are left
// out: widening is what favro_widen_collection_sharing is for.
func TestEverySharingArgumentRefusesItsWidestValue(t *testing.T) {
	t.Parallel()
	widest := map[string]any{
		"share_to_users": []any{map[string]any{"email": "someone@example.invalid", "role": "view"}},
		"members":        []any{map[string]any{"email": "someone@example.invalid", "role": "member"}},
		"owner_role":     "guests",
		"edit_role":      "guests",
		"public_sharing": "public",
	}
	checked := 0
	for name, tool := range listToolsWith(t, Options{Destructive: true, UploadDir: t.TempDir()}) {
		if destructive(tool) {
			continue
		}
		for prop := range inputProperties(t, tool) {
			value, ok := widest[prop]
			if !ok {
				continue
			}
			checked++
			f := newSharingFavro(t)
			args := maps.Clone(smokeToolInputs[name])
			args[prop], args["dry_run"] = value, false
			res := f.call(t, name, args)
			if writes, _, _ := f.made(); !strings.HasPrefix(resultText(res), "[blocked] ") || len(writes) != 0 {
				t.Errorf("%s with the widest %s: got %q and writes %v, want [blocked] and none",
					name, prop, resultText(res), writes)
			}
		}
	}
	// Six tools take eleven of these between them.
	if checked < 11 {
		t.Errorf("checked %d sharing arguments, want at least 11", checked)
	}
}

// What someone already holds is not a new grant. favro_update_group's
// list replaces the whole one, so everyone who stays is re-sent with the
// role they hold, a guest and an administrator among them; refusing them
// would make the group's membership impossible to change.
func TestWhatIsAlreadyHeldIsNotANewGrant(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		tool string
		args map[string]any
		want string
	}{
		"a group's administrator and guest re-sent, a member added": {updateGroupToolName, map[string]any{
			"group_id": "g-1", "members": []any{
				map[string]any{"userId": "u-admin", "role": "administrator"},
				map[string]any{"userId": "u-guest", "role": "member"},
				map[string]any{"userId": "u-3", "role": "member"},
			},
		}, `"members":[{"userId":"u-admin","role":"administrator"},{"userId":"u-guest","role":"member"},{"userId":"u-3","role":"member"}]`},
		"a collection's admin re-sent, a guest re-roled down, a group re-roled": {updateCollectionToolName, map[string]any{
			"collection_id": "c-1", "members": []any{
				map[string]any{"userId": "u-1", "role": "admin"},
				map[string]any{"userId": "u-guest", "role": "guest"},
				map[string]any{"type": "group", "groupId": "g-1", "role": "view"},
			},
		}, `"members":[{"userId":"u-1","role":"admin"},{"userId":"u-guest","role":"guest"},{"type":"group","groupId":"g-1","role":"view"}]`},
		"a removal names anyone": {updateCollectionToolName, map[string]any{
			"collection_id": "c-1", "members": []any{map[string]any{"userId": "u-nobody", "delete": true}},
		}, `"members":[{"userId":"u-nobody","delete":true}]`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newSharingFavro(t)
			res := f.call(t, tc.tool, tc.args)
			if res.IsError {
				t.Fatalf("%s: refused: %s", tc.tool, resultText(res))
			}
			writes, _, body := f.made()
			if len(writes) != 1 || !strings.Contains(body, tc.want) {
				t.Errorf("%s: writes %v, body %s, want one write carrying %s", tc.tool, writes, body, tc.want)
			}
		})
	}
}

// The ordinary collection writes set only 'users'. Opening a collection
// to the organization or to anyone with its link is its own tool, behind
// the destructive flag.
func TestCollectionWritesRefuseWideSharing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tool, mode string
		refused    bool
	}{
		{createCollectionToolName, "public", true},
		{createCollectionToolName, "organization", true},
		{createCollectionToolName, "Public ", true},
		{createCollectionToolName, "anyone", true},
		{createCollectionToolName, "users", false},
		{updateCollectionToolName, "public", true},
		{updateCollectionToolName, "ORGANIZATION", true},
		{updateCollectionToolName, "users", false},
	} {
		f := newSharingFavro(t)
		args := map[string]any{"public_sharing": tc.mode}
		if tc.tool == createCollectionToolName {
			args["name"] = "x"
		} else {
			args["collection_id"] = "c-1"
		}
		res := f.call(t, tc.tool, args)
		text := resultText(res)
		writes, _, _ := f.made()
		// A mode that opens the collection is refused with the tool
		// that does it named; one this server does not know, without.
		wide := tc.refused && tc.mode != "anyone"
		if names := strings.Contains(text, widenCollectionSharingToolName); names != wide {
			t.Errorf("%s public_sharing %q: names %s: %v in %q", tc.tool, tc.mode, widenCollectionSharingToolName, names, text)
		}
		switch {
		case tc.refused && (!strings.HasPrefix(text, "[blocked] ") || len(writes) != 0):
			t.Errorf("%s public_sharing %q: got %q and writes %v, want [blocked] and none", tc.tool, tc.mode, text, writes)
		case !tc.refused && (res.IsError || len(writes) != 1):
			t.Errorf("%s public_sharing %q: got %q and writes %v, want one write", tc.tool, tc.mode, text, writes)
		}
	}
}

// favro_widen_collection_sharing reads the collection, holds the name
// the caller passed against Favro's, and writes only the mode; then it
// reads the collection back and says whether the mode took.
func TestWidenCollectionSharing(t *testing.T) {
	t.Parallel()
	args := func(over map[string]any) map[string]any {
		a := map[string]any{"collection_id": "c-1", "collection_name": "Eng", "public_sharing": "public", "confirm": true}
		maps.Copy(a, over)
		return a
	}

	t.Run("opens it, and reads it back", func(t *testing.T) {
		t.Parallel()
		f := newSharingFavro(t)
		res := f.call(t, widenCollectionSharingToolName, args(map[string]any{"public_sharing": "Organization"}))
		writes, _, body := f.made()
		if res.IsError || len(writes) != 1 || writes[0] != "PUT /collections/c-1" || body != `{"publicSharing":"organization"}` {
			t.Fatalf("got %q, writes %v, body %s; want one PUT of the mode alone", resultText(res), writes, body)
		}
		text := resultText(res)
		for _, note := range []string{
			`public_sharing was "users" before this call.`,
			`verified by reading the collection back: public_sharing is "organization".`,
		} {
			if !strings.Contains(text, note) {
				t.Errorf("the result does not say %q: %s", note, text)
			}
		}
	})

	t.Run("a write Favro did not store is reported", func(t *testing.T) {
		t.Parallel()
		f := newSharingFavro(t)
		f.ignore = true
		res := f.call(t, widenCollectionSharingToolName, args(nil))
		want := `public_sharing did not change: the collection was read back after the write and reports "users", not "public".`
		if res.IsError || !strings.Contains(resultText(res), want) {
			t.Errorf("got %q, want a result saying %q", resultText(res), want)
		}
	})

	for name, tc := range map[string]struct {
		over  map[string]any
		class string
		reads bool
	}{
		"a name that is not Favro's":   {map[string]any{"collection_name": "Engineering"}, "conflict", true},
		"a name that differs in case":  {map[string]any{"collection_name": "eng"}, "conflict", true},
		"a narrowing":                  {map[string]any{"public_sharing": "users"}, "invalid", false},
		"no confirm":                   {map[string]any{"confirm": false}, "blocked", false},
		"a name differing, in dry run": {map[string]any{"collection_name": "Other", "dry_run": true, "confirm": false}, "conflict", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newSharingFavro(t)
			res := f.call(t, widenCollectionSharingToolName, args(tc.over))
			writes, reads, _ := f.made()
			if text := resultText(res); !strings.HasPrefix(text, "["+tc.class+"] ") || len(writes) != 0 {
				t.Errorf("got %q and writes %v, want [%s] and none", text, writes, tc.class)
			}
			if (len(reads) > 0) != tc.reads {
				t.Errorf("reads %v, want reads: %v", reads, tc.reads)
			}
		})
	}

	t.Run("a dry run reads and writes nothing", func(t *testing.T) {
		t.Parallel()
		f := newSharingFavro(t)
		res := f.call(t, widenCollectionSharingToolName, args(map[string]any{"dry_run": true, "confirm": false, "collection_name": " Eng "}))
		writes, reads, _ := f.made()
		if res.IsError || len(writes) != 0 || len(reads) != 1 || !strings.HasPrefix(resultText(res), "DRY RUN") {
			t.Errorf("got %q, writes %v, reads %v; want a dry run after one read", resultText(res), writes, reads)
		}
	})
}

// favro_delete_widget never deletes everywhere by omission: a call names
// one collection, or says everywhere, and exactly one of them.
func TestDeleteWidgetNamesItsScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args    map[string]any
		refused bool
		query   string
	}{
		{map[string]any{}, true, ""},
		{map[string]any{"collection_id": "c-1", "everywhere": true}, true, ""},
		{map[string]any{"everywhere": false}, true, ""},
		{map[string]any{"collection_id": "c-1"}, false, "collectionId=c-1"},
		{map[string]any{"everywhere": true}, false, ""},
	} {
		var calls []string
		c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}))
		args := map[string]any{"widget_common_id": "w-1", "confirm": true}
		maps.Copy(args, tc.args)
		res, err := connectInMemoryWith(t, c).CallTool(t.Context(), &mcp.CallToolParams{Name: deleteWidgetToolName, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case tc.refused && (!strings.HasPrefix(resultText(res), "[invalid] ") || len(calls) != 0):
			t.Errorf("%v: got %q and calls %v, want [invalid] and none", tc.args, resultText(res), calls)
		case !tc.refused && (res.IsError || len(calls) != 1 || calls[0] != "DELETE /widgets/w-1?"+tc.query):
			t.Errorf("%v: got %q and calls %v, want DELETE /widgets/w-1?%s", tc.args, resultText(res), calls, tc.query)
		}
	}
}
