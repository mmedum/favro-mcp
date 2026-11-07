package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The reader takes each key from the step that owns it, in any order, and
// each command line from its run: block, in either scalar style.
func TestWorkflowReaderReadsTheBlockStyle(t *testing.T) {
	w := parseWorkflow("w.yml", strings.ReplaceAll(textLines(
		"env:",
		"  TOP_VERSION: 1.0.0",
		"jobs:",
		"  build:",
		"    env:",
		"      JOB: x",
		"    container:",
		"      image: node@sha256:1",
		"    steps:",
		"      - name: first",
		"        with:",
		"          version: v1.2.3 # the tool",
		"        env:",
		"          STEP: y",
		`        uses: 'a/b@c' # v1`,
		"      - run: echo one",
		"      - with:",
		"          subject-path: |",
		"            dist/*.zip,",
		"            dist/*.mcpb",
		"        uses: d/e@f",
		"      - run: |",
		"          # a comment",
		"          go run x \\",
		"            ./...",
		"          echo two",
		"        env:",
		"          RUN_ENV: z",
		"      - run: >-",
		"          go vet",
		"          ./...",
		"  call:",
		"    uses: o/r/.github/workflows/x.yml@g",
		"on:",
		"  push:",
		"    branches:",
		"      - main",
		"      - 'release/**' # a comment",
		"    paths: [go.mod, go.sum]",
	), "\n", "\r\n"))
	var got []string
	for _, s := range w.steps {
		got = append(got, fmt.Sprintf("%s:%d %s #%s with=%v env=%v", s.job, s.line, s.uses, s.comment, s.with, s.env))
	}
	want := []string{
		"build:15 a/b@c #v1 with=map[version:{version v1.2.3 12}] env=map[STEP:{STEP y 14}]",
		"build:21 d/e@f # with=map[subject-path:{subject-path dist/*.zip,\ndist/*.mcpb 18}] env=map[]",
		"call:33 o/r/.github/workflows/x.yml@g # with=map[] env=map[]",
	}
	if !slices.Equal(got, want) {
		t.Errorf("steps:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	got = nil
	for _, r := range w.runs {
		got = append(got, fmt.Sprintf("%s:%d %s", r.job, r.line, r.text))
	}
	want = []string{"build:16 echo one", "build:24 go run x ./...", "build:26 echo two", "build:30 go vet ./..."}
	if !slices.Equal(got, want) {
		t.Errorf("runs = %q, want %q", got, want)
	}
	got = nil
	for _, k := range w.env {
		got = append(got, fmt.Sprintf("%s=%s:%d", k.key, k.value, k.line))
	}
	if want := []string{"TOP_VERSION=1.0.0:2", "JOB=x:6", "STEP=y:14", "RUN_ENV=z:28"}; !slices.Equal(got, want) {
		t.Errorf("env = %q, want %q", got, want)
	}
	if len(w.images) != 1 || w.images[0].value != "node@sha256:1" || w.images[0].what != "the container" || w.images[0].job != "build" {
		t.Errorf("images = %+v", w.images)
	}
	if w.refused != nil {
		t.Errorf("refused = %q", w.refused)
	}
}

// A sequence may sit at its key's own column, the dash counted as
// indentation (https://yaml.org/spec/1.2.2/#823-block-nodes), and its
// items are read as if they were indented under it.
func TestWorkflowReaderReadsASequenceAtItsKeysColumn(t *testing.T) {
	w := parseWorkflow("w.yml", textLines(
		"jobs:",
		"  build:",
		"    steps:",
		"    - uses: a/b@c # v1",
		"      with:",
		"        version: v1.2.3",
		"    - run: echo one",
		"    - run: |",
		"        go vet ./...",
		"      env:",
		"        K: v",
		"    runs-on: x",
		"  next:",
		"    steps:",
		"    - run: echo two",
	))
	var got []string
	for _, s := range w.steps {
		got = append(got, fmt.Sprintf("%s:%d %s #%s with=%v env=%v", s.job, s.line, s.uses, s.comment, s.with, s.env))
	}
	if want := []string{"build:4 a/b@c #v1 with=map[version:{version v1.2.3 6}] env=map[]"}; !slices.Equal(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
	got = nil
	for _, r := range w.runs {
		got = append(got, fmt.Sprintf("%s:%d %s", r.job, r.line, r.text))
	}
	if want := []string{"build:7 echo one", "build:9 go vet ./...", "next:15 echo two"}; !slices.Equal(got, want) {
		t.Errorf("runs = %q, want %q", got, want)
	}
	got = nil
	for _, k := range w.env {
		got = append(got, fmt.Sprintf("%s=%s:%d", k.key, k.value, k.line))
	}
	if want := []string{"K=v:11"}; !slices.Equal(got, want) {
		t.Errorf("env = %q, want %q", got, want)
	}
	if w.refused != nil {
		t.Errorf("refused = %q", w.refused)
	}
}

// A shape the reader does not read is refused by name, never read as
// nothing.
func TestWorkflowReaderRefusesWhatItCannotRead(t *testing.T) {
	w := parseWorkflow("w.yml", textLines(
		"jobs:",
		"  build:",
		"    services:",
		"      db: {image: postgres}",
		"    steps:",
		"      - uses: a/b@c",
		"        with: {version: v1}",
		"      -",
		"        uses: d/e@f",
		"      - run: go run x",
		"          --flag",
		"      - uses: &anchor g/h@i",
		"      - *anchor",
		`      - "uses": j/k@l`,
		"      - name: x",
		"        'with':",
		"          version: v1",
		"  other:",
		"    steps:",
		"    - *anchor",
		`    - run: "echo \"a\" b"`,
		`      name: 'it''s'`,
		`    - run: "go vet C:\\x"`,
		`      name: "it's" # 'a'' b'`,
		`    - run: 'echo "a"'`,
		"    - name: a name",
		"        continued",
		"    - ? a complex key",
		"    - run: echo",
		"      shell: bash",
		"    runs-on:",
		"      - [self-hosted]",
		"      - - nested",
		"      -",
		"...",
	))
	want := []string{
		"w.yml:4: db: a flow collection; write it as a block, one key a line",
		"w.yml:7: with: a flow collection; write it as a block, one key a line",
		"w.yml:8: an item of steps: is not a block mapping; write it one key a line",
		"w.yml:11: run: a value continued on the next line; write it on one line or as a block scalar",
		"w.yml:12: uses: an anchor, an alias or a tag; write the value out",
		"w.yml:13: an item of steps: is not a block mapping; write it one key a line",
		`w.yml:14: "uses": a quoted key; write the key plain`,
		`w.yml:16: 'with': a quoted key; write the key plain`,
		"w.yml:20: an item of steps: is not a block mapping; write it one key a line",
		"w.yml:21: run: a quoted value with an escape in it; write it unquoted or as a block",
		"w.yml:22: name: a quoted value with an escape in it; write it unquoted or as a block",
		"w.yml:23: run: a quoted value with an escape in it; write it unquoted or as a block",
		`w.yml:27: "continued": not a block key, a one-line item or a comment; write it in block style, one key a line`,
		"w.yml:28: an item of steps: is not a block mapping; write it one key a line",
		`w.yml:32: "- [self-hosted]": not a block key, a one-line item or a comment; write it in block style, one key a line`,
		`w.yml:33: "- - nested": not a block key, a one-line item or a comment; write it in block style, one key a line`,
		`w.yml:34: "-": not a block key, a one-line item or a comment; write it in block style, one key a line`,
		`w.yml:35: "...": not a block key, a one-line item or a comment; write it in block style, one key a line`,
		// A uses: and a version under a line refused above are read at a
		// path no rule reads, so they are named as well.
		"w.yml:9: uses: not read as a step's or a job's; write the step in block style, one key a line",
		"w.yml:17: version: not read as a step's with: or an env: key; write it in block style, one key a line",
	}
	if !slices.Equal(w.refused, want) {
		t.Errorf("refused:\n  %s\nwant:\n  %s", strings.Join(w.refused, "\n  "), strings.Join(want, "\n  "))
	}
}

// A flow collection whose brackets sit on lines of their own opens no step,
// so the action inside it, and the version it is given, land at a path no
// gate reads. Each bracket line is refused, and the uses: and the version
// are too.
func TestWorkflowReaderRefusesAFlowCollectionOnLinesOfItsOwn(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{
			"an action in a flow sequence of steps",
			textLines(
				"jobs:",
				"  sneaky:",
				"    runs-on: ubuntu-latest",
				"    steps:",
				"      [",
				"        {",
				"          uses: evil-org/evil-action@main",
				"        }",
				"      ]",
			),
			[]string{
				`w.yml:5: "[": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:6: "{": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:8: "}": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:9: "]": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				"w.yml:7: uses: not read as a step's or a job's; write the step in block style, one key a line",
			},
		},
		{
			"a flow with: under it",
			textLines(
				"jobs:",
				"  announce:",
				"    steps:",
				"      [",
				"        {",
				"          uses: evil-org/release-notifier@main,",
				"          with:",
				"            {",
				`              token: "${{ github.token }}"`,
				"            }",
				"        }",
				"      ]",
			),
			[]string{
				`w.yml:4: "[": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:5: "{": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:8: "{": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:10: "}": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:11: "}": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:12: "]": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				"w.yml:6: uses: not read as a step's or a job's; write the step in block style, one key a line",
			},
		},
		{
			"an installer's version in a flow with:",
			textLines(
				"jobs:",
				"  resign:",
				"    steps:",
				"      [",
				"        {",
				"          uses: sigstore/cosign-installer@0123456789abcdef0123456789abcdef01234567,",
				"          with:",
				"            {",
				"              cosign-release: latest",
				"            }",
				"        }",
				"      ]",
			),
			[]string{
				`w.yml:4: "[": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:5: "{": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:8: "{": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:10: "}": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:11: "}": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				`w.yml:12: "]": not a block key, a one-line item or a comment; write it in block style, one key a line`,
				"w.yml:6: uses: not read as a step's or a job's; write the step in block style, one key a line",
				"w.yml:9: cosign-release: not read as a step's with: or an env: key; write it in block style, one key a line",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := parseWorkflow("w.yml", tc.text)
			if !slices.Equal(w.refused, tc.want) {
				t.Errorf("refused:\n  %s\nwant:\n  %s", strings.Join(w.refused, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}

// A uses: is read only as a step's or a job's, and a version only as a
// step's with: or env: key, so either one anywhere else on a line that is
// not a comment is refused, a line inside a block scalar included. A key
// line outside every job's steps holds no installer's version, and is read
// as any key is.
func TestWorkflowReaderHoldsEveryUsesAndVersionToWhatItRecorded(t *testing.T) {
	w := parseWorkflow("w.yml", textLines(
		"on:",
		"  release:",
		"    types: [published]",
		"  workflow_dispatch:",
		"    inputs:",
		"      version:",
		`        description: "the version: a tag"`,
		"jobs:",
		"  build:",
		"    outputs:",
		"      version: ${{ steps.v.outputs.version }}",
		"    steps:",
		"      # - uses: a/b@main # cosign-release: latest",
		"      - id: v",
		"        run: |",
		"          uses: a/b@main",
		"          cosign-release: latest",
		"  call:",
		"    uses: ./.github/workflows/x.yml",
		"    with:",
		"      version: ${{ github.ref_name }}",
	))
	want := []string{
		"w.yml:16: uses: not read as a step's or a job's; write the step in block style, one key a line",
		"w.yml:17: cosign-release: not read as a step's with: or an env: key; write it in block style, one key a line",
	}
	if !slices.Equal(w.refused, want) {
		t.Errorf("refused:\n  %s\nwant:\n  %s", strings.Join(w.refused, "\n  "), strings.Join(want, "\n  "))
	}
}

// Every workflow here reads with nothing refused, and its steps are read.
func TestWorkflowReaderReadsEveryWorkflowHere(t *testing.T) {
	t.Chdir(parityRepoRoot)
	flows, err := readWorkflows()
	if err != nil {
		t.Fatal(err)
	}
	steps := 0
	for _, w := range flows {
		if w.refused != nil {
			t.Errorf("%s refused:\n  %s", w.path, strings.Join(w.refused, "\n  "))
		}
		steps += len(w.steps)
	}
	if len(flows) < 5 || steps < 30 {
		t.Errorf("read %d workflows and %d steps, under the floors of 5 and 30", len(flows), steps)
	}
}

// A comment starts at a # after white space, a tab included, and a # inside
// a word or a quoted value is text (https://yaml.org/spec/1.2.2/#66-comments).
func TestWorkflowValueCutsTheComment(t *testing.T) {
	for _, tc := range []struct{ in, value, comment string }{
		{"a/b@c # v1", "a/b@c", "v1"},
		{"a/b@c\t# v1", "a/b@c", "v1"},
		{"a#b", "a#b", ""},
		{"# v1", "", "v1"},
		{`'a # b' # c`, "a # b", "c"},
		{"\"a\"\t# c", "a", "c"},
		{"", "", ""},
	} {
		if value, comment := workflowValue(tc.in); value != tc.value || comment != tc.comment {
			t.Errorf("workflowValue(%q) = %q, %q, want %q, %q", tc.in, value, comment, tc.value, tc.comment)
		}
	}
}
