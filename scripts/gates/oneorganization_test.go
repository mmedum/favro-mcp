package main

import (
	"os"
	"strings"
	"testing"
)

// favro_get_organization once took a required organization_id that
// Favro ignored: the API routes by the organizationId header, so any
// value returned the bound organization with a 200.
func TestOneOrganizationAgainstThisRepository(t *testing.T) {
	bin := defaultBinary()
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no binary; `make build` first (CI builds before it runs this gate)")
	}
	var out sink
	if err := oneOrganization(&out, []string{bin}); err != nil {
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
	bin := defaultBinary()
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no binary; `make build` first")
	}

	restore := oneOrganizationWaived
	oneOrganizationWaived = map[string]string{"favro_ping": "it does not declare one, so this waiver is stale"}
	t.Cleanup(func() { oneOrganizationWaived = restore })

	var out sink
	err := oneOrganization(&out, []string{bin})
	if err == nil {
		t.Fatal("a waiver for a tool that declares no organization_id was accepted")
	}
	if !strings.Contains(err.Error(), "remove the waiver") {
		t.Errorf("the error does not say the waiver is stale: %v", err)
	}

	oneOrganizationWaived = map[string]string{"favro_not_registered": "names nothing"}
	var out2 sink
	if err := oneOrganization(&out2, []string{bin}); err == nil ||
		!strings.Contains(err.Error(), "is not registered") {
		t.Errorf("a waiver naming an unregistered tool was not caught: %v", err)
	}
}
