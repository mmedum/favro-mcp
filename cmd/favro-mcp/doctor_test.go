package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/auth"
	"github.com/mmedum/favro-mcp/internal/config"
)

// The organization ids these tests use are fabricated, and deliberately
// NOT 24 hex characters: internal/redact's id pattern is anchored on
// that length, so these are caught only because doctor REGISTERS them
// with the redactor rather than hoping a pattern matches. A fixture
// that matched the pattern would pass whether the registration worked
// or not, which is the whole reason these two are shaped this way.
const (
	boundOrgID = "org-bound-not-real"
	otherOrgID = "org-other-not-real"
)

// favroStub serves the one endpoint doctor calls. orgIDs is what the
// token can see.
func favroStub(t *testing.T, status int, orgIDs ...string) string {
	t.Helper()
	entities := make([]map[string]any, 0, len(orgIDs))
	for _, id := range orgIDs {
		entities = append(entities, map[string]any{"organizationId": id, "name": "a workspace"})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"limit": 100, "page": 0, "pages": 1, "requestId": "req", "entities": entities,
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// doctorEnv points credential resolution at fabricated env values.
func doctorEnv(t *testing.T, orgID string) {
	t.Helper()
	isolateCredentials(t)
	restoreDefaultLogger(t)
	t.Setenv(auth.EnvUserEmail, testEmail)
	t.Setenv(auth.EnvAPIToken, testAPIToken)
	t.Setenv(auth.EnvOrganizationID, orgID)
}

func runReport(t *testing.T, showIDs bool, baseURL string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	err := doctorReport(context.Background(), config.Load(), &buf, showIDs, baseURL)
	return buf.String(), err
}

// TestDoctorRedactsCredentials is the rule this whole subcommand rests
// on: the default report is meant to be pasted into a public issue, so
// no value that identifies the tenant may survive in it.
func TestDoctorRedactsCredentials(t *testing.T) {
	doctorEnv(t, boundOrgID)
	out, err := runReport(t, false, favroStub(t, http.StatusOK, boundOrgID))
	if err != nil {
		t.Fatalf("doctorReport: %v", err)
	}
	for _, secret := range []string{testEmail, testAPIToken, boundOrgID} {
		if strings.Contains(out, secret) {
			t.Errorf("the redacted report contains %q; it is written to be pasted into a public issue", secret)
		}
	}
	if !strings.Contains(out, "{credential}") && !strings.Contains(out, "{user 1}") {
		t.Error("nothing in the report was redacted; a report that redacts nothing and one that " +
			"stopped redacting print the same page")
	}
}

// TestDoctorShowIDsPrintsIDsButNeverTheToken is the other half. The
// flag exists because a redacted report diagnoses a wrong binding and
// cannot fix it — the user needs the real id. The token is still not
// theirs to paste anywhere.
func TestDoctorShowIDsPrintsIDsButNeverTheToken(t *testing.T) {
	doctorEnv(t, boundOrgID)
	out, err := runReport(t, true, favroStub(t, http.StatusOK, boundOrgID))
	if err != nil {
		t.Fatalf("doctorReport: %v", err)
	}
	if !strings.Contains(out, boundOrgID) {
		t.Errorf("--show-ids did not print the organization id; that is the one thing it is for")
	}
	if !strings.Contains(out, testEmail) {
		t.Errorf("--show-ids did not print the address")
	}
	if strings.Contains(out, testAPIToken) {
		t.Error("--show-ids printed the API token; no mode prints the token")
	}
	if !strings.Contains(out, "NOT REDACTED") {
		t.Error("--show-ids did not warn that its output is unsafe to paste")
	}
}

// TestDoctorFailsOnWrongOrganizationBinding covers the failure this
// subcommand exists for: credentials that work, bound to an
// organization the token cannot see. Every tool returns not_found and
// nothing says why.
func TestDoctorFailsOnWrongOrganizationBinding(t *testing.T) {
	doctorEnv(t, boundOrgID)
	out, err := runReport(t, false, favroStub(t, http.StatusOK, otherOrgID))
	if err == nil {
		t.Fatal("doctorReport returned nil for an organization id the token cannot see")
	}
	if !strings.Contains(out, "FAIL") {
		t.Error("the report marked no check as failed")
	}
	if !strings.Contains(out, auth.EnvOrganizationID) {
		t.Errorf("the failure does not name %s, so it does not say what to change", auth.EnvOrganizationID)
	}
	if !strings.Contains(out, "--show-ids") {
		t.Error("the failure does not tell the user how to see which id to use")
	}
	for _, secret := range []string{boundOrgID, otherOrgID} {
		if strings.Contains(out, secret) {
			t.Errorf("the failure path leaked %q into a report meant for an issue", secret)
		}
	}
}

// TestDoctorReportsUnreachableAPI: a 401 is a failure with a message,
// not a crash.
func TestDoctorReportsUnreachableAPI(t *testing.T) {
	doctorEnv(t, boundOrgID)
	out, err := runReport(t, false, favroStub(t, http.StatusUnauthorized))
	if err == nil {
		t.Fatal("doctorReport returned nil when Favro rejected the credentials")
	}
	if !strings.Contains(out, "FAIL") || !strings.Contains(out, "reachable") {
		t.Errorf("the report does not show the reachability check failing:\n%s", out)
	}
}

// TestDoctorWithoutCredentialsSkipsTheNetwork. With nothing resolved
// there is nothing to ask Favro, and a report that hung on DNS would be
// worse than one that says so.
func TestDoctorWithoutCredentialsSkipsTheNetwork(t *testing.T) {
	isolateCredentials(t)
	restoreDefaultLogger(t)
	// A URL nothing listens on: if the report contacts it despite
	// having no credentials, this test fails rather than passing slowly.
	out, err := runReport(t, false, "http://127.0.0.1:1/api/v1")
	if err == nil {
		t.Fatal("doctorReport returned nil with no credentials configured")
	}
	if !strings.Contains(out, "skipped") {
		t.Errorf("the API section did not report itself skipped:\n%s", out)
	}
	if !strings.Contains(out, missingCredsHint()) {
		t.Error("the report does not say how to configure credentials")
	}
}

// TestDoctorReportsDestructiveFlag: whether the delete-style tools are
// registered changes what this server can do unattended, and a bug
// report that omits it is missing the first thing to ask about.
func TestDoctorReportsDestructiveFlag(t *testing.T) {
	doctorEnv(t, boundOrgID)
	t.Setenv(config.EnvEnableDestructive, "true")
	out, err := runReport(t, false, favroStub(t, http.StatusOK, boundOrgID))
	if err != nil {
		t.Fatalf("doctorReport: %v", err)
	}
	if !strings.Contains(out, "warn") || !strings.Contains(out, config.EnvEnableDestructive) {
		t.Errorf("the report does not warn that destructive tools are registered:\n%s", out)
	}
}

// TestDoctorRejectsUnknownFlag — a typo'd flag must not silently
// produce the default report.
func TestDoctorRejectsUnknownFlag(t *testing.T) {
	isolateCredentials(t)
	restoreDefaultLogger(t)
	var buf bytes.Buffer
	if err := runDoctor(context.Background(), config.Load(), &buf, []string{"--show-id"}); err == nil {
		t.Fatal("runDoctor accepted an unknown flag")
	}
}
