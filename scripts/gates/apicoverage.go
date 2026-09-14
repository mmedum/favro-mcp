package main

import (
	"encoding/json"
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

// Verdicts a coverage row may carry.
const (
	verdictImplemented = "implemented"
	verdictOut         = "out"
)

// apiCoverageFloors are what "this gate read something" means. A gate
// that compared two empty sets would print the same cheerful sentence
// as one that compared eighty-eight rows.
const (
	minSurfaceEndpoints = 80
	minCoverageRows     = 50
	minClientMethods    = 40
	minOutReasonChars   = 60 // "not yet" is not a reason
)

// coverageRow is one hand-written verdict.
type coverageRow struct {
	method  string
	path    string
	verdict string
	detail  string
	line    int
}

// prefix reports whether the row covers a family of endpoints rather
// than one.
func (r coverageRow) prefix() (string, bool) {
	p, ok := strings.CutSuffix(r.path, "*")
	return p, ok
}

// matches reports whether this row is the verdict for one endpoint.
// A method of * covers every verb, so a family of endpoints decided
// together can be one row rather than one row per verb.
func (r coverageRow) matches(e apiEndpoint) bool {
	if r.method != "*" && r.method != e.Method {
		return false
	}
	if p, ok := r.prefix(); ok {
		return strings.HasPrefix(e.Path, p)
	}
	return r.path == e.Path
}

// apiCoverage holds the committed API snapshot and the hand-written
// verdicts to each other, in both directions.
//
// The direction that is easy to see the need for: an endpoint Favro
// documents and this file does not mention is a gap nobody decided
// about. The direction that actually rots is the other one — a verdict
// for an endpoint that no longer exists reads, to anyone auditing
// coverage, exactly like a considered decision, and it is the residue
// of one that stopped being true.
//
// It runs offline, against whatever `gates api-diff` last fetched.
func apiCoverage(w io.Writer, _ []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	surface, err := readSurface(filepath.Join(root, filepath.FromSlash(apiSurfacePath)))
	if err != nil {
		return err
	}
	if len(surface.Endpoints) < minSurfaceEndpoints {
		return fmt.Errorf("%s holds %d endpoints, expected at least %d — refetch it with `make api-diff`",
			apiSurfacePath, len(surface.Endpoints), minSurfaceEndpoints)
	}

	rows, err := readCoverage(filepath.Join(root, filepath.FromSlash(apiCoveragePath)))
	if err != nil {
		return err
	}
	if len(rows) < minCoverageRows {
		return fmt.Errorf("%s holds %d verdicts, expected at least %d", apiCoveragePath, len(rows), minCoverageRows)
	}

	methods, err := clientMethods(filepath.Join(root, "internal", "favroapi"))
	if err != nil {
		return err
	}
	if len(methods) < minClientMethods {
		return fmt.Errorf("read %d client methods from internal/favroapi, expected at least %d", len(methods), minClientMethods)
	}

	problems := checkCoverage(surface.Endpoints, rows, methods)
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("the API surface and its verdicts disagree:\n  %s", strings.Join(problems, "\n  "))
	}

	implemented, out, covered := tally(surface.Endpoints, rows)
	_, err = fmt.Fprintf(w,
		"api-coverage ok: %d endpoints in %s (fetched %s), each with a verdict — %d implemented, %d written off; %d client methods named and all present\n",
		len(surface.Endpoints), apiSurfacePath, surface.FetchedAt, implemented, out, covered)
	return err
}

// checkCoverage returns every disagreement between the two files.
func checkCoverage(endpoints []apiEndpoint, rows []coverageRow, methods map[string]bool) []string {
	var problems []string

	used := make([]bool, len(rows))
	for _, e := range endpoints {
		var hits []int
		for i, r := range rows {
			if r.matches(e) {
				hits = append(hits, i)
				used[i] = true
			}
		}
		switch len(hits) {
		case 0:
			problems = append(problems, fmt.Sprintf(
				"%s %s is documented and has no verdict in %s — decide about it, even if the decision is `out`",
				e.Method, e.Path, apiCoveragePath))
		case 1:
		default:
			problems = append(problems, fmt.Sprintf(
				"%s %s matches %d verdict rows; one endpoint, one decision", e.Method, e.Path, len(hits)))
		}
	}

	for i, r := range rows {
		if !used[i] {
			problems = append(problems, fmt.Sprintf(
				"%s:%d: %s %s has a verdict and is not in the snapshot — Favro removed it, or it was never there",
				apiCoveragePath, r.line, r.method, r.path))
			continue
		}
		problems = append(problems, checkRow(r, methods)...)
	}
	return problems
}

// checkRow validates one verdict on its own terms.
func checkRow(r coverageRow, methods map[string]bool) []string {
	var problems []string
	switch r.verdict {
	case verdictImplemented:
		for _, name := range strings.Split(r.detail, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				problems = append(problems, fmt.Sprintf("%s:%d: %s %s is implemented by nothing named",
					apiCoveragePath, r.line, r.method, r.path))
				continue
			}
			if !methods[name] {
				problems = append(problems, fmt.Sprintf(
					"%s:%d: %s %s names %s, which is not a method on internal/favroapi.Client",
					apiCoveragePath, r.line, r.method, r.path, name))
			}
		}
	case verdictOut:
		if len(r.detail) < minOutReasonChars {
			problems = append(problems, fmt.Sprintf(
				"%s:%d: %s %s is written off in %d characters; say why, and why it would stay that way",
				apiCoveragePath, r.line, r.method, r.path, len(r.detail)))
		}
	default:
		problems = append(problems, fmt.Sprintf("%s:%d: unknown verdict %q; use %s or %s",
			apiCoveragePath, r.line, r.verdict, verdictImplemented, verdictOut))
	}
	return problems
}

// tally counts the verdicts, expanding the categorical rows.
func tally(endpoints []apiEndpoint, rows []coverageRow) (implemented, out, methods int) {
	named := map[string]bool{}
	for _, e := range endpoints {
		for _, r := range rows {
			if !r.matches(e) {
				continue
			}
			if r.verdict == verdictImplemented {
				implemented++
				for _, n := range strings.Split(r.detail, ",") {
					named[strings.TrimSpace(n)] = true
				}
			} else {
				out++
			}
			break
		}
	}
	return implemented, out, len(named)
}

func readSurface(path string) (apiSurface, error) {
	var s apiSurface
	body, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("%w (run `make api-diff` to create it)", err)
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// readCoverage parses the TSV, skipping comments and blank lines.
func readCoverage(path string) ([]coverageRow, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []coverageRow
	for i, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("%s:%d: got %d tab-separated fields, want 4 (METHOD PATH VERDICT DETAIL)",
				apiCoveragePath, i+1, len(fields))
		}
		rows = append(rows, coverageRow{
			method:  strings.TrimSpace(fields[0]),
			path:    strings.TrimSpace(fields[1]),
			verdict: strings.TrimSpace(fields[2]),
			detail:  strings.TrimSpace(fields[3]),
			line:    i + 1,
		})
	}
	return rows, nil
}

// clientMethods returns the names of every method on favroapi.Client,
// read from the source so that a verdict naming a method that was
// renamed or deleted fails here.
func clientMethods(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			t := fn.Recv.List[0].Type
			if star, ok := t.(*ast.StarExpr); ok {
				t = star.X
			}
			if id, ok := t.(*ast.Ident); ok && id.Name == "Client" {
				out[fn.Name.Name] = true
			}
		}
	}
	return out, nil
}
