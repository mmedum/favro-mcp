package tools

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// settingWriters are the calls that change what the server is, rather
// than what is in Favro: the environment its settings come from, a file
// on this computer, and the keyring its credentials live in.
var settingWriters = map[string][]string{
	"os": {
		"Setenv", "Unsetenv", "Clearenv", "WriteFile", "Create", "CreateTemp", "OpenFile", "Mkdir",
		"MkdirAll", "MkdirTemp", "Remove", "RemoveAll", "Rename", "Chmod", "Chown", "Lchown", "Chtimes",
		"Symlink", "Link", "Truncate",
	},
	"github.com/zalando/go-keyring": {"Set", "Delete", "DeleteAll"},
}

// modulePath is this module's import path, the prefix that tells the
// server's own packages from everything else.
const modulePath = "github.com/mmedum/favro-mcp/v4"

// TestNoToolWritesTheServersOwnSettings holds standard §3's "the model
// never widens its own reach": no tool turns on a flag, moves the upload
// directory, or rewrites the credentials, because nothing a tool can
// reach writes any of them.
//
// The packages are every one this package imports from the module,
// followed to the end, so a package added under the tools is read by
// being imported. In them, a call that writes the environment, a file
// or the keyring may sit only in a method of auth.KeyringSource, which
// `favro-mcp auth` uses and no reachable package outside internal/auth
// may name. The registry's own fields, the flags as registration read
// them, are set in Register's composite literal and assigned nowhere.
func TestNoToolWritesTheServersOwnSettings(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]bool{}
	rt := reflect.TypeFor[registry]()
	for i := range rt.NumField() {
		fields[rt.Field(i).Name] = true
	}

	seen := map[string]bool{}
	queue := []string{modulePath + "/internal/tools"}
	var files, calls int
	var writers []string
	fset := token.NewFileSet()
	for len(queue) > 0 {
		pkg := queue[0]
		queue = queue[1:]
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		dir := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pkg, modulePath+"/")))
		bp, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		for _, imp := range bp.Imports {
			if strings.HasPrefix(imp, modulePath+"/") {
				queue = append(queue, imp)
			}
		}
		for _, name := range bp.GoFiles {
			path := filepath.Join(dir, name)
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files++
			imports := map[string]string{}
			for _, is := range file.Imports {
				p, _ := strconv.Unquote(is.Path.Value)
				local := filepath.Base(p)
				if p == "github.com/zalando/go-keyring" {
					local = "keyring"
				}
				if is.Name != nil {
					local = is.Name.Name
				}
				imports[local] = p
			}
			inAuth := pkg == modulePath+"/internal/auth"
			for _, decl := range file.Decls {
				fn, _ := decl.(*ast.FuncDecl)
				ast.Inspect(decl, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.CallExpr:
						calls++
						sel, ok := n.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}
						x, ok := sel.X.(*ast.Ident)
						if !ok || !slices.Contains(settingWriters[imports[x.Name]], sel.Sel.Name) {
							return true
						}
						at := fset.Position(n.Pos()).String()
						if !inAuth || fn == nil || receiverName(fn) != "KeyringSource" {
							t.Errorf("%s: %s.%s writes the server's own settings, and a tool can reach it", at, x.Name, sel.Sel.Name)
						}
						writers = append(writers, at)
					case *ast.SelectorExpr:
						if x, ok := n.X.(*ast.Ident); ok && !inAuth && imports[x.Name] == modulePath+"/internal/auth" &&
							n.Sel.Name == "KeyringSource" {
							t.Errorf("%s: names auth.KeyringSource, which writes the credentials", fset.Position(n.Pos()))
						}
					case *ast.AssignStmt:
						for _, lhs := range n.Lhs {
							if sel, ok := lhs.(*ast.SelectorExpr); ok && pkg == modulePath+"/internal/tools" && fields[sel.Sel.Name] {
								t.Errorf("%s: assigns the registry's %s after Register built it", fset.Position(n.Pos()), sel.Sel.Name)
							}
						}
					}
					return true
				})
			}
		}
	}

	// The floor. A walk that stopped at this package, or read no calls,
	// finds nothing and passes; these are what it reads today, less a
	// margin for code that moves.
	if len(seen) < 7 || files < 90 || calls < 1800 {
		t.Errorf("read %d packages, %d files and %d calls; want at least 7, 90 and 1800", len(seen), files, calls)
	}
	// And the one place a write is allowed has to be found, or the
	// check is not looking at the keyring at all.
	if len(writers) != 4 {
		t.Errorf("found %d writes, want only the 4 in auth.KeyringSource: %v", len(writers), writers)
	}
}

// receiverName is the type a method is declared on, or "".
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch r := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return r.Name
	case *ast.StarExpr:
		if id, ok := r.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}
