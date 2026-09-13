package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// "Every gate here has a test" is the first claim this program's own doc
// comment makes, and CLAUDE.md's eleventh hard rule says the same thing
// about every rule adopted from the standard. It was false when it was
// written: `schema-diff` — the command that decides what counts as a
// breaking change, and so the whole of the repository's semver promise —
// had none.
//
// This is that claim, held. It is the same shape as the two assertions
// the gates already run over their own exemption lists: derive the list
// from the code, then require each entry to be backed by something.
//
// A command counts as covered when the tests exercise it by name, or
// exercise a function it delegates to — `plugin-pack` is staging plus a
// zip write, and the two halves are tested separately, which is a real
// test of the command rather than a hole. The delegation is read from
// the source rather than assumed, so this cannot be satisfied by a test
// that merely mentions the right word in a comment.
func TestEveryCommandHasATest(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tests, testFiles := readPackage(t, dir, true)
	if testFiles < 5 {
		t.Fatalf("only %d test files read; this check is not reading the package", testFiles)
	}
	if len(commands) < 8 {
		t.Fatalf("the registry holds %d commands; it is not being read", len(commands))
	}

	calls := callGraph(t, dir)
	if len(calls) < 20 {
		t.Fatalf("the call graph holds %d functions; the package is not being parsed", len(calls))
	}

	for name, c := range commands {
		run := funcName(c.run)
		if exercised(tests, name) || exercised(tests, run) {
			continue
		}
		covered := false
		for _, callee := range calls[run] {
			if exercised(tests, callee) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("the command %q (%s) has no test, and nothing it calls has one either. "+
				"A gate nobody has watched fail is not yet a gate: %q and %q print the same sentence",
				name, run, "found nothing", "looked at nothing")
		}
	}
}

// exercised reports whether the test sources name this identifier in
// code rather than in prose — a word in a comment is not a test.
func exercised(tests, name string) bool {
	if name == "" {
		return false
	}
	for _, sep := range []string{name + "(", name + ")", `"` + name + `"`} {
		if strings.Contains(tests, sep) {
			return true
		}
	}
	return false
}

// readPackage concatenates the package's sources, with comments removed
// so a name mentioned in prose cannot stand in for a test.
func readPackage(t *testing.T, dir string, testsOnly bool) (string, int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	files := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") != testsOnly {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		files++
		b.WriteString(stripComments(t, name, data))
	}
	return b.String(), files
}

func stripComments(t *testing.T, name string, data []byte) string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, data, 0) // no ParseComments: they are dropped
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var b strings.Builder
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			b.WriteString(v.Name + "(")
		case *ast.BasicLit:
			b.WriteString(v.Value)
		}
		return true
	})
	return b.String()
}

// callGraph maps each function in the package's non-test sources to the
// functions it calls. One file at a time rather than ParseDir, which is
// deprecated because it ignores build tags.
func callGraph(t *testing.T, dir string) map[string][]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok {
						calls[fn.Name.Name] = append(calls[fn.Name.Name], id.Name)
					}
				}
				return true
			})
		}
	}
	return calls
}
