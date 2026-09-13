package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// favroErrorsPath is internal/favro's error declarations, read for the
// test that requires Classify to have a case for each of them.
const favroErrorsPath = "../favro/errors.go"

// declaredErrorTypes returns the names of every struct type declared in
// internal/favro/errors.go.
func declaredErrorTypes(t *testing.T) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), favroErrorsPath, nil, 0)
	require.NoError(t, err)

	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := ts.Type.(*ast.StructType); !ok {
				continue
			}
			out = append(out, ts.Name.Name)
		}
	}
	return out
}
