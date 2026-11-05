package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/v4/internal/favroapi"
	"github.com/mmedum/favro-mcp/v4/internal/render"
)

// listToolsWith builds a server with the given options and returns
// every tool it registered, by name.
func listToolsWith(t *testing.T, opts Options) map[string]*mcp.Tool {
	t.Helper()

	cs := connectInMemoryOpts(t, favroapi.NewClient(fixtureToken()), opts)
	res, err := cs.ListTools(t.Context(), nil)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	out := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

// takesConfirm reports whether a listed tool's input has confirm, which
// is what makes a tool destructive here (addTool).
func takesConfirm(t *testing.T, tool *mcp.Tool) bool {
	t.Helper()
	_, ok := inputProperties(t, tool)["confirm"]
	return ok
}

// TestDestructiveToolsAreOptIn is the whole of FAVRO_ENABLE_DESTRUCTIVE.
//
// The set it checks is derived from the confirm input rather than typed
// out here, so a destructive tool added later is covered by taking
// confirm rather than by somebody remembering this file exists.
func TestDestructiveToolsAreOptIn(t *testing.T) {
	t.Parallel()

	all := listToolsWith(t, Options{Destructive: true})
	def := listToolsWith(t, Options{})

	var gated []string
	for name, tool := range all {
		if takesConfirm(t, tool) {
			gated = append(gated, name)
		}
	}

	// The floor. "No destructive tools leaked" and "no tools were read"
	// print the same sentence otherwise, and this repository has
	// fifteen — a number that is allowed to change, but not to become
	// zero without somebody noticing.
	if len(gated) < 15 {
		t.Errorf("read %d destructive tools out of a surface of %d; the set derived from confirm cannot have shrunk that far: got %v, want at least %v", len(gated), len(all), len(gated), 15)
	}
	if len(all) <= len(gated) {
		t.Errorf("the surface is not all deletes: got %v, want greater than %v", len(all), len(gated))
	}

	for _, name := range gated {
		if _, present := def[name]; present {
			t.Errorf("%s takes confirm but is registered without FAVRO_ENABLE_DESTRUCTIVE: %q present", name, name)
		}
	}
	if len(def) != len(all)-len(gated) {
		t.Fatalf("the default surface must differ from the full one by exactly the destructive tools: got %d", len(def))
	}

	for name := range all {
		if _, kept := def[name]; !kept {
			if !slices.Contains(gated, name) {
				t.Errorf("%s disappeared from the default surface without taking confirm: %q missing", name, name)
			}
		}
	}
}

// TestEveryToolIsAnnotated holds that every tool says what it is to a
// client. A tool with no annotations reads as a destructive write that
// is not read-only, and a write whose DestructiveHint is nil reads as
// destructive by the schema's default: neither is a decision anybody
// made. The read tools leave DestructiveHint unset, which the schema
// ignores when readOnlyHint is true.
func TestEveryToolIsAnnotated(t *testing.T) {
	t.Parallel()

	all := listToolsWith(t, Options{Destructive: true, UploadDir: t.TempDir()})
	if len(all) < 83 {
		t.Errorf("read %d tools; the surface cannot have shrunk that far: got %v, want at least %v", len(all), len(all), 83)
	}

	for name, tool := range all {
		if tool.Annotations == nil {
			t.Fatalf("%s carries no annotations; use readOnly, changing or adding", name)
		}
		if tool.Annotations.ReadOnlyHint {
			continue
		}
		if tool.Annotations.DestructiveHint == nil {
			t.Fatalf("%s mutates and left DestructiveHint nil; use changing or adding, which always set it", name)
		}
	}
}

// TestContentAndStructuredContentDiffer holds that both halves of a
// result are present and are not the same bytes (D-61): a client may
// show the model one half or the other. A handler that returns a nil
// *mcp.CallToolResult, as every handler here does, is exactly the state
// where the SDK copies the marshaled output into a TextContent block.
func TestContentAndStructuredContentDiffer(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      pingToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	if res.StructuredContent == nil {
		t.Fatal("the machine half must be present")
	}
	if len(res.Content) != 1 {
		t.Fatalf("the readable half must be present: got %d", len(res.Content))
	}

	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Error("ok = false, want true")
	}
	if len(text.Text) == 0 {
		t.Fatal("text.Text is empty")
	}

	structured, err := json.Marshal(res.StructuredContent)
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if text.Text == string(structured) {
		t.Errorf("text.Text = %v, want anything else", text.Text)
	}
	if strings.HasPrefix(strings.TrimSpace(text.Text), "{") {
		t.Error("the readable half is JSON, which is the other half again")
	}

	// It still has to say something true.
	if !strings.Contains(text.Text, "favro-mcp") {
		t.Errorf("text.Text does not contain %q", "favro-mcp")
	}
}

// TestListToolContentSummarizesThePage covers the shape that matters
// most: this server never aggregates pages, so a caller that misses
// next_page reads a prefix of the answer and believes it is the whole
// one. The readable half has to say so.
func TestListToolContentSummarizesThePage(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entities":[
			{"organizationId":"o1","name":"First"},
			{"organizationId":"o2","name":"Second"}],
			"page":0,"pages":3,"limit":2,"requestId":"r1"}`))
	}))

	cs := connectInMemoryWith(t, c)
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      listOrgsToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.IsError {
		t.Error("res.IsError = true, want false")
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "2 items") {
		t.Errorf("text does not contain %q", "2 items")
	}
	if !strings.Contains(text, "next_page") {
		t.Errorf("text does not contain %q", "next_page")
	}
	if !strings.Contains(text, "First") {
		t.Errorf("the entries are named, or the readable half is a count: %q missing", "First")
	}
}

// TestToolErrorsCarryAClass is the closed vocabulary reaching the
// wire. The class has to be one render declares, not a plausible word.
func TestToolErrorsCarryAClass(t *testing.T) {
	t.Parallel()

	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	cs := connectInMemoryWith(t, c)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      getOrgToolName,
		Arguments: map[string]any{},
	})
	if err := err; err != nil {
		t.Fatalf("an error from Favro is a tool result, not a protocol error: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}

	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.HasPrefix(text, "[forbidden] ") {
		t.Errorf("a 403 must render as [forbidden]; got %q", text)
	}

	// And the prefix is always a declared class, whatever the error.
	res, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      searchCardsToolName,
		Arguments: map[string]any{"query": "anything"},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if !res.IsError {
		t.Error("res.IsError = false, want true")
	}
	requireDeclaredClassPrefix(t, res.Content[0].(*mcp.TextContent).Text)
}

// requireDeclaredClassPrefix asserts text starts with "[class] " for a
// class render.Classes names.
func requireDeclaredClassPrefix(t *testing.T, text string) {
	t.Helper()

	// Fatal, not Error: everything below indexes into text on the
	// strength of these, so continuing past a failure turns a clear
	// message into a slice-bounds panic.
	if !strings.HasPrefix(text, "[") {
		t.Fatalf("no class prefix on %q", text)
	}
	end := strings.Index(text, "] ")
	if end <= 0 {
		t.Fatalf("no closing bracket on %q", text)
	}

	got := render.Class(text[1:end])
	if !slices.Contains(render.Classes, got) {
		t.Errorf("%q is not in the closed vocabulary; add it to render.Classes and to the error-class table in docs/TOOLS.md#error-classes, or use one that is already there", got)
	}
}

// TestEverySentinelIsClassified reads the source of the packages that
// declare error sentinels and requires each one to name its class.
//
// Reading the source is the point. render.Classify falls back to
// ClassInvalid, which is right for every unsentineled error here today
// and wrong the moment somebody adds a sentinel that means "not found"
// — and a fallback is exactly the kind of thing that silently absorbs a
// mistake. Go cannot enumerate a package's variables at run time, so
// this parses them.
func TestEverySentinelIsClassified(t *testing.T) {
	t.Parallel()

	var files []string
	for _, dir := range []string{".", "../service", "../favroapi"} {
		matched, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err := err; err != nil {
			t.Fatalf("err: %v", err)
		}
		files = append(files, matched...)
	}

	fset := token.NewFileSet()
	var checked, filesRead int

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		filesRead++
		src, err := os.ReadFile(path)
		if err := err; err != nil {
			t.Fatalf("err: %v", err)
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err := err; err != nil {
			t.Fatalf("err: %v", err)
		}

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "err") || i >= len(vs.Values) {
						continue
					}
					checked++
					call, ok := vs.Values[i].(*ast.CallExpr)
					if !ok {
						t.Errorf("%s: sentinel %s is not built by render.Sentinel", path, name.Name)
					}
					fn, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || fn.Sel.Name != "Sentinel" {
						t.Errorf("%s: sentinel %s must be render.Sentinel(render.Class…, …) so the boundary can render [class] without matching on its text", path, name.Name)
					}
					if len(call.Args) != 2 {
						t.Fatalf("len(call.Args) = %d, want 2", len(call.Args))
					}
					sel, ok := call.Args[0].(*ast.SelectorExpr)
					if !ok {
						t.Errorf("%s: %s does not name a render class", path, name.Name)
					}
					if !strings.HasPrefix(sel.Sel.Name, "Class") {
						t.Errorf("%s: %s names %s, which is not a class constant", path, name.Name, sel.Sel.Name)
					}
				}
			}
		}
	}

	// The floor, twice over: a glob that matched nothing and a package
	// with no sentinels left both look like success. ../favroapi joined
	// the glob when a move guard landed there and neither this test nor
	// TestEveryErrorTypeNamesItsClass — which reads declared types, not
	// vars — would have held it.
	if filesRead < 65 {
		t.Errorf("read only %d source files: got %v, want at least %v", filesRead, filesRead, 65)
	}
	if checked < 19 {
		t.Errorf("found only %d sentinels: got %v, want at least %v", checked, checked, 19)
	}
}

// TestClassedErrorsSurviveWrapping is the property the sentinels are
// used through: every one of them is raised inside a
// fmt.Errorf("%w: …") that adds the offending value, so a classifier
// reading only the outermost error would see none of them.
func TestClassedErrorsSurviveWrapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want render.Class
	}{
		{fmt.Errorf("%w: %q", errTagToCardUnknown, "a-name"), render.ClassNotFound},
		{fmt.Errorf("%w (%d matches for %q)", errTagToCardAmbiguous, 3, "a-name"), render.ClassAmbiguous},
		{fmt.Errorf("%w: field %q is %q", errUnsupportedCustomFieldType, "f", "Timeline"), render.ClassUnsupported},
		{fmt.Errorf("%w: %q", errAttachmentPathNotAFile, "/dev/null"), render.ClassInvalid},
		{fmt.Errorf("%w (card %q)", errDescriptionFindNoMatch, "c1"), render.ClassNotFound},
		{fmt.Errorf("%w: %q", errCustomFieldNotFound, "cf1"), render.ClassNotFound},
	}

	for _, tc := range cases {
		if got := render.Classify(tc.err); got != tc.want {
			t.Errorf("wrapping lost the class of %v: got %v, want %v", tc.err, got, tc.want)
		}
	}
}

// TestColumnMoveContractIsStatedOnce holds every tool schema to the one
// wire contract for a column move, rather than to whichever half of it
// the author of a given struct tag remembered.
//
// The contract: Favro requires widgetCommonId whenever columnId or
// laneId is set, and answers 200 with a stub card without it
// (favroapi.UpdateCard). list_position has nothing to do with it.
//
// A contract that only prose states decays, so this one is a test. Its
// list of tools comes from the registered surface, so a card tool added
// later is covered by having been registered.
func TestColumnMoveContractIsStatedOnce(t *testing.T) {
	t.Parallel()

	all := listToolsWith(t, Options{Destructive: true})
	var checkedPosition, checkedWidget int

	for name, tool := range all {
		props := inputProperties(t, tool)
		if props == nil {
			continue
		}
		// Nothing may tell the model that a column move needs a
		// position. It does not, and saying so sends it looking at the
		// wrong argument when the move silently fails.
		if pos, ok := props["list_position"]; ok {
			checkedPosition++
			if desc := strings.ToLower(pos.Description); strings.Contains(desc, "required for column") {
				t.Errorf("%s: list_position claims to be required for a column move; widget_common_id is the field Favro requires", name)
			}
		}
		// A tool taking both a card_id and a column_id is relocating an
		// existing card, which is the only shape the contract binds —
		// a column_id alone is a filter (favro_list_cards), the column
		// a new card is created in, or the column itself
		// (favro_update_column). Such a tool must offer the widget and
		// must say it is required, because the failure without it is a
		// success.
		if _, ok := props["card_id"]; !ok {
			continue
		}
		if _, ok := props["column_id"]; !ok {
			continue
		}
		checkedWidget++
		widget, ok := props["widget_common_id"]
		if !ok {
			t.Errorf("%s: moves a card between columns and takes no widget_common_id, so every move it makes is a silent no-op", name)
			continue
		}
		if desc := strings.ToLower(widget.Description); !strings.Contains(desc, "required") {
			t.Errorf("%s: takes a card_id and a column_id and its widget_common_id description does not say it is required for that; Favro answers 200 and moves nothing without it", name)
		}
	}

	// The floor: a schema walk that found no such tool would pass every
	// assertion above without reading anything.
	if checkedPosition < 3 {
		t.Errorf("read only %d list_position inputs: got %v, want at least %v", checkedPosition, checkedPosition, 3)
	}
	if checkedWidget < 2 {
		t.Errorf("read only %d tools taking both column_id and widget_common_id: got %v, want at least %v", checkedWidget, checkedWidget, 2)
	}
}

// schemaProperty is the one part of an input schema this file reads:
// what the model is told an argument means.
type schemaProperty struct {
	Description string `json:"description"`
}

// inputProperties unwraps a registered tool's input schema into its
// top-level properties. The schema arrives from ListTools as the
// marshaled form rather than a typed value, so it round-trips through
// JSON. Returns nil for a tool with no object schema.
func inputProperties(t *testing.T, tool *mcp.Tool) map[string]schemaProperty {
	t.Helper()

	if tool.InputSchema == nil {
		return nil
	}
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var schema struct {
		Properties map[string]schemaProperty `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("err: %v", err)
	}
	return schema.Properties
}

// TestEveryDestructiveCallNeedsConfirm: a destructive call without
// confirm: true is refused [blocked] before Favro hears of it, and the
// same call as a dry run is not. The tools come from the confirm input,
// so a delete added later is covered by taking it.
func TestEveryDestructiveCallNeedsConfirm(t *testing.T) {
	t.Parallel()

	var requests, writes atomic.Int32
	count := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		smokeFixtureHandler(t).ServeHTTP(w, r)
	})
	cs := connectInMemoryWith(t, favroFixture(t, count))
	flagged := favroFixture(t, count)
	flagged.ForceDryRun = true
	forced := connectInMemoryWith(t, flagged)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tool := range res.Tools {
		if !takesConfirm(t, tool) {
			continue
		}
		checked++
		args := maps.Clone(smokeToolInputs[tool.Name])
		args["dry_run"] = false
		before := requests.Load()
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		want := "[blocked] " + tool.Name + " is refused without confirm: true"
		if text := res.Content[0].(*mcp.TextContent).Text; !res.IsError || !strings.HasPrefix(text, want) {
			t.Errorf("%s without confirm: got %q, want %q", tool.Name, text, want)
		}
		if n := requests.Load() - before; n != 0 {
			t.Errorf("%s without confirm reached Favro %d times before it was refused", tool.Name, n)
		}

		args["dry_run"] = true
		res, err = cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Errorf("%s as a dry run without confirm was refused: %s", tool.Name, serializedResponseString(t, res))
		}

		// --dry-run makes every call a dry run, whatever the arguments.
		args["dry_run"] = false
		before = writes.Load()
		res, err = forced.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Errorf("%s under --dry-run without confirm was refused: %s", tool.Name, serializedResponseString(t, res))
		}
		if n := writes.Load() - before; n != 0 {
			t.Errorf("%s under --dry-run wrote to Favro %d times", tool.Name, n)
		}
	}
	if checked < 15 {
		t.Errorf("checked %d destructive tools, want at least 15", checked)
	}
}

// A dry run needs no confirm, so it must not rest on each handler
// putting the dry run on the context its writes use: addTool does that
// for every input that asks for one.
func TestADryRunWritesNothingWhateverTheHandlerDoes(t *testing.T) {
	t.Parallel()

	var writes atomic.Int32
	c := favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	type input struct {
		dryRunInput
		confirmInput
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	addTool(&registry{srv: srv, destructive: true, client: c},
		&mcp.Tool{Name: "favro_delete_forgetfully", Annotations: changing("x")},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ input) (*mcp.CallToolResult, struct{}, error) {
			_ = c.DeleteTag(ctx, "t-1") // the handler's own dry-run handling, forgotten
			return nil, struct{}{}, nil
		})
	res, err := connectServer(t, srv).CallTool(t.Context(), &mcp.CallToolParams{
		Name: "favro_delete_forgetfully", Arguments: map[string]any{"dry_run": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || writes.Load() != 0 {
		t.Errorf("a dry run without confirm: error %v, writes %d; want a result and no write", res.IsError, writes.Load())
	}
}

// TestEveryWriteTakesDryRunAndSendsNothing holds dry_run over the
// registered surface (D-28): every tool not marked read-only takes
// dry_run, and its smoke row as a dry run sends no write to Favro. A dry
// run may still read to build its preview, as dry_run's own description
// says, so a GET is not counted.
func TestEveryWriteTakesDryRunAndSendsNothing(t *testing.T) {
	t.Parallel()

	var writes atomic.Int32
	smoke := smokeFixtureHandler(t)
	cs := connectInMemoryWith(t, favroFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		smoke.ServeHTTP(w, r)
	})))
	file := writeSmokeAttachment(t)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			continue
		}
		if _, ok := inputProperties(t, tool)["dry_run"]; !ok {
			t.Errorf("%s is not marked read-only and takes no dry_run; embed dryRunInput in its input", tool.Name)
			continue
		}
		args := substituteSmokePlaceholders(smokeToolInputs[tool.Name], file)
		args["dry_run"] = true
		before := writes.Load()
		out, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if out.IsError {
			t.Errorf("%s: its dry run failed, so nothing was checked: %s", tool.Name, serializedResponseString(t, out))
			continue
		}
		if n := writes.Load() - before; n != 0 {
			t.Errorf("%s as a dry run sent writes to Favro: got %d, want 0", tool.Name, n)
		}
	}
}

// TestOnlyTheWidestWritesPrompt holds the marked set (D-34). A tag,
// group, widget or collection deleted takes with it something that
// every card, share or board depends on, and a collection opened to the
// organization or to anyone stays seen; Claude Code prompts on every
// such call. Nothing else is marked: a delete made many times in a row
// would bring back a prompt nobody reads.
func TestOnlyTheWidestWritesPrompt(t *testing.T) {
	t.Parallel()

	var got []string
	for name, tool := range listToolsWith(t, Options{Destructive: true, UploadDir: t.TempDir()}) {
		if v, marked := tool.Meta[promptMeta]; marked {
			if v != true {
				t.Errorf("%s carries %s = %v, want true", name, promptMeta, v)
			}
			got = append(got, name)
		}
	}
	slices.Sort(got)
	want := []string{
		"favro_delete_collection", "favro_delete_group", "favro_delete_tag",
		"favro_delete_widget", "favro_widen_collection_sharing",
	}
	if !slices.Equal(got, want) {
		t.Errorf("tools carrying %s = %v, want %v", promptMeta, got, want)
	}
}

// A tool that takes confirm without saying destructiveHint: true, or an
// ordinary one that carries the mark, fails when the server starts
// rather than when a person first calls it.
func TestRegistrationRefusesAMissingRule(t *testing.T) {
	t.Parallel()

	type noConfirm struct{ dryRunInput }
	type withConfirm struct {
		dryRunInput
		confirmInput
	}
	type noDryRun struct{}
	hn := func(context.Context, *mcp.CallToolRequest, noConfirm) (*mcp.CallToolResult, struct{}, error) {
		return nil, struct{}{}, nil
	}
	hc := func(context.Context, *mcp.CallToolRequest, withConfirm) (*mcp.CallToolResult, struct{}, error) {
		return nil, struct{}{}, nil
	}
	hd := func(context.Context, *mcp.CallToolRequest, noDryRun) (*mcp.CallToolResult, struct{}, error) {
		return nil, struct{}{}, nil
	}
	for name, c := range map[string]struct {
		add  func(*registry)
		want string
	}{
		"a confirm on a write that adds": {func(reg *registry) {
			addTool(reg, &mcp.Tool{Name: "x", Annotations: adding("x")}, hc)
		}, "takes confirm and is not annotated destructive"},
		"a confirm on a read": {func(reg *registry) {
			addTool(reg, &mcp.Tool{Name: "x", Annotations: readOnly("x")}, hc)
		}, "takes confirm and is not annotated destructive"},
		"a marked ordinary write": {func(reg *registry) {
			addTool(reg, &mcp.Tool{Name: "x", Annotations: changing("x"), Meta: promptEveryCall()}, hn)
		}, "is not destructive"},
		"a marked read": {func(reg *registry) {
			addTool(reg, &mcp.Tool{Name: "x", Annotations: readOnly("x"), Meta: promptEveryCall()}, hn)
		}, "is not destructive"},
		"a write that changes something without dry_run": {func(reg *registry) {
			addTool(reg, &mcp.Tool{Name: "x", Annotations: changing("x")}, hd)
		}, `tool "x" is not marked read-only and takes no dry_run`},
		"a write that adds without dry_run": {func(reg *registry) {
			addTool(reg, &mcp.Tool{Name: "x", Annotations: adding("x")}, hd)
		}, `tool "x" is not marked read-only and takes no dry_run`},
		"a tool with no annotations and no dry_run": {func(reg *registry) {
			addTool(reg, &mcp.Tool{Name: "x"}, hd)
		}, `tool "x" is not marked read-only and takes no dry_run`},
	} {
		func() {
			defer func() {
				got := fmt.Sprint(recover())
				if !strings.Contains(got, c.want) {
					t.Errorf("%s: panicked with %q, want it to say %q", name, got, c.want)
				}
			}()
			c.add(&registry{srv: mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil), destructive: true})
		}()
	}
	// And the rules' own shapes register: a marked delete, an ordinary
	// write that changes something without taking confirm, and a read
	// that takes no dry_run.
	reg := &registry{srv: mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil), destructive: true}
	addTool(reg, &mcp.Tool{Name: "x", Annotations: changing("x"), Meta: promptEveryCall()}, hc)
	addTool(reg, &mcp.Tool{Name: "y", Annotations: changing("y")}, hn)
	addTool(reg, &mcp.Tool{Name: "z", Annotations: readOnly("z")}, hd)
}

// TestNoWriteWithoutConfirmSendsADelete backs up TestEveryWriteIsClassified
// with what a write does rather than where it is listed. Every write that
// does not take confirm runs its smoke row as a dry run, and none may
// preview an HTTP DELETE. A removal sent as a PUT, as
// favro_remove_attachment's is, passes this; its list is what holds it.
func TestNoWriteWithoutConfirmSendsADelete(t *testing.T) {
	t.Parallel()

	cs := connectInMemoryWith(t, favroFixture(t, smokeFixtureHandler(t)))
	file := writeSmokeAttachment(t)
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tool := range res.Tools {
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint || takesConfirm(t, tool) {
			continue
		}
		out, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.Name, Arguments: substituteSmokePlaceholders(smokeToolInputs[tool.Name], file)})
		if err != nil {
			t.Fatal(err)
		}
		if out.IsError {
			t.Errorf("%s: its smoke row's dry run failed, so nothing was checked: %s", tool.Name, serializedResponseString(t, out))
			continue
		}
		raw, err := json.Marshal(out.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var preview struct {
			WouldCall *DryRunCall `json:"would_call"`
		}
		if err := json.Unmarshal(raw, &preview); err != nil {
			t.Fatal(err)
		}
		if preview.WouldCall == nil {
			t.Errorf("%s: its dry run names no would_call, so nothing was checked", tool.Name)
			continue
		}
		checked++
		if preview.WouldCall.Method == http.MethodDelete {
			t.Errorf("%s takes no confirm and previews %s %s; a delete embeds confirmInput",
				tool.Name, preview.WouldCall.Method, preview.WouldCall.URL)
		}
	}
	if checked < 33 {
		t.Errorf("checked %d writes' previews; the surface cannot have shrunk that far: got %v, want at least %v", checked, checked, 33)
	}
}

// The writes, by what each does. Which list a write is in is a decision
// made per tool, so the lists are typed out: a new write fails
// TestEveryWriteIsClassified until somebody puts it in one, and moving a
// tool between them is a change a reviewer sees.
var (
	// writesThatAdd create something new and change nothing already
	// there: destructiveHint false, registered by default.
	writesThatAdd = []string{
		"favro_add_comment_to_card", "favro_create_card", "favro_create_collection",
		"favro_create_column", "favro_create_comment", "favro_create_group",
		"favro_create_tag", "favro_create_task", "favro_create_tasklist",
		"favro_create_widget", "favro_upload_attachment", "favro_upload_comment_attachment",
	}
	// writesThatChange change or remove something already there and can
	// be set back by another write: destructiveHint true, registered by
	// default, no confirm.
	writesThatChange = []string{
		"favro_add_dependencies", "favro_add_tag_to_card", "favro_append_card_description",
		"favro_archive_card", "favro_move_card", "favro_prepend_card_description",
		"favro_remove_tag_from_card", "favro_replace_in_card_description", "favro_set_card_custom_field",
		"favro_unarchive_card", "favro_update_card", "favro_update_collection",
		"favro_update_column", "favro_update_comment", "favro_update_dependency",
		"favro_update_group", "favro_update_tag", "favro_update_tags",
		"favro_update_task", "favro_update_tasklist", "favro_update_widget",
	}
	// writesThatDestroy cannot be undone, or open a collection wide:
	// destructiveHint true, and they take confirm, which puts them behind
	// FAVRO_ENABLE_DESTRUCTIVE.
	writesThatDestroy = []string{
		"favro_delete_all_dependencies", "favro_delete_card", "favro_delete_collection",
		"favro_delete_column", "favro_delete_comment", "favro_delete_dependency",
		"favro_delete_group", "favro_delete_tag", "favro_delete_task",
		"favro_delete_tasklist", "favro_delete_webhook", "favro_delete_widget",
		"favro_remove_attachment", "favro_replace_dependencies", "favro_widen_collection_sharing",
	}
)

// TestOnlyToolsThatAddSayTheyAreNotDestructive holds that
// destructiveHint is false only on a write that adds (D-33). The MCP
// schema reads false as "the tool performs only additive updates", so a
// write says false only when it creates something new and changes
// nothing already there. A card's tags and dependencies are changes to
// the card, and an append rewrites the whole description.
func TestOnlyToolsThatAddSayTheyAreNotDestructive(t *testing.T) {
	t.Parallel()

	var got []string
	for name, tool := range listToolsWith(t, Options{Destructive: true, UploadDir: t.TempDir()}) {
		if tool.Annotations == nil {
			t.Errorf("%s carries no annotations", name)
			continue
		}
		if h := tool.Annotations.DestructiveHint; !tool.Annotations.ReadOnlyHint && h != nil && !*h {
			got = append(got, name)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, writesThatAdd) {
		t.Errorf("writes that say destructiveHint false = %v, want %v", got, writesThatAdd)
	}
}

// TestEveryWriteIsClassified closes the gate's blind side. The gate reads
// confirm, and a delete and an update both use changing(), so a delete
// written without confirmInput would register by default and run
// unasked. Every write has to be in one of the three lists, and its hint
// and its confirm have to be the ones the list says.
func TestEveryWriteIsClassified(t *testing.T) {
	t.Parallel()

	type class struct {
		hint, confirm bool
	}
	want := map[string]class{}
	for _, l := range []struct {
		names []string
		c     class
	}{
		{writesThatAdd, class{hint: false, confirm: false}},
		{writesThatChange, class{hint: true, confirm: false}},
		{writesThatDestroy, class{hint: true, confirm: true}},
	} {
		for _, name := range l.names {
			if _, twice := want[name]; twice {
				t.Errorf("%s is in two lists", name)
			}
			want[name] = l.c
		}
	}

	writes := 0
	for name, tool := range listToolsWith(t, Options{Destructive: true, UploadDir: t.TempDir()}) {
		if tool.Annotations == nil {
			t.Errorf("%s carries no annotations", name)
			continue
		}
		if tool.Annotations.ReadOnlyHint {
			continue
		}
		writes++
		w, listed := want[name]
		if !listed {
			t.Errorf("%s is a write in none of writesThatAdd, writesThatChange or writesThatDestroy; put it in the one that says what it does", name)
			continue
		}
		delete(want, name)
		h := tool.Annotations.DestructiveHint
		if got := (class{hint: h == nil || *h, confirm: takesConfirm(t, tool)}); got != w {
			t.Errorf("%s: destructiveHint %v and confirm %v, want %v and %v", name, got.hint, got.confirm, w.hint, w.confirm)
		}
	}
	for name := range want {
		t.Errorf("%s is listed but not registered", name)
	}
	if writes < 48 {
		t.Errorf("read %d writes; the surface cannot have shrunk that far: got %v, want at least %v", writes, writes, 48)
	}
}
