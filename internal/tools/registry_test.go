package tools

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/render"
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

// TestDestructiveToolsAreOptIn is the whole of FAVRO_ENABLE_DESTRUCTIVE.
//
// The set it checks is derived from the annotations rather than typed
// out here, so a destructive tool added later is covered by having
// been annotated — which its author has to do anyway — rather than by
// somebody remembering this file exists.
func TestDestructiveToolsAreOptIn(t *testing.T) {
	t.Parallel()

	all := listToolsWith(t, Options{Destructive: true})
	def := listToolsWith(t, Options{})

	var destructive []string
	for name, tool := range all {
		if tool.Annotations != nil && tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			destructive = append(destructive, name)
		}
	}

	// The floor. "No destructive tools leaked" and "no tools were read"
	// print the same sentence otherwise, and this repository has
	// thirteen — a number that is allowed to change, but not to become
	// zero without somebody noticing.
	if len(destructive) < 13 {
		t.Errorf("read %d destructive tools out of a surface of %d; the annotation-derived set cannot have shrunk that far: got %v, want at least %v", len(destructive), len(all), len(destructive), 13)
	}
	if len(all) <= len(destructive) {
		t.Errorf("the surface is not all deletes: got %v, want greater than %v", len(all), len(destructive))
	}

	for _, name := range destructive {
		if _, present := def[name]; present {
			t.Errorf("%s is annotated destructive but is registered without FAVRO_ENABLE_DESTRUCTIVE: %q present", name, name)
		}
	}
	if len(def) != len(all)-len(destructive) {
		t.Fatalf("the default surface must differ from the full one by exactly the destructive tools: got %d", len(def))
	}

	for name := range all {
		if _, gated := def[name]; !gated {
			if !slices.Contains(destructive, name) {
				t.Errorf("%s disappeared from the default surface without being annotated destructive: %q missing", name, name)
			}
		}
	}
}

// TestEveryToolIsAnnotated closes the way the destructive gate can
// fail open.
//
// addTool decides from `DestructiveHint`, and treats nil as "not
// destructive" — which is right for the read tools, whose readOnly()
// annotations leave it unset, and wrong for anything that mutates. A
// tool registered with no annotations at all, or a mutating tool whose
// DestructiveHint was left nil, is registered unconditionally and
// every other assertion in this file still passes: both surfaces agree
// about it, because both read the same missing annotation.
//
// So the gate's real precondition is that the annotation is always
// there to read, and this is what holds it. Note the SDK's own default
// runs the other way — `DestructiveHint` nil means destructive to a
// client — which is a second reason nil must never reach the wire on a
// tool that writes.
func TestEveryToolIsAnnotated(t *testing.T) {
	t.Parallel()

	all := listToolsWith(t, Options{Destructive: true})
	if len(all) < 83 {
		t.Errorf("read %d tools; the surface cannot have shrunk that far: got %v, want at least %v", len(all), len(all), 83)
	}

	for name, tool := range all {
		if tool.Annotations == nil {
			t.Fatalf("%s carries no annotations, so addTool's destructive gate has nothing to read and registers it unconditionally", name)
		}
		if tool.Annotations.ReadOnlyHint {
			continue
		}
		if tool.Annotations.DestructiveHint == nil {
			t.Fatalf("%s mutates and left DestructiveHint nil; use mutating(title, …), which always sets it explicitly", name)
		}
	}
}

// TestContentAndStructuredContentDiffer is standard §2: a client may
// show the model one half or the other, so both must be present and
// they must not be the same bytes. Every tool in this repository
// returned a nil *mcp.CallToolResult, which is exactly the state where
// the SDK copies the marshalled output into a TextContent block.
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

// TestListToolContentSummarisesThePage covers the shape that matters
// most for §4.4: this server never aggregates pages, so a caller that
// misses next_page reads a prefix of the answer and believes it is the
// whole one. The readable half has to say so.
func TestListToolContentSummarisesThePage(t *testing.T) {
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
		t.Errorf("%q is not in the closed vocabulary; add it to render.Classes and docs/architecture.md §6.2, or use one that is already there", got)
	}
}

// TestEverySentinelIsClassified reads the source of the packages that
// declare error sentinels and requires each one to name its class.
//
// Reading the source is the point. render.Classify falls back to
// ClassInvalid, which is right for every unsentinelled error here today
// and wrong the moment somebody adds a sentinel that means "not found"
// — and a fallback is exactly the kind of thing that silently absorbs a
// mistake. Go cannot enumerate a package's variables at run time, so
// this parses them.
func TestEverySentinelIsClassified(t *testing.T) {
	t.Parallel()

	var files []string
	for _, dir := range []string{".", "../service"} {
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
	// with no sentinels left both look like success.
	if filesRead < 40 {
		t.Errorf("read only %d source files: got %v, want at least %v", filesRead, filesRead, 40)
	}
	if checked < 14 {
		t.Errorf("found only %d sentinels: got %v, want at least %v", checked, checked, 14)
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
