package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestOutcomesAgainstThisRepository is the gate against the surface it
// guards, which is the run that matters.
func TestOutcomesAgainstThisRepository(t *testing.T) {
	var out sink
	if err := outcomes(&out, nil); err != nil {
		t.Fatalf("a tool states an outcome the response did not carry: %v", err)
	}
	out.mustSay(t, "boolean request fields")
	out.mustSay(t, "branch(es) testing one")
	// The floors are the point of this gate, so the report has to show
	// its working: "found nothing" and "looked at nothing" are the same
	// sentence otherwise.
	out.mustSay(t, "internal/tools")
}

// TestOutcomesCatchesAnAssertedOutcome is the gate against the shape it
// exists for. A check that has never been seen to fail is a check
// nobody knows the meaning of.
func TestOutcomesCatchesAnAssertedOutcome(t *testing.T) {
	t.Parallel()

	const src = `package tools

type deleteThingInput struct {
	Everywhere bool
}

func register(r *resolver) {
	addTool(func(in deleteThingInput) string {
		if in.Everywhere {
			return "the thing has been removed from every widget"
		}
		return "ok"
	})
}
`
	claims, branches := claimsIn(t, src)
	if branches != 1 {
		t.Fatalf("branches = %d, want %d", branches, 1)
	}
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want %d", len(claims), 1)
	}
	if got := claims[0].field; got != "Everywhere" {
		t.Errorf("claims[0].field = %v, want %v", got, "Everywhere")
	}
	if !strings.Contains(claims[0].quote, "has been removed") {
		t.Errorf("claims[0].quote = %q, want it to carry the sentence", claims[0].quote)
	}
}

// TestOutcomesAcceptsTheHonestForms pins the three shapes that are not
// findings, each for its own reason. Without this the gate could be
// made to pass by flagging nothing, and nothing would notice.
func TestOutcomesAcceptsTheHonestForms(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
	}{
		{
			// States the request, not the world. This is the wording
			// the gate is asking a flagged branch to move to, so it
			// must not itself be a finding.
			name: "hypothetical",
			body: `return "would delete the thing from every widget — irreversible"`,
		},
		{
			// Asks Favro before it speaks.
			name: "consults Favro",
			body: `got, _ := r.Client().DeleteThing(ctx); return "the thing is now " + got.State`,
		},
		{
			// Refuses. A refusal states nothing about the world.
			name: "refuses",
			body: `return fmt.Errorf("everywhere is not supported for this thing")`,
		},
		{
			name: "enters the dry-run context",
			body: `writeCtx = favroapi.WithDryRun(ctx); return "the thing has been removed everywhere"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := `package tools

type deleteThingInput struct {
	Everywhere bool
}

func register(r *resolver) {
	addTool(func(in deleteThingInput) string {
		if in.Everywhere {
			` + tc.body + `
		}
		return "ok"
	})
}
`
			claims, branches := claimsIn(t, src)
			if branches != 1 {
				t.Fatalf("branches = %d, want %d — the gate stopped recognising the branch", branches, 1)
			}
			if len(claims) != 0 {
				t.Errorf("claims = %d, want 0: %q", len(claims), claims[0].quote)
			}
		})
	}
}

// TestOutcomesReadsAClosureThatCapturesTheRequest is the one way this
// repository differs from the sibling the gate came from. Handlers here
// are literals passed to addTool, and a state-diff closure takes no
// parameters at all — it reads the handler's `in` from the enclosing
// scope. A per-function walk, which is what the sibling does, sees
// nothing here.
func TestOutcomesReadsAClosureThatCapturesTheRequest(t *testing.T) {
	t.Parallel()

	const src = `package tools

type deleteThingInput struct {
	Everywhere bool
}

func register(r *resolver) {
	addTool(func(in deleteThingInput) string {
		return runWrite(
			func() string {
				if in.Everywhere {
					return "the thing has been removed from every widget"
				}
				return "ok"
			},
		)
	})
}
`
	claims, branches := claimsIn(t, src)
	if branches != 1 {
		t.Fatalf("branches = %d, want %d — a closure capturing the request was not read", branches, 1)
	}
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want %d", len(claims), 1)
	}
}

// TestOutcomeClaimsRowsAreWellFormed holds the record file to its shape:
// three columns, and a reason that says something.
func TestOutcomeClaimsRowsAreWellFormed(t *testing.T) {
	t.Parallel()

	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := readOutcomeClaims(root + "/" + outcomeClaimsFile)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for key, reason := range rows {
		if len(strings.Fields(reason)) < 4 {
			t.Errorf("%s: the reason is %q, which is not an argument anybody can check", key, reason)
		}
	}
}

// claimsIn runs the detector over one source file.
func claimsIn(t *testing.T, src string) ([]outcomeClaim, int) {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "tools_x.go", src, 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	files := []outcomeFile{{path: "internal/tools/tools_x.go", file: parsed}}
	return outcomeClaims(files, boolInputFields(files), fset)
}
