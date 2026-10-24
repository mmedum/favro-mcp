package config_test

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/v4/internal/auth"
	"github.com/mmedum/favro-mcp/v4/internal/config"
)

// load is config.LoadFrom over env alone.
func load(env map[string]string) config.Config {
	return config.LoadFrom(func(name string) string { return env[name] })
}

// varsFloor is how many variables the server reads today, on purpose:
// the floor moves with it, so a registry that records nothing cannot
// pass a walk over it.
const varsFloor = 9

// registry is config.Vars, refused below the floor.
func registry(t *testing.T) []config.Var {
	t.Helper()
	vars := config.Vars()
	if len(vars) < varsFloor {
		t.Fatalf("Vars has %d variables, want at least %d", len(vars), varsFloor)
	}
	return vars
}

// EnvVars is every variable the server reads, the removed ones and the
// three credentials auth reads included.
func TestEnvVars(t *testing.T) {
	want := []string{
		"FAVRO_API_TOKEN", "FAVRO_ENABLE_DESTRUCTIVE", "FAVRO_INTERACTION_HINT", "FAVRO_LOG_LEVEL",
		"FAVRO_MCP_SKIP_VALIDATE", "FAVRO_ORGANIZATION_ID", "FAVRO_REQUIRE_PROMPT", "FAVRO_UPLOAD_DIR",
		"FAVRO_USER_EMAIL",
	}
	if got := config.EnvVars(); !slices.Equal(got, want) {
		t.Errorf("EnvVars =\n%v\nwant\n%v", got, want)
	}
}

// Vars is what the bundle gate reads a setting's kind from, so each
// claim it makes is held to what Load does. A switch loads true and
// false differently, warning of neither, and warns of anything else. A
// removed setting changes nothing but what Load refuses or warns of,
// whatever it is set to, and something it is set to does one of those.
func TestVarsDescribeWhatLoadDoes(t *testing.T) {
	empty := load(nil)
	// besides is a configuration with what Load refuses and warns of
	// taken out.
	besides := func(c config.Config) config.Config {
		c.Refusals, c.Warnings = nil, nil
		return c
	}
	quiet := func(c config.Config) bool { return len(c.Refusals) == 0 && len(c.Warnings) == 0 }
	defined := map[string]bool{}
	for _, v := range config.Defined() {
		defined[v.Name] = true
	}
	var switches, removed []string
	for _, v := range registry(t) {
		if !defined[v.Name] {
			if v.Switch || v.Removed {
				t.Errorf("%s is read outside Load and marked a switch or removed", v.Name)
			}
			continue
		}
		with := func(value string) config.Config { return load(map[string]string{v.Name: value}) }
		on, off := with("true"), with("false")
		boolean := quiet(on) && quiet(off) && !reflect.DeepEqual(on, off) && len(with("maybe").Warnings) > 0
		if boolean != v.Switch {
			t.Errorf("%s: Switch is %t, and Load reads it as a boolean: %t", v.Name, v.Switch, boolean)
		}
		gone, says := true, false
		for _, value := range []string{"true", "false", "maybe", "debug", t.TempDir()} {
			c := with(value)
			gone = gone && reflect.DeepEqual(besides(c), besides(empty))
			says = says || !quiet(c)
		}
		if only := gone && says; only != v.Removed {
			t.Errorf("%s: Removed is %t, and Load only refuses or warns of it: %t", v.Name, v.Removed, only)
		}
		if v.Switch {
			switches = append(switches, v.Name)
		}
		if v.Removed {
			removed = append(removed, v.Name)
		}
	}
	if want := []string{"FAVRO_ENABLE_DESTRUCTIVE"}; !slices.Equal(switches, want) {
		t.Errorf("switches %v, want %v", switches, want)
	}
	if want := []string{"FAVRO_INTERACTION_HINT", "FAVRO_REQUIRE_PROMPT"}; !slices.Equal(removed, want) {
		t.Errorf("Vars marks %v removed, want %v", removed, want)
	}
}

// Load reads each variable once, and records each once for Vars. Two
// settings on one variable would leave it to whichever is read last.
func TestLoadReadsEachVariableOnce(t *testing.T) {
	registry(t)
	reads := map[string]int{}
	config.LoadFrom(func(name string) string { reads[name]++; return "" })
	var listed []string
	for _, v := range config.Defined() {
		listed = append(listed, v.Name)
	}
	slices.Sort(listed)
	if read := slices.Sorted(maps.Keys(reads)); !slices.Equal(listed, read) {
		t.Errorf("Load records %v\nLoad reads %v", listed, read)
	}
	for name, n := range reads {
		if n != 1 {
			t.Errorf("Load reads %s %d times", name, n)
		}
	}
	// A variable Load reads and auth reads too would be read under two
	// rules; EnvVars lists it twice, so this sees it.
	if names := config.EnvVars(); !slices.Equal(names, slices.Compact(slices.Clone(names))) {
		t.Errorf("EnvVars names a variable twice: %v", names)
	}
}

// A variable of spaces alone is unset, as an empty one is, and the spaces
// around a value are dropped, so a form field cleared to a space starts
// the server with the default. That holds for every variable the server
// reads: Load's, and each one read outside Load through its reader. One
// read outside Load with no reader here fails, so a variable added there
// cannot miss the rule.
func TestASettingOfSpacesIsUnset(t *testing.T) {
	// A credential is read with the other two set, so the value read is
	// the one in the token rather than lost to a missing field.
	credential := func(name string) func(string) string {
		return func(value string) string {
			env := map[string]string{
				config.EnvUserEmail: "someone@example.test", config.EnvAPIToken: "synthetic-token",
				config.EnvOrganizationID: "synthetic-organization", name: value,
			}
			tok, err := auth.EnvSource{Lookup: func(n string) string { return env[n] }}.Load(context.Background())
			return fmt.Sprintf("%+v %v", tok, err)
		}
	}
	outside := map[string]func(string) string{
		config.EnvUserEmail:      credential(config.EnvUserEmail),
		config.EnvAPIToken:       credential(config.EnvAPIToken),
		config.EnvOrganizationID: credential(config.EnvOrganizationID),
	}
	defined := map[string]bool{}
	for _, v := range config.Defined() {
		defined[v.Name] = true
	}
	dir := t.TempDir()
	for _, v := range registry(t) {
		read := func(value string) string { return fmt.Sprintf("%+v", load(map[string]string{v.Name: value})) }
		if !defined[v.Name] {
			var ok bool
			if read, ok = outside[v.Name]; !ok {
				t.Errorf("%s is read outside Load, and this test has no reader for it", v.Name)
				continue
			}
			delete(outside, v.Name)
		}
		for _, pair := range [][2]string{{"  ", ""}, {" \t", ""}, {" x ", "x"}, {" true ", "true"}, {" " + dir + " ", dir}} {
			if got, want := read(pair[0]), read(pair[1]); got != want {
				t.Errorf("%s=%q: %s\nwant %s, as %s=%q", v.Name, pair[0], got, want, v.Name, pair[1])
			}
		}
	}
	for name := range outside {
		t.Errorf("this test has a reader for %s, which Vars does not list as read outside Load", name)
	}
}

// Lookup is the value without the spaces around it.
func TestLookup(t *testing.T) {
	env := map[string]string{"A": " a b ", "B": "   "}
	for name, want := range map[string]string{"A": "a b", "B": "", "C": ""} {
		if got := config.Lookup(func(n string) string { return env[n] }, name); got != want {
			t.Errorf("Lookup(%s) = %q, want %q", name, got, want)
		}
	}
}

// Every FAVRO_ variable the server's code spells is one EnvVars lists, so
// a variable read outside Load cannot escape the bundle and staleness
// gates. So is every one scripts/ spells: the live driver, the evals
// harness and the smoke gate set them for the server, and a misspelling
// there tests a configuration nobody asked for. A name is seen as a
// string literal, or as a chain of literals and EnvPrefix joined by +; one
// built from any other constant, or through a format verb, is not seen.
// The scan must read every root and find a name outside the config
// package, or it read too little.
func TestEveryVariableTheCodeNamesIsInEnvVars(t *testing.T) {
	known := map[string]bool{}
	for _, name := range config.EnvVars() {
		known[name] = true
	}
	found := map[string]string{}
	elsewhere := 0
	fset := token.NewFileSet()
	roots := []string{"../../cmd", "../../internal", "../../scripts"}
	files := map[string]int{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			files[root]++
			outside := filepath.Dir(path) != filepath.Join("..", "..", "internal", "config")
			spell := func(s string, at token.Pos) {
				for _, word := range strings.FieldsFunc(s, func(r rune) bool {
					return r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
				}) {
					if isName(word) {
						found[word] = fset.Position(at).String()
						if outside {
							elsewhere++
						}
					}
				}
			}
			var visit func(n ast.Node) bool
			visit = func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.BinaryExpr:
					if n.Op != token.ADD {
						return true
					}
					// A + chain is read in order: each run of literals and
					// EnvPrefix is one string, and any other operand ends
					// the run and is read on its own.
					run, at := "", n.Pos()
					for _, op := range operands(n) {
						if s, ok := joined(op); ok {
							if run == "" {
								at = op.Pos()
							}
							run += s
							continue
						}
						spell(run, at)
						run = ""
						ast.Inspect(op, visit)
					}
					spell(run, at)
					return false
				case *ast.BasicLit:
					if s, err := strconv.Unquote(n.Value); n.Kind == token.STRING && err == nil {
						spell(s, n.Pos())
					}
				}
				return true
			}
			ast.Inspect(f, visit)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range roots {
		if files[root] == 0 {
			t.Errorf("the scan parsed no Go file under %s; it read too little", root)
		}
	}
	if elsewhere == 0 {
		t.Error("the scan found no FAVRO_ name outside internal/config; it read too little")
	}
	for _, v := range slices.Sorted(maps.Keys(found)) {
		if !known[v] {
			t.Errorf("%s names %s, which EnvVars does not list", found[v], v)
		}
	}
}

// isName reports whether a word is a FAVRO_ name: the prefix and a
// letter or digit after it.
func isName(word string) bool {
	rest, ok := strings.CutPrefix(word, config.EnvPrefix)
	return ok && rest != "" && rest[0] != '_'
}

// operands is a + chain's operands in order, through parentheses.
func operands(e ast.Expr) []ast.Expr {
	switch e := e.(type) {
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return append(operands(e.X), operands(e.Y)...)
		}
	case *ast.ParenExpr:
		if b, ok := e.X.(*ast.BinaryExpr); ok && b.Op == token.ADD {
			return operands(b)
		}
	}
	return []ast.Expr{e}
}

// joined is a + chain of string literals and EnvPrefix as one string.
func joined(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		x, ok := joined(e.X)
		if !ok {
			return "", false
		}
		y, ok := joined(e.Y)
		return x + y, ok
	case *ast.ParenExpr:
		return joined(e.X)
	case *ast.BasicLit:
		s, err := strconv.Unquote(e.Value)
		return s, e.Kind == token.STRING && err == nil
	case *ast.Ident:
		return config.EnvPrefix, e.Name == "EnvPrefix"
	case *ast.SelectorExpr:
		return config.EnvPrefix, e.Sel.Name == "EnvPrefix"
	}
	return "", false
}
