package main

import (
	"strings"
	"testing"
)

// A number in prose is a claim about the list beside it. The fix for a
// wrong one is not to edit the number by hand each release; it is this.
func TestCountsMatchHoldsTheNumberAgainstTheList(t *testing.T) {
	if problems := countsMatch("It exposes Favro's REST API as 83 typed tools.", 83); len(problems) > 0 {
		t.Errorf("83 and 83 agree; got %v", problems)
	}
	problems := countsMatch("It exposes Favro's REST API as 83 typed tools.", 84)
	if len(problems) != 1 || !strings.Contains(problems[0], "registers 84") {
		t.Errorf("a stale count should be reported, got %v", problems)
	}
	// A README that states no count leaves this rule holding nothing,
	// and a rule holding nothing should say so rather than pass.
	if problems := countsMatch("no numbers here", 83); len(problems) == 0 {
		t.Error("a README with no tool count means this check reads nothing; it should say so")
	}
}

func TestToolsDocumentedReadsBothDirections(t *testing.T) {
	registered := []string{"favro_ping", "favro_list_cards"}
	toolsDoc := "| `favro_ping` | 1 | Liveness. |\n| `favro_list_cards` | 3 | Lists cards. |\n"

	if problems := toolsDocumented(toolsDoc, "", registered); len(problems) > 0 {
		t.Errorf("both tools are documented; got %v", problems)
	}

	// A registered tool nothing documents.
	problems := toolsDocumented("| `favro_ping` | 1 | Liveness. |\n", "", registered)
	if !strings.Contains(strings.Join(problems, "\n"), "favro_list_cards") {
		t.Errorf("an undocumented tool should be named, got %v", problems)
	}

	// Documented in the README's prose rather than the table is fine:
	// the table is the default surface, and a tool behind a flag is
	// described in the paragraph that explains the flag.
	if problems := toolsDocumented("| `favro_ping` | 1 | Liveness. |\n",
		"and `favro_list_cards` when enabled", registered); len(problems) > 0 {
		t.Errorf("prose counts as documentation; got %v", problems)
	}

	// A row naming something that is not a tool.
	problems = toolsDocumented(toolsDoc+"| `favro_teleport` | 9 | Invented. |\n", "", registered)
	if !strings.Contains(strings.Join(problems, "\n"), "favro_teleport") {
		t.Errorf("a table row for an unregistered tool should be reported, got %v", problems)
	}

	// The floor: a file with no rows is a check reading the wrong thing.
	problems = toolsDocumented("", "`favro_ping` `favro_list_cards`", registered)
	if !strings.Contains(strings.Join(problems, "\n"), "no tool table rows") {
		t.Errorf("an empty table should be reported as a broken check, got %v", problems)
	}
}

// A design document's status line claims something no tag can derive, so
// nothing but this will catch it. A sibling's said v0.5.0 five releases
// after v0.5.0.
func TestStatusCurrentComparesAgainstTheNewestHeading(t *testing.T) {
	arch := "**Status, 2026-09-13.** Released: v1.1.2. The rest of the document.\n"
	changelog := "## [Unreleased]\n\n## [1.1.2] - 2026-08-27\n\n## [1.1.1] - 2026-08-26\n"
	if problems := statusCurrent(arch, changelog); len(problems) > 0 {
		t.Errorf("the status line is current; got %v", problems)
	}

	stale := "**Status, 2026-09-13.** Released: v1.0.0. The rest.\n"
	if problems := statusCurrent(stale, changelog); len(problems) != 1 {
		t.Errorf("a stale status line should be reported, got %v", problems)
	}
	if problems := statusCurrent("no status line here", changelog); len(problems) != 1 {
		t.Errorf("a missing status line means the check is reading the wrong file, got %v", problems)
	}
	if problems := statusCurrent(arch, "# Changelog\n"); len(problems) != 1 {
		t.Errorf("a changelog with no headings should be reported, got %v", problems)
	}
}

// The gate against the repository.
func TestStalenessAgainstThisRepository(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	bin := root + "/bin/favro-mcp"
	if !built(bin) {
		t.Skip("no binary; `make build` first (CI builds before it runs this gate)")
	}
	var out sink
	if err := staleness(&out, bin); err != nil {
		t.Fatalf("the documentation should match the code: %v", err)
	}
	out.mustSay(t, "tools documented")
}
