package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// pinsShell reads depth: asking only whether both keys appear says yes
// to a `shell: bash` sitting beside `run:` rather than under it, which is
// `defaults.shell` — not a key GitHub has, so it pins nothing while
// reading as though it does.
func TestPinsShellReadsDepth(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want bool
	}{
		{"nested correctly", "defaults:\n  run:\n    shell: bash\n", true},
		{"quoted", "defaults:\n  run:\n    shell: \"bash\"\n", true},
		{"with a trailing comment", "defaults:\n  run:\n    shell: bash # windows\n", true},
		{"tab indented", "defaults:\n\trun:\n\t\tshell: bash\n", true},
		{"beside run, not under it", "defaults:\n  shell: bash\n  run:\n", false},
		{"another shell", "defaults:\n  run:\n    shell: pwsh\n", false},
		{"no defaults block", "jobs:\n  test:\n    steps: []\n", false},
		{"defaults not at column zero", "  defaults:\n    run:\n      shell: bash\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pinsShell(tc.yaml); got != tc.want {
				t.Errorf("pinsShell() = %v, want %v for:\n%s", got, tc.want, tc.yaml)
			}
		})
	}
}

func TestExactSemverRejectsWhatCanMoveUnderYou(t *testing.T) {
	for _, v := range []string{"v2.13.2", "2.13.2", "v8.30.1", "v1.0.0-rc.1"} {
		if !exactSemver.MatchString(v) {
			t.Errorf("%q is exactly one version and should be accepted", v)
		}
	}
	for _, v := range []string{"latest", "v2", "v2.13", ">=2.13.2", "^2.13.2", "stable"} {
		if exactSemver.MatchString(v) {
			t.Errorf("%q lets the tool change under a pinned action and should be refused", v)
		}
	}
}

// A step's pin is the version key in its own block, never one a later
// step carries.
func TestWorkflowPinsReadsOnlyTheStepsOwnBlock(t *testing.T) {
	const (
		module = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
		sha    = "0123456789abcdef0123456789abcdef01234567"
	)
	for _, tc := range []struct {
		name string
		yaml string
		want []string
	}{
		{
			"the step's own version is its pin",
			"jobs:\n  a:\n    steps:\n      - uses: golangci/golangci-lint-action@" + sha +
				"\n        with:\n          version: v1.0.0\n",
			[]string{"v1.0.0"},
		},
		{
			"the next step's version does not cover this one",
			"jobs:\n  a:\n    steps:\n      - uses: golangci/golangci-lint-action@" + sha +
				"\n      - uses: actions/setup-go@" + sha + "\n        with:\n          version: v9.9.9\n",
			nil,
		},
		{
			"a blank line above an unpinned installer lends it no later step's version",
			"jobs:\n  a:\n    steps:\n      - run: echo\n\n      - uses: golangci/golangci-lint-action@" + sha +
				"\n      - uses: actions/setup-go@" + sha + "\n        with:\n          version: v9.9.9\n",
			nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := workflowPins(tc.yaml, module); !slices.Equal(got, tc.want) {
				t.Errorf("workflowPins() = %q, want %q for:\n%s", got, tc.want, tc.yaml)
			}
		})
	}
}

// The gate against the repository, which is also the floor: a run that
// reads no workflows reports exactly what a clean run reports.
func TestPinsReadsTheWorkflows(t *testing.T) {
	var out sink
	if err := pins(&out, nil); err != nil {
		t.Fatalf("the workflows should be pinned: %v", err)
	}
	out.mustSay(t, "actions by SHA")
}

// Stdout carries only MCP JSON-RPC frames, an MCP spec MUST NOT that
// `forbidigo` enforces (D-45). Nothing else would notice the linter being
// dropped: the tree is clean, so removing the rule changes no output.
func TestForbidigoGuardsStdout(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(data)

	for _, want := range []string{
		"- forbidigo",    // enabled
		"analyze-types",  // resolves aliased imports and os.Stdout as a value
		`^fmt\.Print.*$`, // the function family
		`^os\.Stdout$`,   // and the destination, which is the half a Print matcher misses
	} {
		if !strings.Contains(config, want) {
			t.Errorf(".golangci.yml no longer carries %q; stdout is unguarded and nothing else "+
				"would fail to say so", want)
		}
	}
}

// pinCopy is one copy of a tool's pin: the file that holds it, and a
// pattern whose first group is everything on the line before the version.
type pinCopy struct {
	file    string
	pattern *regexp.Regexp
}

const (
	pinsCI      = ".github/workflows/ci.yml"
	pinsRelease = ".github/workflows/release.yml"
)

// The copies of the four tools the Makefile pins: one in the Makefile,
// and each a workflow holds.
var (
	mkGolangci  = pinCopy{"Makefile", regexp.MustCompile(`(?m)^(GOLANGCI_LINT\s*\?=\s*github\.com/golangci/golangci-lint/v2/cmd/golangci-lint@)\S+`)}
	mkVulncheck = pinCopy{"Makefile", regexp.MustCompile(`(?m)^(GOVULNCHECK\s*\?=\s*golang\.org/x/vuln/cmd/govulncheck@)\S+`)}
	mkLicenses  = pinCopy{"Makefile", regexp.MustCompile(`(?m)^(GOLICENSES\s*\?=\s*github\.com/google/go-licenses@)\S+`)}
	mkGitleaks  = pinCopy{"Makefile", regexp.MustCompile(`(?m)^(GITLEAKS\s*\?=\s*github\.com/zricethezav/gitleaks/v8@)\S+`)}
	ciGolangci  = pinCopy{pinsCI, golangciAction}
	relGolangci = pinCopy{pinsRelease, golangciAction}
	ciVulncheck = pinCopy{pinsCI, regexp.MustCompile(`(golang\.org/x/vuln/cmd/govulncheck@)\S+`)}
	ciLicenses  = pinCopy{pinsCI, regexp.MustCompile(`(github\.com/google/go-licenses@)\S+`)}
	ciGitleaks  = pinCopy{pinsCI, regexp.MustCompile(`(?m)^(\s+GITLEAKS_VERSION:\s*)\S+`)}
)

// golangciAction is the version input of a golangci-lint-action step.
var golangciAction = regexp.MustCompile(`(golangci/golangci-lint-action@[^\n]*\n\s+with:\r?\n\s+version:\s*)\S+`)

// set writes version into the one place the copy is, and fails the test
// when the file holds it anywhere else or nowhere.
func (c pinCopy) set(t *testing.T, files map[string]string, version string) {
	t.Helper()
	if n := len(c.pattern.FindAllStringIndex(files[c.file], -1)); n != 1 {
		t.Fatalf("%s matches %s %d times, want once", c.pattern, c.file, n)
	}
	files[c.file] = c.pattern.ReplaceAllString(files[c.file], "${1}"+version)
}

// Every module@version the Makefile pins is held to each copy a
// workflow holds, run at module@version or named by its action's input
// or variable. Every row starts with every copy at v1.0.0, and drifts one
// copy per tool or breaks one rule.
func TestPinsHoldTheMakefileToCI(t *testing.T) {
	t.Parallel()
	const drift = "; make check runs one and CI the other"
	edit := func(file, from, to string) func(*testing.T, map[string]string) {
		return func(t *testing.T, f map[string]string) {
			if !strings.Contains(f[file], from) {
				t.Fatalf("%s does not contain %q", file, from)
			}
			f[file] = strings.Replace(f[file], from, to, 1)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(t *testing.T, files map[string]string)
		want string
	}{
		{"every copy agrees", func(*testing.T, map[string]string) {}, ""},
		{
			"golangci-lint moved in the Makefile", func(t *testing.T, f map[string]string) { mkGolangci.set(t, f, "v1.0.1") },
			"golangci-lint is v1.0.1 in the Makefile's GOLANGCI_LINT and v1.0.0 in ci.yml" + drift + "\n" +
				"golangci-lint is v1.0.1 in the Makefile's GOLANGCI_LINT and v1.0.0 in release.yml" + drift,
		},
		{
			"golangci-lint moved in the CI workflow", func(t *testing.T, f map[string]string) { ciGolangci.set(t, f, "v1.0.1") },
			"golangci-lint is v1.0.0 in the Makefile's GOLANGCI_LINT and v1.0.1 in ci.yml" + drift,
		},
		{
			"golangci-lint moved in the release workflow", func(t *testing.T, f map[string]string) { relGolangci.set(t, f, "v1.0.1") },
			"golangci-lint is v1.0.0 in the Makefile's GOLANGCI_LINT and v1.0.1 in release.yml" + drift,
		},
		{
			"govulncheck moved in the Makefile", func(t *testing.T, f map[string]string) { mkVulncheck.set(t, f, "v1.0.1") },
			"govulncheck is v1.0.1 in the Makefile's GOVULNCHECK and v1.0.0 in ci.yml" + drift,
		},
		{
			"govulncheck moved in CI", func(t *testing.T, f map[string]string) { ciVulncheck.set(t, f, "v1.0.1") },
			"govulncheck is v1.0.0 in the Makefile's GOVULNCHECK and v1.0.1 in ci.yml" + drift,
		},
		{
			"go-licenses moved in the Makefile", func(t *testing.T, f map[string]string) { mkLicenses.set(t, f, "v1.0.1") },
			"go-licenses is v1.0.1 in the Makefile's GOLICENSES and v1.0.0 in ci.yml" + drift,
		},
		{
			"go-licenses moved in CI", func(t *testing.T, f map[string]string) { ciLicenses.set(t, f, "v1.0.1") },
			"go-licenses is v1.0.0 in the Makefile's GOLICENSES and v1.0.1 in ci.yml" + drift,
		},
		{
			"gitleaks moved in the Makefile", func(t *testing.T, f map[string]string) { mkGitleaks.set(t, f, "v1.0.1") },
			"gitleaks is v1.0.1 in the Makefile's GITLEAKS and v1.0.0 in ci.yml" + drift,
		},
		{
			"gitleaks moved in CI", func(t *testing.T, f map[string]string) { ciGitleaks.set(t, f, "1.0.1") },
			"gitleaks is v1.0.0 in the Makefile's GITLEAKS and 1.0.1 in ci.yml" + drift,
		},
		{"a leading v on one copy only", func(t *testing.T, f map[string]string) { ciVulncheck.set(t, f, "1.0.0") }, ""},
		{
			"the last of two assignments is the one make runs",
			edit("Makefile", "GOVULNCHECK   ?=", "GOVULNCHECK ?= golang.org/x/vuln/cmd/govulncheck@v1.0.1\nGOVULNCHECK   ?="), "",
		},
		{
			"an earlier assignment does not hide the last",
			edit("Makefile", "\nGOLICENSES", "\nGOVULNCHECK ?= golang.org/x/vuln/cmd/govulncheck@v1.0.1\nGOLICENSES"),
			"govulncheck is v1.0.1 in the Makefile's GOVULNCHECK and v1.0.0 in ci.yml" + drift,
		},
		{
			"the Makefile runs a tool at a literal",
			edit("Makefile", "$(GOVULNCHECK) ./...", "golang.org/x/vuln/cmd/govulncheck@v1.0.0 ./..."),
			"the Makefile sets GOVULNCHECK and never runs $(GOVULNCHECK)",
		},
		{
			"a Makefile pin no workflow makes",
			edit("Makefile", "\nGOLICENSES", "\nSTATICCHECK ?= honnef.co/go/tools/cmd/staticcheck@v1.0.0 # lint\nGOLICENSES"),
			"the Makefile sets STATICCHECK and never runs $(STATICCHECK)\n" +
				"no workflow pins staticcheck, which the Makefile runs at $(STATICCHECK)",
		},
		{"CI runs no copy", func(t *testing.T, f map[string]string) {
			f[pinsCI] = ciLicenses.pattern.ReplaceAllString(f[pinsCI], "go-licenses-gone ")
		}, "no workflow pins go-licenses, which the Makefile runs at $(GOLICENSES)"},
		{"a Makefile with fewer pins than the floor", func(t *testing.T, f map[string]string) {
			for _, c := range []pinCopy{mkGolangci, mkVulncheck, mkLicenses} {
				f["Makefile"] = c.pattern.ReplaceAllString(f["Makefile"], "")
			}
		}, "only 1 module@version pins read from the Makefile, fewer than 2; the check is not reading them"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := pinsFiles(t)
			for _, c := range []pinCopy{mkGolangci, mkVulncheck, mkLicenses, mkGitleaks, ciGolangci, relGolangci, ciVulncheck, ciLicenses, ciGitleaks} {
				c.set(t, files, "v1.0.0")
			}
			tc.edit(t, files)
			root := t.TempDir()
			putAll(t, root, files)
			if got := errorText(pinsAt(root, io.Discard)); got != tc.want {
				t.Errorf("pins =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// pinsFiles is the repository's copy of every file the pins gate reads.
func pinsFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, rel := range []string{"go.mod", "Makefile"} {
		files[rel] = string(repoFile(t, rel))
	}
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), ".github", "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		rel := ".github/workflows/" + e.Name()
		files[rel] = string(repoFile(t, rel))
	}
	return files
}

// errorText is the error's text, empty for none.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
