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
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/render"
)

// listToolsWith builds a server with the given options and returns
// every tool it registered, by name.
func listToolsWith(t *testing.T, opts Options) map[string]*mcp.Tool {
	t.Helper()

	cs := connectInMemoryOpts(t, favroapi.NewClient(fixtureToken()), opts)
	res, err := cs.ListTools(t.Context(), nil)
	require.NoError(t, err)

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
	require.GreaterOrEqual(t, len(destructive), 13,
		"read %d destructive tools out of a surface of %d; the annotation-derived set cannot have shrunk that far",
		len(destructive), len(all))
	require.Greater(t, len(all), len(destructive), "the surface is not all deletes")

	for _, name := range destructive {
		require.NotContains(t, def, name,
			"%s is annotated destructive but is registered without FAVRO_ENABLE_DESTRUCTIVE", name)
	}
	require.Len(t, def, len(all)-len(destructive),
		"the default surface must differ from the full one by exactly the destructive tools")

	for name := range all {
		if _, gated := def[name]; !gated {
			require.Contains(t, destructive, name,
				"%s disappeared from the default surface without being annotated destructive", name)
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
	require.GreaterOrEqual(t, len(all), 83,
		"read %d tools; the surface cannot have shrunk that far", len(all))

	for name, tool := range all {
		require.NotNil(t, tool.Annotations,
			"%s carries no annotations, so addTool's destructive gate has nothing to read and registers it unconditionally", name)
		if tool.Annotations.ReadOnlyHint {
			continue
		}
		require.NotNil(t, tool.Annotations.DestructiveHint,
			"%s mutates and left DestructiveHint nil; use mutating(title, …), which always sets it explicitly", name)
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
	require.NoError(t, err)
	require.False(t, res.IsError)

	require.NotEmpty(t, res.StructuredContent, "the machine half must be present")
	require.Len(t, res.Content, 1, "the readable half must be present")

	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.NotEmpty(t, text.Text)

	structured, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.NotEqual(t, string(structured), text.Text,
		"the two halves are the same bytes; the readable half is carrying nothing the machine half did not")
	require.False(t, strings.HasPrefix(strings.TrimSpace(text.Text), "{"),
		"the readable half is JSON, which is the other half again")

	// It still has to say something true.
	require.Contains(t, text.Text, "favro-mcp")
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
	require.NoError(t, err)
	require.False(t, res.IsError)

	text := res.Content[0].(*mcp.TextContent).Text
	require.Contains(t, text, "2 items")
	require.Contains(t, text, "next_page")
	require.Contains(t, text, "First", "the entries are named, or the readable half is a count")
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
		Arguments: map[string]any{"organization_id": "no-such-organization"},
	})
	require.NoError(t, err, "an error from Favro is a tool result, not a protocol error")
	require.True(t, res.IsError)

	text := res.Content[0].(*mcp.TextContent).Text
	require.True(t, strings.HasPrefix(text, "[forbidden] "),
		"a 403 must render as [forbidden]; got %q", text)

	// And the prefix is always a declared class, whatever the error.
	res, err = cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      searchCardsToolName,
		Arguments: map[string]any{"query": "anything"},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	requireDeclaredClassPrefix(t, res.Content[0].(*mcp.TextContent).Text)
}

// requireDeclaredClassPrefix asserts text starts with "[class] " for a
// class render.Classes names.
func requireDeclaredClassPrefix(t *testing.T, text string) {
	t.Helper()

	require.True(t, strings.HasPrefix(text, "["), "no class prefix on %q", text)
	end := strings.Index(text, "] ")
	require.Positive(t, end, "no class prefix on %q", text)

	got := render.Class(text[1:end])
	require.Contains(t, render.Classes, got,
		"%q is not in the closed vocabulary; add it to render.Classes and docs/architecture.md §6.2 or use one that is there", got)
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
		require.NoError(t, err)
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
		require.NoError(t, err)
		file, err := parser.ParseFile(fset, path, src, 0)
		require.NoError(t, err)

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
					require.True(t, ok,
						"%s: sentinel %s is not built by render.Sentinel", path, name.Name)
					fn, ok := call.Fun.(*ast.SelectorExpr)
					require.True(t, ok && fn.Sel.Name == "Sentinel",
						"%s: sentinel %s must be render.Sentinel(render.Class…, …) so the boundary can render [class] without matching on its text",
						path, name.Name)
					require.Len(t, call.Args, 2)
					sel, ok := call.Args[0].(*ast.SelectorExpr)
					require.True(t, ok, "%s: %s does not name a render class", path, name.Name)
					require.True(t, strings.HasPrefix(sel.Sel.Name, "Class"),
						"%s: %s names %s, which is not a class constant", path, name.Name, sel.Sel.Name)
				}
			}
		}
	}

	// The floor, twice over: a glob that matched nothing and a package
	// with no sentinels left both look like success.
	require.GreaterOrEqual(t, filesRead, 40, "read only %d source files", filesRead)
	require.GreaterOrEqual(t, checked, 14, "found only %d sentinels", checked)
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
		require.Equal(t, tc.want, render.Classify(tc.err), "wrapping lost the class of %v", tc.err)
	}
}
