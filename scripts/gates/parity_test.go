package main

import (
	"strings"
	"testing"
)

// A Makefile and a workflow that agree, in the shape this repository's
// do: some prerequisites run a gate, some run a pinned tool through
// `go run`, some run plain go commands, and CI satisfies the tool ones
// with an action rather than a `run:` step.
const goodMakefile = `GO        ?= go
GOLANGCI_LINT ?= github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
GOVULNCHECK   ?= golang.org/x/vuln/cmd/govulncheck@v1.8.0
GITLEAKS      ?= github.com/zricethezav/gitleaks/v8@v8.30.1

fmt-check:
	@$(GO) run ./scripts/gates fmt-check

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy -diff

lint:
	$(GO) run $(GOLANGCI_LINT) run ./...

vuln:
	$(GO) run $(GOVULNCHECK) ./...

secrets:
	$(GO) run $(GITLEAKS) dir . --config .gitleaks.toml --no-banner

cover:
	@$(GO) run ./scripts/gates coverage coverage.out $(COVER_MIN)

leaks:
	@$(GO) run ./scripts/gates leaks

pins:
	@$(GO) run ./scripts/gates pins

parity:
	@$(GO) run ./scripts/gates parity

smoke:
	@$(GO) run ./scripts/gates smoke $(BIN)

check: fmt-check vet tidy lint cover vuln secrets leaks pins parity smoke
`

const goodCI = `jobs:
  gates:
    steps:
      - run: go run ./scripts/gates coverage coverage.out 80
      - run: go run ./scripts/gates leaks
      - run: go run ./scripts/gates pins
      - run: go run ./scripts/gates parity
      - run: go run ./scripts/gates smoke bin/favro-mcp
  static:
    steps:
      - run: go run ./scripts/gates fmt-check
      - run: go vet ./...
      - run: go mod tidy -diff
      - uses: golangci/golangci-lint-action@ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a # v9.3.0
      - run: go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
  secrets:
    steps:
      - uses: gitleaks/gitleaks-action@e0c47f4f8be36e29cdc102c57e68cb5cbf0e8d1e # v3.0.0
`

var declaredForTest = []string{"coverage", "leaks", "parity", "pins", "smoke"}

func TestParityAcceptsTwoListsThatAgree(t *testing.T) {
	problems, vets, err := parityCheck(goodMakefile, goodCI, declaredForTest)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Errorf("these two agree; got %v", problems)
	}
	if vets != 0 {
		t.Errorf("no tagged vet passes here, got %d", vets)
	}
}

func TestParityCatchesEachDivergence(t *testing.T) {
	for _, tc := range []struct {
		name, makefile, ci, want string
	}{
		{
			name:     "a gate CI does not run",
			makefile: goodMakefile,
			ci:       strings.Replace(goodCI, "      - run: go run ./scripts/gates leaks\n", "", 1),
			want:     "nothing in ci.yml does",
		},
		{
			name:     "a gate commented out of CI",
			makefile: goodMakefile,
			ci:       strings.Replace(goodCI, "      - run: go run ./scripts/gates pins", "      # - run: go run ./scripts/gates pins", 1),
			want:     "nothing in ci.yml does",
		},
		{
			// The hole this gate had while it read only the registry:
			// seven of `check`'s fifteen prerequisites were not gates, so
			// deleting the vulnerability scan from CI left it green.
			name:     "a non-gate step deleted from CI",
			makefile: goodMakefile,
			ci:       strings.Replace(goodCI, "      - run: go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...\n", "", 1),
			want:     "`make vuln` runs \"govulncheck\"",
		},
		{
			name:     "an action removed from CI",
			makefile: goodMakefile,
			ci:       strings.Replace(goodCI, "      - uses: gitleaks/gitleaks-action@e0c47f4f8be36e29cdc102c57e68cb5cbf0e8d1e # v3.0.0\n", "", 1),
			want:     "`make secrets` runs \"gitleaks\"",
		},
		{
			name:     "a gate `make check` does not depend on",
			makefile: strings.Replace(goodMakefile, "parity smoke\n", "parity\n", 1),
			ci:       goodCI,
			want:     "`make check` does not depend on it",
		},
		{
			name:     "a prerequisite with no recipe at all",
			makefile: goodMakefile + "\nplaceholder:\n",
			ci:       goodCI,
			want:     "gives it no recipe",
		},
		{
			name:     "a tagged vet pass that only runs locally",
			makefile: strings.Replace(goodMakefile, "\t$(GO) vet ./...\n", "\t$(GO) vet ./...\n\t$(GO) vet -tags=live ./...\n", 1),
			ci:       goodCI,
			want:     "compile only on a maintainer's machine",
		},
		{
			name:     "a tagged vet pass a contributor cannot reproduce",
			makefile: goodMakefile,
			ci:       goodCI + "      - run: go vet -tags=live ./...\n",
			want:     "a contributor cannot reproduce it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			makefile := tc.makefile
			if tc.name == "a prerequisite with no recipe at all" {
				makefile = strings.Replace(makefile, "parity smoke\n", "parity smoke placeholder\n", 1)
			}
			problems, _, err := parityCheck(makefile, tc.ci, declaredForTest)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Errorf("problems = %v, want one mentioning %q", problems, tc.want)
			}
		})
	}
}

// The floors: a comparison of two empty lists passes for the wrong
// reason, and so does one where the Makefile's shape has changed under
// the parser.
func TestParityRefusesToCompareNothing(t *testing.T) {
	if _, _, err := parityCheck(goodMakefile, goodCI, []string{"leaks"}); err == nil {
		t.Error("one declared gate is not a registry; the check should refuse")
	}
	if _, _, err := parityCheck("all: build\n", goodCI, declaredForTest); err == nil {
		t.Error("a Makefile with no check target should fail loudly")
	}
	if _, _, err := parityCheck("check: fmt\n", goodCI, declaredForTest); err == nil {
		t.Error("a check target with one prerequisite is not the whole list")
	}
	// Every prerequisite present but every recipe unreadable: the
	// signatures would all come back empty and every comparison would
	// trivially pass.
	bare := "check: a b c d e f g h i j k\n"
	if _, _, err := parityCheck(bare, goodCI, declaredForTest); err == nil {
		t.Error("prerequisites with no recipes mean nothing was compared; the check should refuse")
	}
}

// signature is what lets the two files spell the same job differently.
func TestSignatureNamesTheJob(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"go run ./scripts/gates leaks", "./scripts/gates leaks"},
		{"go run ./scripts/gates coverage coverage.out 80", "./scripts/gates coverage"},
		{"go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run ./...", "golangci-lint"},
		{"go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...", "govulncheck"},
		{"go run github.com/zricethezav/gitleaks/v8@v8.30.1 dir .", "gitleaks"},
		{"go vet ./...", "go vet ./..."},
		{"go mod tidy -diff", "go mod tidy"},
		{"", ""},
	} {
		if got := signature(tc.line); got != tc.want {
			t.Errorf("signature(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// A recipe spelled with variables has to compare equal to a workflow
// that spells the tool out.
func TestRecipeExpandsVariables(t *testing.T) {
	vars := makeVars(goodMakefile)
	recipe := recipeOf(goodMakefile, "secrets", vars)
	if len(recipe) != 1 {
		t.Fatalf("recipe = %v, want one line", recipe)
	}
	if !strings.Contains(recipe[0], "gitleaks/v8@v8.30.1") {
		t.Errorf("the variable was not expanded: %q", recipe[0])
	}
	if got := recipeOf(goodMakefile, "nosuchtarget", vars); got != nil {
		t.Errorf("an absent target has no recipe, got %v", got)
	}
}

// A multi-line `run: |` block is where a naive reader stops seeing the
// commands CI plainly runs.
func TestRunLinesReadsBlockScalars(t *testing.T) {
	ci := "jobs:\n  x:\n    steps:\n      - name: gates\n        run: |\n          go run ./scripts/gates leaks\n          go run ./scripts/gates pins\n      - uses: actions/checkout@sha\n"
	got := runLines(ci)
	for _, want := range []string{"gates leaks", "gates pins"} {
		if !strings.Contains(got, want) {
			t.Errorf("runLines() dropped %q from a block scalar:\n%s", want, got)
		}
	}
	if strings.Contains(got, "actions/checkout") {
		t.Errorf("runLines() kept a step past the end of the block:\n%s", got)
	}
}

// The registry against the repository: every gate really is in both, and
// every other prerequisite of `check` really is in CI.
func TestParityAgainstThisRepository(t *testing.T) {
	var out sink
	if err := parity(&out, nil); err != nil {
		t.Fatalf("`make check` and ci.yml should agree: %v", err)
	}
	out.mustSay(t, "gates in both")
}
