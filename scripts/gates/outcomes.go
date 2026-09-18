package main

import (
	"encoding/csv"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// outcomes holds hard rule 2 — a 200 from Favro is not confirmation — to
// the one shape of it a machine can check.
//
// Adopted from google-drive-mcp, which is the only sibling that has it,
// and which wrote it after a live run found three tools asserting things
// the response never carried. This repository shipped its own instance
// the week this gate arrived: `favro_move_card` returned success because
// the request had been accepted, never because the response confirmed a
// move, and it did that for every release from 1.0.0 to 2.0.1. The fix
// was to consult the response before speaking. This is that, as a check.
//
// The checkable shape is: a branch tests a boolean field of the REQUEST
// and then states, in prose, what is now true. The response is never
// consulted; the argument the caller passed is treated as its own
// evidence.
//
// Three shapes are honest and the gate recognises all three structurally
// rather than by a list of function names:
//
//   - the branch consults Favro, by calling through the client or by
//     entering the dry-run context;
//   - the branch refuses, returning an error;
//   - the prose is hypothetical. This repository's dry-run vocabulary is
//     "would …", which states what was ASKED FOR rather than what
//     happened, and a state-diff closure is only ever reached on the
//     dry-run path.
//
// What it cannot do is judge the words, and the honest form of a
// flagged branch looks the same to a parser. So a branch that is right
// anyway takes a row in testdata/outcome-claims.tsv with the reason, and
// the gate's job is the one a machine can do: make somebody look at
// every branch of this shape, and refuse a new one nobody has looked at.
//
// Note the unit is the BRANCH, not the function. Two correct sites in
// this repository read a request field and then speak: the delete-card
// state diff branches on `in.Everywhere` to word two different "would"
// sentences, and every write handler branches on `in.DryRun` to pick a
// context. Both consult something, or state a hypothetical, before they
// say anything. What makes a branch honest is that it consults something
// before it speaks.
func outcomes(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, filepath.FromSlash(outcomeScanDir))

	fset := token.NewFileSet()
	files, err := parseOutcomeDir(fset, dir)
	if err != nil {
		return err
	}
	// A scan that read too little reports a clean package for ever.
	if len(files) < minOutcomeFiles {
		return fmt.Errorf("read %d Go file(s) in %s, expected at least %d — this check is not looking at the code it is meant to",
			len(files), outcomeScanDir, minOutcomeFiles)
	}
	fields := boolInputFields(files)
	if len(fields) < minOutcomeFields {
		return fmt.Errorf("found %d boolean input field(s) in %s, expected at least %d — this check is not looking at the code it is meant to",
			len(fields), outcomeScanDir, minOutcomeFields)
	}

	exempt, err := readOutcomeClaims(filepath.Join(root, filepath.FromSlash(outcomeClaimsFile)))
	if err != nil {
		return err
	}

	var problems []string
	used := map[string]int{}
	claims, branches := outcomeClaims(files, fields, fset)
	// A rule with nothing of its shape to look at is a rule nobody is
	// holding. Every write tool in this repository branches on DryRun,
	// so a run that finds no branch at all has stopped recognising them.
	if branches < minOutcomeBranches {
		return fmt.Errorf("found %d branch(es) testing a boolean request field, expected at least %d — this check is not recognising them any more",
			branches, minOutcomeBranches)
	}

	for _, c := range claims {
		key := c.file + "\t" + c.field
		if _, ok := exempt[key]; ok {
			used[key]++
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s:%d: this branch tests the request field %s and then states an outcome — %q — without asking Favro anything. Read it back, or word it as what was asked for rather than what happened, or record it in %s with the reason",
			c.file, c.line, c.field, c.quote, outcomeClaimsFile))
	}

	// A stale exemption outlives the code it excused and reads as a
	// decision somebody made about today's code.
	for _, key := range sortedStringKeys(exempt) {
		file, field, _ := strings.Cut(key, "\t")
		switch {
		case used[key] == 0:
			problems = append(problems, fmt.Sprintf(
				"%s excuses %s (%s) and nothing there states an outcome from the request any more; delete the row",
				outcomeClaimsFile, field, file))
		case used[key] > 1:
			// One row, more than one branch. A key names a file and a
			// field, so a second branch testing the same boolean in the
			// same file would be excused by an argument written about
			// the first — silently, and it is the new branch nobody has
			// looked at. Keying on the line instead would make every row
			// stale on the next edit above it.
			problems = append(problems, fmt.Sprintf(
				"%s excuses %s (%s) once and %d branches there state an outcome after testing it. One argument cannot cover two branches: make them one, or word the second so it does not state an outcome",
				outcomeClaimsFile, field, file, used[key]))
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		for _, p := range problems {
			_, _ = fmt.Fprintln(w, p)
		}
		return fmt.Errorf("%d outcome(s) asserted from the request", len(problems))
	}

	_, err = fmt.Fprintf(w,
		"outcomes ok: %d boolean request fields across %d files in %s, %d branch(es) testing one, %d excused with a reason\n",
		len(fields), len(files), outcomeScanDir, branches, len(exempt))
	return err
}

const (
	// outcomeScanDir is the MCP surface: where the *Input types are
	// declared and where both the handlers and the state-diff closures
	// that word a result live.
	outcomeScanDir = "internal/tools"
	// outcomeClaimsFile records the branches somebody has looked at and
	// judged honest, one per line, with the reason.
	outcomeClaimsFile = "testdata/outcome-claims.tsv"

	// The floors. Zero findings and zero inputs print the same sentence.
	minOutcomeFiles    = 30
	minOutcomeFields   = 8
	minOutcomeBranches = 30
)

// outcomeFile is one parsed file, kept with its repo-relative path so a
// failure names a place rather than a syntax tree.
type outcomeFile struct {
	path string
	file *ast.File
}

// outcomeClaim is one branch that states an outcome from the request.
type outcomeClaim struct {
	file  string
	line  int
	field string
	quote string
}

func parseOutcomeDir(fset *token.FileSet, dir string) ([]outcomeFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%w (the MCP surface is %s)", err, outcomeScanDir)
	}
	var out []outcomeFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		parsed, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if perr != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), perr)
		}
		out = append(out, outcomeFile{path: outcomeScanDir + "/" + e.Name(), file: parsed})
	}
	return out, nil
}

// boolInputFields is every boolean field on every *Input type, derived
// from the source rather than listed here — a tool added later is
// covered by having declared its input.
func boolInputFields(files []outcomeFile) map[string]bool {
	out := map[string]bool{}
	for _, pf := range files {
		ast.Inspect(pf.file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok || !strings.HasSuffix(strings.ToLower(spec.Name.Name), "input") {
				return true
			}
			st, ok := spec.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				return true
			}
			for _, f := range st.Fields.List {
				if id, ok := f.Type.(*ast.Ident); !ok || id.Name != "bool" {
					continue
				}
				for _, name := range f.Names {
					out[name.Name] = true
				}
			}
			return true
		})
	}
	return out
}

// requestNames is every identifier bound to an *Input value anywhere in
// the file, whether by a top-level func or by one of the handler
// literals passed to addTool.
//
// Per-file rather than per-function on purpose. A state-diff closure
// takes no parameters and reads the handler's `in` from the enclosing
// scope, so a per-function walk — which is what the sibling does, where
// handlers are top-level — would not see the one branch in this
// repository that words two different sentences.
func requestNames(file *ast.File) map[string]bool {
	out := map[string]bool{}
	collect := func(params *ast.FieldList) {
		if params == nil {
			return
		}
		for _, p := range params.List {
			t := p.Type
			if star, ok := t.(*ast.StarExpr); ok {
				t = star.X
			}
			id, ok := t.(*ast.Ident)
			if !ok || !strings.HasSuffix(strings.ToLower(id.Name), "input") {
				continue
			}
			for _, name := range p.Names {
				out[name.Name] = true
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			collect(v.Type.Params)
		case *ast.FuncLit:
			collect(v.Type.Params)
		}
		return true
	})
	return out
}

// outcomeClaims returns the dishonest branches, and how many branches of
// this shape were examined at all.
func outcomeClaims(files []outcomeFile, fields map[string]bool, fset *token.FileSet) ([]outcomeClaim, int) {
	var found []outcomeClaim
	branches := 0
	for _, pf := range files {
		requests := requestNames(pf.file)
		if len(requests) == 0 {
			continue
		}
		ast.Inspect(pf.file, func(n ast.Node) bool {
			stmt, ok := n.(*ast.IfStmt)
			if !ok {
				return true
			}
			field := testedRequestField(stmt.Cond, fields, requests)
			if field == "" {
				return true
			}
			branches++
			if consultsFavro(stmt.Body) || refusesWithError(stmt.Body) {
				return true
			}
			quote, line := statedOutcome(stmt.Body, fset)
			if quote == "" {
				return true
			}
			found = append(found, outcomeClaim{file: pf.path, line: line, field: field, quote: quote})
			return true
		})
	}
	return found, branches
}

// testedRequestField reports the boolean request field a condition
// tests, or "" for a condition about anything else.
func testedRequestField(cond ast.Expr, fields, requests map[string]bool) string {
	name := ""
	ast.Inspect(cond, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || name != "" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && requests[id.Name] && fields[sel.Sel.Name] {
			name = sel.Sel.Name
			return false
		}
		return true
	})
	return name
}

// consultsFavro reports whether the branch asks Favro something before
// it speaks: a call through the client, or entering the dry-run context,
// which is this repository's way of saying "do not perform this".
func consultsFavro(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// r.Client().Whatever(...) — the selector's receiver is itself a
		// Client() call. Matched by shape, not by method name, so a new
		// client method is covered by being one.
		if inner, ok := sel.X.(*ast.CallExpr); ok {
			if innerSel, ok := inner.Fun.(*ast.SelectorExpr); ok && innerSel.Sel.Name == "Client" {
				found = true
				return false
			}
		}
		if sel.Sel.Name == "WithDryRun" {
			found = true
			return false
		}
		return true
	})
	return found
}

// refusesWithError reports whether the branch ends by returning an
// error rather than a result. A refusal states nothing about the world.
func refusesWithError(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	ret, ok := body.List[len(body.List)-1].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, result := range ret.Results {
		found := false
		ast.Inspect(result, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "Errorf" || sel.Sel.Name == "New" {
					found = true
					return false
				}
			}
			if id, ok := call.Fun.(*ast.Ident); ok && (id.Name == "classed" || id.Name == "fmt") {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// statedOutcome returns the first sentence in the branch that states
// what is now true, and the line it is on.
//
// A hypothetical is not an outcome. This repository's dry-run vocabulary
// is "would …", and a sentence in that form describes the request rather
// than the world — which is the honest wording the gate is asking for,
// so recognising it is not an escape hatch but the rule itself.
func statedOutcome(body *ast.BlockStmt, fset *token.FileSet) (string, int) {
	quote, line := "", 0
	ast.Inspect(body, func(n ast.Node) bool {
		if quote != "" {
			return false
		}
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value := strings.Trim(lit.Value, "`\"")
		if !isProse(value) || isHypothetical(value) {
			return true
		}
		quote, line = clipQuote(value), fset.Position(lit.Pos()).Line
		return false
	})
	return quote, line
}

// isProse separates a sentence from a wire key, a format verb or a
// field name. Prose has a space in it and some length to it.
func isProse(s string) bool {
	return len(s) >= 16 && strings.Contains(strings.TrimSpace(s), " ")
}

// isHypothetical recognises the dry-run wording, which states what was
// asked for rather than what happened.
func isHypothetical(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	for _, prefix := range []string{"would ", "asked ", "requested "} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

func clipQuote(s string) string {
	const limit = 70
	if len(s) <= limit {
		return s
	}
	cut := 0
	for i := range s {
		if i > limit {
			break
		}
		cut = i
	}
	return s[:cut] + "…"
}

// readOutcomeClaims reads the record of branches somebody has judged
// honest. Two columns: the file, the field, then the reason.
func readOutcomeClaims(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	r.Comma = '\t'
	r.FieldsPerRecord = -1
	r.Comment = '#'
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", outcomeClaimsFile, err)
	}
	out := map[string]string{}
	for i, row := range rows {
		if len(row) < 3 {
			return nil, fmt.Errorf("%s line %d: want file<TAB>field<TAB>reason, got %d column(s)",
				outcomeClaimsFile, i+1, len(row))
		}
		if strings.TrimSpace(row[2]) == "" {
			return nil, fmt.Errorf("%s line %d: a row with no reason is not a decision anybody made",
				outcomeClaimsFile, i+1)
		}
		out[row[0]+"\t"+row[1]] = row[2]
	}
	return out, nil
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
