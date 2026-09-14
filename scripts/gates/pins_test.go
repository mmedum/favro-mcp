package main

import "testing"

// pinsShell is the rule that cost a sibling an afternoon: asking only
// whether both keys appear says yes to a `shell: bash` sitting beside
// `run:` rather than under it, which is `defaults.shell` — not a key
// GitHub has, so it pins nothing while reading as though it does.
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

// The gate against the repository, which is also the floor: a run that
// reads no workflows reports exactly what a clean run reports.
func TestPinsReadsTheWorkflows(t *testing.T) {
	var out sink
	if err := pins(&out, nil); err != nil {
		t.Fatalf("the workflows should be pinned: %v", err)
	}
	out.mustSay(t, "actions by SHA")
}

func TestScannersAgreeNeedsBothSides(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := scannersAgree(root, []workflowFile{{name: "ci.yml", data: "jobs:\n"}}); err == nil {
		t.Error("a workflow naming no gitleaks version should fail, not pass quietly")
	}
}
