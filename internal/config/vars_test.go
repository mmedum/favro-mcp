package config_test

import (
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
// three credentials CredentialsFrom reads included.
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
	if want := []string{"FAVRO_ENABLE_DESTRUCTIVE", "FAVRO_MCP_SKIP_VALIDATE"}; !slices.Equal(switches, want) {
		t.Errorf("switches %v, want %v", switches, want)
	}
	if want := []string{"FAVRO_INTERACTION_HINT", "FAVRO_REQUIRE_PROMPT"}; !slices.Equal(removed, want) {
		t.Errorf("Vars marks %v removed, want %v", removed, want)
	}
}

// Secret marks the one credential a form must not show, the API token:
// the email and the organization id say which account, and the token
// signs in as it. The bundle gate holds a secret's field to sensitive.
func TestOnlyTheAPITokenIsSecret(t *testing.T) {
	var secrets []string
	for _, v := range registry(t) {
		if v.Secret {
			secrets = append(secrets, v.Name)
		}
	}
	if want := []string{"FAVRO_API_TOKEN"}; !slices.Equal(secrets, want) {
		t.Errorf("Vars marks %v secret, want %v", secrets, want)
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
	// A variable LoadFrom reads and CredentialsFrom reads too would be
	// read under two rules; EnvVars lists it twice, so this sees it.
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
	credential := func(name string) func(string) string {
		return func(value string) string {
			env := map[string]string{name: value}
			return fmt.Sprintf("%+v", config.CredentialsFrom(func(n string) string { return env[n] }))
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
// a variable read outside LoadFrom cannot escape the bundle and staleness
// gates. So is every one scripts/ spells: the live driver, the evals
// harness and the smoke gate set them for the server, and a misspelling
// there tests a configuration nobody asked for. The scan must read every
// root and find a name, or a use of config's constants, outside the
// config package, or it read too little.
func TestEveryVariableTheCodeNamesIsInEnvVars(t *testing.T) {
	known := map[string]bool{}
	for _, name := range config.EnvVars() {
		known[name] = true
	}
	scan := scanCode(t)
	if scan.elsewhere+scan.constants == 0 {
		t.Error("the scan found no FAVRO_ name and no config constant outside internal/config; it read too little")
	}
	for _, v := range slices.Sorted(maps.Keys(scan.found)) {
		if !known[v] {
			t.Errorf("%s names %s, which EnvVars does not list", scan.found[v], v)
		}
	}
}

// A server variable is spelled once, in internal/config: everything else
// names it by config's constant, or as EnvPrefix and its key, so a rename
// is one edit and a misspelling does not compile. A struct tag is the one
// exception, since a tag cannot name a constant. The scan must find a
// use of the constants, or it read too little.
func TestServerVariablesAreSpelledOnce(t *testing.T) {
	scan := scanCode(t)
	if scan.constants == 0 {
		t.Error("the scan found no use of config's Env constants outside internal/config; it read too little")
	}
	for _, at := range scan.typed {
		t.Errorf("%s types a server variable by hand; name it by config's constant", at)
	}
}

// The scan reads a + chain in order, so a name built after another
// operand is seen; and a run another operand completes is a prefix, not
// a name, while the words before it in the run still are. A struct tag
// types a name by hand and is not refused for it; anything else is.
func TestTheScanReadsChainsAndTags(t *testing.T) {
	const src = "package p\n\n" +
		"type T struct {\n\tPath string `jsonschema:\"inside FAVRO_TAGGED\"`\n}\n\n" +
		"var (\n" +
		"\ta = EnvPrefix + \"ENABLE_\" + kind\n" +
		"\tb = \"set \" + x + EnvPrefix + \"NEW\"\n" +
		"\tc = \"FAVRO_OLD or \" + config.EnvPrefix + \"READ_\" + kind + \"_ONLY\"\n" +
		"\td = config.EnvUploadDir + \"=\" + dir\n" +
		")\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Sorted(maps.Keys(spellings(fset, f)))
	if want := []string{"FAVRO_NEW", "FAVRO_OLD", "FAVRO_TAGGED"}; !slices.Equal(got, want) {
		t.Errorf("the names spelled are %q, want %q", got, want)
	}
	typed, constants := handTyped(fset, f)
	if want := []string{`p.go:10:6: "FAVRO_OLD"`}; !slices.Equal(typed, want) {
		t.Errorf("typed by hand: %q, want %q", typed, want)
	}
	if constants != 2 {
		t.Errorf("the scan counts %d uses of config's constants, want 2", constants)
	}
}

// codeScan is what a scan of the server's Go code, tests aside, found.
type codeScan struct {
	// found is every FAVRO_ name the code spells, with where.
	found map[string]string
	// elsewhere counts the names spelled outside internal/config, and
	// constants the uses of config's Env constants there.
	elsewhere, constants int
	// typed is every literal outside internal/config typing the prefix.
	typed []string
}

// scanCode scans every Go file under cmd/, internal/ and scripts/,
// tests aside, and fails when a root yields none.
func scanCode(t *testing.T) codeScan {
	t.Helper()
	scan := codeScan{found: map[string]string{}}
	fset := token.NewFileSet()
	for _, root := range []string{"../../cmd", "../../internal", "../../scripts"} {
		files := 0
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			files++
			outside := filepath.Dir(path) != filepath.Join("..", "..", "internal", "config")
			for name, at := range spellings(fset, f) {
				scan.found[name] = at
				if outside {
					scan.elsewhere++
				}
			}
			if outside {
				typed, constants := handTyped(fset, f)
				scan.typed = append(scan.typed, typed...)
				scan.constants += constants
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if files == 0 {
			t.Errorf("the scan parsed no Go file under %s; it read too little", root)
		}
	}
	return scan
}

// spellings is every FAVRO_ name f spells, with where: in a string
// literal, or in a + chain of literals and EnvPrefix read in order, each
// run of them one string. A run another operand follows is a prefix that
// operand completes, as in EnvPrefix + "ENABLE_" + kind, so the word it
// ends with is no name; the words before it are. A name built from any
// other constant, or through a format verb, is not seen.
func spellings(fset *token.FileSet, f *ast.File) map[string]string {
	found := map[string]string{}
	spell := func(s string, at token.Pos, open bool) {
		words := strings.FieldsFunc(s, notNameRune)
		if open && len(words) > 0 && strings.HasSuffix(s, words[len(words)-1]) {
			words = words[:len(words)-1]
		}
		for _, word := range words {
			if isName(word) {
				found[word] = fset.Position(at).String()
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
			run, at := "", n.Pos()
			for _, op := range operands(n) {
				if s, ok := joined(op); ok {
					if run == "" {
						at = op.Pos()
					}
					run += s
					continue
				}
				spell(run, at, true)
				run = ""
				ast.Inspect(op, visit)
			}
			spell(run, at, false)
			return false
		case *ast.BasicLit:
			if s, err := strconv.Unquote(n.Value); n.Kind == token.STRING && err == nil {
				spell(s, n.Pos(), false)
			}
		}
		return true
	}
	ast.Inspect(f, visit)
	return found
}

// handTyped is every string literal in f, a struct tag aside, holding a
// word that starts with the prefix, with where; and how many times f
// names one of config's Env constants or EnvPrefix.
func handTyped(fset *token.FileSet, f *ast.File) (typed []string, constants int) {
	tags := map[*ast.BasicLit]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			if n.Tag != nil {
				tags[n.Tag] = true
			}
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok && x.Name == "config" && strings.HasPrefix(n.Sel.Name, "Env") {
				constants++
			}
		case *ast.BasicLit:
			s, err := strconv.Unquote(n.Value)
			if n.Kind != token.STRING || err != nil || tags[n] {
				return true
			}
			for _, word := range strings.FieldsFunc(s, notNameRune) {
				if strings.HasPrefix(word, config.EnvPrefix) {
					typed = append(typed, fmt.Sprintf("%s: %q", fset.Position(n.Pos()), word))
				}
			}
		}
		return true
	})
	return typed, constants
}

// notNameRune reports whether r cannot be part of a variable's name.
func notNameRune(r rune) bool {
	return r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
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
