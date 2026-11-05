package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// oneOrganization holds that no tool input declares an organization_id.
// The server binds FAVRO_ORGANIZATION_ID at startup, and Favro routes
// every organization call by the organizationId header. The path segment
// of GET /organizations/{id} selects nothing, although Favro's reference
// calls it required (https://favro.com/developer/#get-an-organization),
// so such an input reads as a choice and is not one.
//
// Inputs only: `favro_ping` reports the bound organization id, and an
// output is not a choice a caller makes. The tools come from the
// binary's own schema dump, so a tool added later is held by being
// registered.

// boundOrgInput is the input key no tool may declare.
const boundOrgInput = "organization_id"

// oneOrganizationWaived are tools allowed to declare it anyway, with the
// reason. It stays empty: an entry is a tool asking a caller to choose
// something this server does not let them choose. It exists because a
// rule with no way to record a considered exception gets weakened
// instead.
var oneOrganizationWaived = map[string]string{}

func oneOrganization(w io.Writer, args []string) error {
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
		if _, waived := oneOrganizationWaived[t.Name]; waived {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s declares an %s input; this server binds one organization at startup and Favro routes "+
				"by the header, so that input cannot select anything — it reads as a choice and is not one",
			t.Name, boundOrgInput))
	}

	// A waiver naming a tool that no longer declares it reads as a
	// considered exception and covers nothing.
	for _, name := range slices.Sorted(maps.Keys(oneOrganizationWaived)) {
		idx := slices.IndexFunc(dump.Tools, func(t schemaTool) bool { return t.Name == name })
		if idx < 0 {
			problems = append(problems, fmt.Sprintf("%s is waived from one-organization and is not registered", name))
			continue
		}
		if _, ok := dump.Tools[idx].InputSchema.Properties[boundOrgInput]; !ok {
			problems = append(problems, fmt.Sprintf(
				"%s is waived from one-organization and does not declare %s; remove the waiver", name, boundOrgInput))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	_, err = fmt.Fprintf(w, "one-organization ok: %d tool inputs carry no %s (%d waived)\n",
		checked, boundOrgInput, len(oneOrganizationWaived))
	return err
}
