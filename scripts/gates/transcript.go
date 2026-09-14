package main

import (
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

// transcriptDriverDir is the live driver the gate reads.
const transcriptDriverDir = "scripts/livefavro"

// transcriptPrinters are the functions allowed to format output. They
// are the redactor's own methods.
var transcriptPrinters = map[string]bool{
	"printer.line":   true,
	"printer.result": true,
}

// transcriptNamesTheTerminal are the only functions allowed to mention
// os.Stdout or os.Stderr at all.
//
// This is the rule that actually holds, and the first version of this
// gate did not have it. That version looked for writes to os.Stdout and
// found one, because the driver wraps the terminal in a bufio.Writer
// once and every print goes through the field — which a syntactic rule
// cannot follow. Checking who may *name* the terminal needs no
// dataflow: if only the printer's constructor can reach it, everything
// else has to go through the printer, and the printer redacts.
var transcriptNamesTheTerminal = map[string]bool{
	"newPrinter": true, // wraps stdout in the one buffered writer
	"fatal":      true, // the single unredacted exit path, by design
	"start":      true, // hands the child process its environment
}

// transcriptFloors: a parser that matched nothing prints the same
// cheerful sentence as one that matched everything.
const (
	minTranscriptFiles    = 1
	minTranscriptMentions = 2
)

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
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, filepath.FromSlash(transcriptDriverDir))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%w (the live driver is %s)", err, transcriptDriverDir)
	}

	var problems []string
	files, writes, mentions := 0, 0, 0
	fset := token.NewFileSet()

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		files++
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			return err
		}

		for _, decl := range file.Decls {
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
								"%s/%s: %s calls %s; print through the Redactor instead (%s)",
								transcriptDriverDir, e.Name(), where, target,
								strings.Join(sortedKeys(transcriptPrinters), ", ")))
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
						"%s/%s: %s names %s; only %s may, so that everything else has to go through the redactor",
						transcriptDriverDir, e.Name(), where, render(sel),
						strings.Join(sortedKeys(transcriptNamesTheTerminal), ", ")))
				}
				return true
			})
		}
	}

	if files < minTranscriptFiles {
		return fmt.Errorf("read %d Go files in %s; that is not the driver", files, transcriptDriverDir)
	}
	if mentions < minTranscriptMentions {
		return fmt.Errorf("found %d mentions of os.Stdout/os.Stderr in %s, expected at least %d — this gate is not recognising them, which is worse than finding none",
			mentions, transcriptDriverDir, minTranscriptMentions)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("the live driver prints without redacting:\n  %s", strings.Join(problems, "\n  "))
	}

	_, err = fmt.Fprintf(w,
		"transcript ok: %d files in %s, %d formatted writes and %d mentions of the terminal, each in a function allowed to have one\n",
		files, transcriptDriverDir, writes, mentions)
	return err
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
