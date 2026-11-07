package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// transcriptDriver is a program that points a live session at a real
// organization and prints what came back: its directory, and the Makefile
// target that runs it, whose `go run` sets the tags it builds with.
type transcriptDriver struct {
	dir, target string
}

// transcriptDrivers are the drivers the gate reads.
//
// A failing eval prints its task's whole trace, live ids the model
// resolved included, so a gate that reads only one driver passes while
// the other leaks. A rule scoped to one path is a rule about that path.
// Adding a driver means adding it here.
var transcriptDrivers = []transcriptDriver{
	{dir: "scripts/livefavro", target: "live"},
	{dir: "scripts/evals", target: "evals"},
}

// transcriptTags is each tag list the Makefile's target for d runs d
// with, one per `go run` of it, read as parity reads a vet pass's. The
// tags are the target's, so a tag added there cannot build a file the
// gate skips.
func transcriptTags(mk *makefile, d transcriptDriver) ([][]string, error) {
	var lists [][]string
	for _, line := range mk.recipe(d.target) {
		for _, c := range goCommands(line) {
			if rest := goAfterFlags(c.args); c.sub == "run" && len(rest) > 0 && path.Clean(rest[0]) == d.dir {
				lists = append(lists, goBuildTags(c.args))
			}
		}
	}
	if len(lists) == 0 {
		return nil, fmt.Errorf("%s: %s runs no `go run` of %s, so the gate cannot tell which of its files the driver builds",
			makefilePath, d.target, d.dir)
	}
	return lists, nil
}

// transcriptPrinters are the functions allowed to format output. They
// are the redactor's own methods.
var transcriptPrinters = map[string]bool{
	"printer.line":     true,
	"printer.result":   true,
	"printer.fail":     true,
	"printer.redacted": true,
}

// transcriptNamesTheTerminal are the only functions allowed to mention
// os.Stdout or os.Stderr at all.
//
// This is the rule that actually holds. A check for writes to os.Stdout
// finds only one, because the driver wraps the terminal in a bufio.Writer
// once and every print goes through the field, which a syntactic rule
// cannot follow. Checking who may *name* the terminal needs no
// dataflow: if only the printer's constructor can reach it, everything
// else has to go through the printer, and the printer redacts.
var transcriptNamesTheTerminal = map[string]bool{
	"newPrinter": true, // wraps stdout in the one buffered writer
	"fatal":      true, // the single unredacted exit path, by design
	"start":      true, // hands the child process its environment
}

// transcriptMentionsFloor is the fewest mentions of the terminal the gate
// may read: a parser that matched nothing prints the same cheerful
// sentence as one that matched everything.
const transcriptMentionsFloor = 4

// transcript fails if anything in the live driver reaches the terminal
// other than through the one redacting helper.
//
// Without it, redaction is a list of call sites somebody remembered to
// route, and the next print added while debugging looks exactly like
// the safe ones beside it. The standard records it catching a section
// header printing directly in one repository, one refactor away from
// carrying a document title with it — and this driver talks to a real
// organization, so what it would carry is a card name.
func transcript(w io.Writer, _ []string) error {
	listed, err := docsFiles(".")
	if err != nil {
		return couldNotRun{err}
	}
	mk, err := readMakefile()
	if err != nil {
		return couldNotRun{err}
	}
	var problems, dirs []string
	read, writes, mentions := 0, 0, 0
	fset := token.NewFileSet()
	for _, d := range transcriptDrivers {
		dirs = append(dirs, d.dir)
		tags, err := transcriptTags(mk, d)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		files, _, unparsed := transcriptFiles(fset, listed, d.dir, tags)
		problems = append(problems, unparsed...)
		if len(files) == 0 {
			problems = append(problems, fmt.Sprintf("read no Go file of %s that its build compiles; "+
				"the drivers table names a directory or a tag that is gone", d.dir))
		}
		for _, f := range files {
			wr, m, p := transcriptScan(f.path, f.ast)
			writes, mentions = writes+wr, mentions+m
			problems = append(problems, p...)
		}
		read += len(files)
	}
	problems = append(problems, floorProblems("mentions of os.Stdout and os.Stderr", mentions, transcriptMentionsFloor)...)
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("a live driver prints without redacting:\n  %s", strings.Join(problems, "\n  "))
	}

	_, err = fmt.Fprintf(w,
		"transcript ok: %d files across %s, %d formatted writes and %d mentions of the terminal, each in a function allowed to have one\n",
		read, strings.Join(dirs, " and "), writes, mentions)
	return err
}

// transcriptFile is one Go file of a driver, by its slash path, parsed.
type transcriptFile struct {
	path string
	ast  *ast.File
}

// transcriptFiles is the Go files under dir among files, as git lists the
// checkout: those a build with one of tags compiles, parsed, and those
// none does, which reach no organization, such as the stub that says how
// to run the real thing. A file that does not parse is a problem.
func transcriptFiles(fset *token.FileSet, files []string, dir string, tags [][]string) (read []transcriptFile, skipped, problems []string) {
	for _, file := range files {
		if !strings.HasSuffix(file, ".go") || parityGoIgnores(file) || !strings.HasPrefix(file, dir+"/") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.FromSlash(file), nil, parser.ParseComments)
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s: %v", file, err))
		case slices.ContainsFunc(tags, func(t []string) bool { return transcriptBuilds(f, t) }):
			read = append(read, transcriptFile{file, f})
		default:
			skipped = append(skipped, file)
		}
	}
	return read, skipped, problems
}

// transcriptBuilds reports whether a driver built with tags compiles a
// file: its //go:build line holds with those tags set, any other unset,
// for some value of the tags the go command sets. A line that does not
// parse fails the build, so the file is read.
func transcriptBuilds(f *ast.File, tags []string) bool {
	expr := parityGoBuildExpr(f)
	return expr == nil || parityBuildsWith(expr, parityFreeTags(expr), tags)
}

// transcriptScan reads one driver file and reports what it found.
func transcriptScan(file string, f *ast.File) (writes, mentions int, problems []string) {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		where := funcLabel(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			// Who formats output.
			if call, ok := n.(*ast.CallExpr); ok {
				if target, isWrite := terminalWrite(call); isWrite {
					writes++
					if !transcriptPrinters[where] && where != "fatal" {
						problems = append(problems, fmt.Sprintf(
							"%s: %s calls %s; print through the Redactor instead (%s)",
							file, where, target, strings.Join(sortedKeys(transcriptPrinters), ", ")))
					}
				}
				return true
			}
			// Who may name the terminal.
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || !isStdStream(sel) {
				return true
			}
			mentions++
			if !transcriptNamesTheTerminal[where] {
				problems = append(problems, fmt.Sprintf(
					"%s: %s names %s; only %s may, so that everything else has to go through the redactor",
					file, where, render(sel), strings.Join(sortedKeys(transcriptNamesTheTerminal), ", ")))
			}
			return true
		})
	}
	return writes, mentions, problems
}

// terminalWrite reports whether a call writes to stdout or stderr, and
// what it was.
func terminalWrite(call *ast.CallExpr) (string, bool) {
	// print / println, the builtins nobody means to leave in.
	if id, ok := call.Fun.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") {
		return id.Name, true
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		// os.Stdout.Write(…) and friends: the receiver is itself a
		// selector.
		if inner, ok := sel.X.(*ast.SelectorExpr); ok && isStdStream(inner) {
			return render(sel), true
		}
		return "", false
	}

	switch pkg.Name {
	case "log":
		return "log." + sel.Sel.Name, true
	case "fmt":
		switch sel.Sel.Name {
		case "Print", "Printf", "Println":
			return "fmt." + sel.Sel.Name, true
		case "Fprint", "Fprintf", "Fprintln":
			if len(call.Args) > 0 {
				if dst, ok := call.Args[0].(*ast.SelectorExpr); ok && isStdStream(dst) {
					return "fmt." + sel.Sel.Name + " to " + render(dst), true
				}
			}
		}
	}
	return "", false
}

// isStdStream reports whether an expression is os.Stdout or os.Stderr.
func isStdStream(sel *ast.SelectorExpr) bool {
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}

// render prints a selector back as source, for the message.
func render(sel *ast.SelectorExpr) string {
	if pkg, ok := sel.X.(*ast.Ident); ok {
		return pkg.Name + "." + sel.Sel.Name
	}
	if inner, ok := sel.X.(*ast.SelectorExpr); ok {
		return render(inner) + "." + sel.Sel.Name
	}
	return sel.Sel.Name
}

// funcLabel names a function the way transcriptAllowed does:
// "receiver.Method" or "Function".
func funcLabel(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
