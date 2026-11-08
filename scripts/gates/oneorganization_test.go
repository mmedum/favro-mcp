package main

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
)

// favro_get_organization once took a required organization_id that
// Favro ignored: the API routes by the organizationId header, so any
// value returned the bound organization with a 200. The surface is made in
// process, with the committed waivers.
func TestOneOrganizationAgainstThisRepository(t *testing.T) {
	var out sink
	if err := oneOrganizationOf(&out, stalenessDump(t), oneOrganizationWaived); err != nil {
		t.Fatalf("no tool may take an organization_id: %v", err)
	}
	out.mustSay(t, "tool inputs carry no organization_id")
}

// The floor: a dump this check cannot recognize must fail rather than
// report that it found nothing wrong.
func TestOneOrganizationRefusesADumpItCannotRead(t *testing.T) {
	var out sink
	err := oneOrganization(&out, []string{"/nonexistent/binary"})
	if err == nil {
		t.Fatal("one-organization passed against a binary that does not exist")
	}
}

// A waiver naming a tool that does not declare the input reads as a
// considered exception and covers nothing — the half that rots.
func TestOneOrganizationRejectsAStaleWaiver(t *testing.T) {
	dump := stalenessDump(t)
	var out sink
	err := oneOrganizationOf(&out, dump, map[string]string{"favro_ping": "it does not declare one, so this waiver is stale"})
	if err == nil {
		t.Fatal("a waiver for a tool that declares no organization_id was accepted")
	}
	if !strings.Contains(err.Error(), "remove the waiver") {
		t.Errorf("the error does not say the waiver is stale: %v", err)
	}

	if err := oneOrganizationOf(&out, dump, map[string]string{"favro_not_registered": "names nothing"}); err == nil ||
		!strings.Contains(err.Error(), "is not registered") {
		t.Errorf("a waiver naming an unregistered tool was not caught: %v", err)
	}

	// A tool that declares the input fails, unless the table waives it.
	declares := stalenessDump(t)
	i := slices.IndexFunc(declares.Tools, func(t schemaTool) bool { return t.Name == "favro_ping" })
	props := map[string]json.RawMessage{boundOrgInput: json.RawMessage(`{"type":"string"}`)}
	maps.Copy(props, declares.Tools[i].InputSchema.Properties)
	declares.Tools[i].InputSchema.Properties = props
	if err := oneOrganizationOf(&out, declares, nil); err == nil || !strings.HasPrefix(err.Error(), "favro_ping declares an organization_id input") {
		t.Errorf("a tool declaring organization_id, waived by nothing = %v", err)
	}
	if err := oneOrganizationOf(&out, declares, map[string]string{"favro_ping": "a considered exception"}); err != nil {
		t.Errorf("a tool declaring organization_id, waived = %v, want none", err)
	}
}
