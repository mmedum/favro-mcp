package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The three files this gate reads. Named rather than searched for,
// because "the document moved and the gate found nothing to disagree
// with" is the failure mode every gate here is written against.
const (
	classesSourcePath = "internal/render/class.go"
	classesDocPath    = "docs/architecture.md"
	classesDocSection = "### 6.2 Error classes"
)

// classesFloor is the smallest vocabulary this repository can have: the
// standard's six plus the three §6.2 argues Favro forces. A gate that
// agreed two empty sets matched would pass forever.
const classesFloor = 9

// classesDocRow matches a table row whose first cell is a backticked
// class name — the shape §6.2's table uses, and one that prose
// mentioning `invalid` in passing cannot produce.
var classesDocRow = regexp.MustCompile("^\\|\\s*`([a-z_]+)`\\s*\\|")

// classes holds the closed error vocabulary and docs/architecture.md
// §6.2 to each other, from both sides.
//
// One side is easy to see the need for: a class the code emits and the
// document does not name leaves a caller reading a vocabulary that is
// missing a member. The other side is the one that actually rots — a
// documented class no code emits reads, to anyone deciding how to
// handle an error, exactly like one that happens not to have occurred
// yet. Both fail here.
//
// "Emits" is checked as a reference from real code rather than as a
// declaration, because a constant nothing returns is not part of a
// vocabulary. That is what catches the class somebody adds in
// anticipation and never wires up.
func classes(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	sourcePath := filepath.Join(root, classesSourcePath)
	declared, err := declaredClasses(sourcePath)
	if err != nil {
		return err
	}
	listed, err := listedClasses(sourcePath)
	if err != nil {
		return err
	}
	if len(declared) < classesFloor {
		return fmt.Errorf("read %d class constants from %s; the vocabulary is the standard's six plus the three §6.2 adds, so it cannot be smaller than %d",
			len(declared), classesSourcePath, classesFloor)
	}

	documented, docLines, err := documentedClasses(filepath.Join(root, classesDocPath))
	if err != nil {
		return err
	}
	if docLines == 0 {
		return fmt.Errorf("%s: found no %q section to read; the gate compared nothing",
			classesDocPath, classesDocSection)
	}

	emitted, filesRead, err := emittedClasses(root, declared)
	if err != nil {
		return err
	}
	if filesRead < 40 {
		return fmt.Errorf("walked only %d Go files looking for class references; that is not the whole module", filesRead)
	}

	var problems []string
	// The Classes slice is the vocabulary a caller ranges over, and
	// nothing in Go makes it agree with the constants. Checked here
	// rather than in a unit test next to it, so the const block is
	// parsed once in the whole repository instead of twice.
	for _, c := range declared {
		if !slices.Contains(listed, c) {
			problems = append(problems, fmt.Sprintf("%s declares %q and leaves it out of the Classes slice", classesSourcePath, c))
		}
	}
	for _, c := range listed {
		if !slices.Contains(declared, c) {
			problems = append(problems, fmt.Sprintf("%s lists %q in Classes with no constant declaring it", classesSourcePath, c))
		}
	}
	for _, c := range declared {
		if !slices.Contains(documented, c) {
			problems = append(problems, fmt.Sprintf("%s declares %q, and %s §6.2 does not name it", classesSourcePath, c, classesDocPath))
		}
		if !slices.Contains(emitted, c) {
			problems = append(problems, fmt.Sprintf("%s declares %q and no code outside the declaration returns it — a class nothing emits is not part of the vocabulary", classesSourcePath, c))
		}
	}
	for _, c := range documented {
		if !slices.Contains(declared, c) {
			problems = append(problems, fmt.Sprintf("%s §6.2 documents %q, and %s declares no constant for it", classesDocPath, c, classesSourcePath))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("the error vocabulary and its documentation disagree:\n  %s", strings.Join(problems, "\n  "))
	}

	_, err = fmt.Fprintf(w, "classes: %d in the vocabulary, each declared and listed in %s, emitted somewhere in %d Go files, and named in %s §6.2\n",
		len(declared), classesSourcePath, filesRead, classesDocPath)
	return err
}

// declaredClasses parses the class constants out of the source. The
// names are the string values, not the Go identifiers: the value is
// what a caller sees in "[class] message", and it is what the document
// has to name.
func declaredClasses(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		// A const block states its type once and inherits it down the
		// block, so the type is tracked across specs.
		var blockType string
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if ident, ok := vs.Type.(*ast.Ident); ok {
				blockType = ident.Name
			}
			if blockType != "Class" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, err
				}
				out = append(out, value)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// listedClasses returns the class constants named in the Classes slice,
// as their wire values, by resolving each identifier back through the
// const block.
func listedClasses(path string) ([]string, error) {
	declared, err := declaredClasses(path)
	if err != nil {
		return nil, err
	}
	byIdent := make(map[string]string, len(declared))
	for _, c := range declared {
		byIdent["Class"+camel(c)] = c
	}

	file, err := parseGoFile(path)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) == 0 || vs.Names[0].Name != "Classes" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, el := range lit.Elts {
					ident, ok := el.(*ast.Ident)
					if !ok {
						continue
					}
					if c, ok := byIdent[ident.Name]; ok {
						out = append(out, c)
					} else {
						// An identifier in the slice that is not a
						// declared constant: reported as an unknown
						// value so the caller sees it rather than a
						// silently shorter list.
						out = append(out, ident.Name)
					}
				}
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// documentedClasses reads §6.2's table. It returns the classes and the
// number of table rows it saw, so a section that was renamed out from
// under it fails rather than agreeing with an empty set.
func documentedClasses(path string) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	_, rest, ok := strings.Cut(string(data), classesDocSection)
	if !ok {
		return nil, 0, nil
	}

	var out []string
	var rows int
	for line := range strings.Lines(rest) {
		trimmed := strings.TrimSpace(line)
		// Stop at the next section of any depth, so a class named in
		// §6.3 is not read as §6.2's.
		if strings.HasPrefix(trimmed, "#") {
			break
		}
		m := classesDocRow.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		rows++
		out = append(out, m[1])
	}
	slices.Sort(out)
	return slices.Compact(out), rows, nil
}

// emittedClasses walks the module for references to each class's Go
// identifier outside its own declaration, and returns the classes that
// have one plus the number of Go files walked.
//
// Identifiers, not string values: a class is returned as
// render.ClassNotFound, and the string "not_found" appears in the
// declaration and in test assertions and nowhere a value is produced.
// Test files are excluded for the same reason — a class only a test
// mentions is one nothing emits.
//
// The declaring file is walked like any other, minus the declarations
// themselves. Classify is in it, and Classify is where six of the nine
// are returned from; skipping the file wholesale reported those six as
// emitted by nothing, which was this gate's own first finding about
// itself.
func emittedClasses(root string, declared []string) ([]string, int, error) {
	idents := make(map[string]string, len(declared))
	for _, c := range declared {
		idents["Class"+camel(c)] = c
	}

	// The tracked files, the way every other gate here enumerates: a
	// tree walk would also pick up build output and whatever is in a
	// developer's working directory, and neither is the module.
	files, err := gitLines(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, 0, fmt.Errorf("git ls-files: %w", err)
	}

	found := map[string]bool{}
	var filesRead int
	for _, name := range files {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		filesRead++
		file, err := parseGoFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			// A file this gate cannot parse is not a class reference,
			// and `go build` is the thing that should complain.
			continue
		}
		declaring := name == classesSourcePath
		for _, decl := range file.Decls {
			if declaring && isClassDeclaration(decl) {
				continue
			}
			// Only *ast.Ident. A qualified render.ClassNotFound is a
			// SelectorExpr whose Sel is that Ident, and Inspect
			// descends into it, so a case for the selector would match
			// the same name twice.
			ast.Inspect(decl, func(n ast.Node) bool {
				ident, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				if c, ok := idents[ident.Name]; ok {
					found[c] = true
				}
				return true
			})
		}
	}

	out := make([]string, 0, len(found))
	for c := range found {
		out = append(out, c)
	}
	slices.Sort(out)
	return out, filesRead, nil
}

// isClassDeclaration reports whether decl is the vocabulary declaring
// itself — the const block of Class constants, or the Classes slice
// that lists them. Neither is an emission; everything else in the file,
// Classify included, is.
func isClassDeclaration(decl ast.Decl) bool {
	gen, ok := decl.(*ast.GenDecl)
	if !ok {
		return false
	}
	switch gen.Tok {
	case token.CONST:
		var blockType string
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if ident, ok := vs.Type.(*ast.Ident); ok {
				blockType = ident.Name
			}
		}
		return blockType == "Class"
	case token.VAR:
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				if name.Name == "Classes" {
					return true
				}
			}
		}
	}
	return false
}

// camel turns a wire value into the Go identifier suffix: "not_found"
// into "NotFound". The two spellings are the one place this gate has to
// know the naming convention, and the floor check is what catches a
// convention that changed.
func camel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// parseGoFile parses one Go file. Exists so the test can reach the same
// parse the gate uses rather than building its own and drifting.
func parseGoFile(path string) (*ast.File, error) {
	return parser.ParseFile(token.NewFileSet(), path, nil, 0)
}
