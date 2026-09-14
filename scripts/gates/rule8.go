package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// Hard rule 8: the server binds FAVRO_ORGANIZATION_ID at startup and no
// tool takes an organization_id.
//
// It was the loudest unheld rule left in this repository, and it was
// false. `favro_get_organization` took one, required — and Favro ignores
// it, because the API routes by the organizationId HEADER and the path
// segment decides nothing. Favro's own reference says the opposite
// ("the id of the organization to be retrieved. Required."), which is
// §2.1 one more time. A model asking for one organization was handed
// another, with a 200 and no indication.
//
// Inputs only. `favro_ping` REPORTS the bound organization id, which is
// the whole point of a liveness check, and an output is not a choice a
// caller makes.
//
// Derived from the binary's own schema dump rather than from a list of
// tools typed here, so a tool added next week is held by having been
// registered.

// boundOrgInput is the input key no tool may declare.
const boundOrgInput = "organization_id"

// rule8Waived are tools allowed to declare it anyway, with the reason.
// Empty, and the intent is that it stays empty: an entry here is a tool
// asking a caller to choose something this server does not let them
// choose. It exists because api-coverage taught that a rule with no way
// to record a considered exception gets weakened instead.
var rule8Waived = map[string]string{}

func rule8(w io.Writer, args []string) error {
	out, err := dumpSchemas(binOr(args))
	if err != nil {
		return err
	}
	dump, err := parseSchemas(out)
	if err != nil {
		return fmt.Errorf("parse --dump-schemas: %w", err)
	}
	if len(dump.Tools) < 50 {
		return fmt.Errorf("the dump carries %d tools; this check is not reading this server",
			len(dump.Tools))
	}

	var problems []string
	checked := 0
	for _, t := range dump.Tools {
		checked++
		if _, ok := t.InputSchema.Properties[boundOrgInput]; !ok {
			continue
		}
		if _, waived := rule8Waived[t.Name]; waived {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s declares an %s input; this server binds one organization at startup and Favro routes "+
				"by the header, so that input cannot select anything — it reads as a choice and is not one",
			t.Name, boundOrgInput))
	}

	// A waiver naming a tool that no longer declares it reads as a
	// considered exception and covers nothing, which is the half that
	// rots — the same failure api-coverage exists to catch.
	for _, name := range slices.Sorted(maps.Keys(rule8Waived)) {
		idx := slices.IndexFunc(dump.Tools, func(t schemaTool) bool { return t.Name == name })
		if idx < 0 {
			problems = append(problems, fmt.Sprintf("%s is waived from rule 8 and is not registered", name))
			continue
		}
		if _, ok := dump.Tools[idx].InputSchema.Properties[boundOrgInput]; !ok {
			problems = append(problems, fmt.Sprintf(
				"%s is waived from rule 8 and does not declare %s; remove the waiver", name, boundOrgInput))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "rule 8 ok: %d tool inputs carry no %s (%d waived)\n",
		checked, boundOrgInput, len(rule8Waived))
	return err
}
