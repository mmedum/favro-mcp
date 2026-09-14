package favroapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mmedum/favro-mcp/internal/auth"
	"github.com/mmedum/favro-mcp/internal/favro"
)

// These markers stand in for the values §9 forbids in a log. Each has
// the length of the real thing and says in itself that it is invented,
// which is what the leak gate asks of a synthetic id. They are checked
// for as substrings, so a log line carrying half of one still fails.
const (
	markerOrgID        = "NoSuchOrganizationAaaa01"
	markerCardCommonID = "NoSuchCardCommonIdAaaa02"
	markerWidgetID     = "NoSuchWidgetCommonIdAa03"
	markerSequentialID = "4242"
	markerCardID       = "NoSuchCardIdAaaaaaaaaa05"
	markerEmail        = "logging-probe@example.test"
	markerToken        = "synthetic-token-for-the-logging-test"
)

// captureLogs installs a slog handler at the most verbose level for
// the duration of the test and returns the buffer it writes to.
//
// LevelDebug matters: the leak this test exists for only ever appeared
// at debug, so a capture at the default level would have passed
// against the broken code.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestDebugLogNeverCarriesTheSubject is the test §9 says A2 owes.
//
// Favro addresses everything by id in the query string, so a debug
// line that logged the query reconstructed which cards a session
// touched — which is what the code did until this commit. The
// assertion is deliberately over the whole captured output rather than
// over the one line under suspicion: the rule is that no forbidden
// value reaches a log at any level, not that one call site was fixed.
func TestDebugLogNeverCarriesTheSubject(t *testing.T) {
	buf := captureLogs(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entities":[],"page":0,"pages":1,"limit":100}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(auth.Token{
		Email:          markerEmail,
		APIToken:       markerToken,
		OrganizationID: markerOrgID,
	})
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()

	// Every id-shaped filter Favro accepts on /cards, all at once...
	_, err := c.ListCards(context.Background(), 0, "", favro.ListCardsFilter{
		CardCommonID:   markerCardCommonID,
		WidgetCommonID: markerWidgetID,
		SequentialID:   4242,
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	// ...and a get-one call, where the id is in the path rather than
	// the query. Driving only a list endpoint is how the first version
	// of this test passed while /cards/{cardId} was still logged whole.
	_, _ = c.GetCard(context.Background(), markerCardID)

	logged := buf.String()
	if len(logged) == 0 {
		t.Fatal("captured nothing, so this test proved nothing — did logRequest stop emitting?")
	}

	for _, forbidden := range []string{
		markerOrgID, markerCardCommonID, markerWidgetID,
		markerSequentialID, markerCardID, markerEmail, markerToken,
	} {
		if strings.Contains(logged, forbidden) {
			t.Errorf("a value that identifies the subject reached the log; §9 forbids it at every level: %q present", forbidden)
		}
	}

	// The floor: the debug line still has to be useful, or "no leak"
	// and "no logging" print the same sentence. The parameter names
	// are what a debug line is for.
	if !strings.Contains(logged, "favro request") {
		t.Errorf("logged does not contain %q", "favro request")
	}
	if !strings.Contains(logged, "cardCommonId") {
		t.Errorf("the query's parameter names are the part worth keeping: %q missing", "cardCommonId")
	}
	if !strings.Contains(logged, "widgetCommonId") {
		t.Errorf("logged does not contain %q", "widgetCommonId")
	}
	if !strings.Contains(logged, "/cards/{id}") {
		t.Errorf("the endpoint shape is the part of the path worth keeping: %q missing", "/cards/{id}")
	}
}

// TestRedactPathIDs pins the rule the line above depends on: the shape
// of the endpoint survives, the identifiers do not.
func TestRedactPathIDs(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"/cards":                 "/cards",
		"/cards/" + markerCardID: "/cards/{id}",
		"/cards/" + markerCardID + "/attachments": "/cards/{id}/attachments",
		"/comments/" + markerCardCommonID:         "/comments/{id}",
		"/customfields":                           "/customfields",
		"":                                        "",
		"/":                                       "/",
	}
	for in, want := range cases {
		if got := redactPathIDs(in); got != want {
			t.Errorf("redactPathIDs(in) = %v, want %v", got, want)
		}
	}

	// Fails safe: a segment the rule cannot read as a resource name is
	// redacted rather than printed.
	if got := redactPathIDs("/Cards"); got != "/{id}" {
		t.Errorf("redactPathIDs(\"/Cards\") = %v, want %v", got, "/{id}")
	}
	if got := redactPathIDs("/cards-2024;drop"); got != "/{id}" {
		t.Errorf("redactPathIDs(\"/cards-2024;drop\") = %v, want %v", got, "/{id}")
	}
}

// TestQueryKeys pins the helper the line above depends on: names in,
// values never.
func TestQueryKeys(t *testing.T) {
	t.Parallel()

	if queryKeys("") != nil {
		t.Errorf("queryKeys(\"\") = %v, want nil", queryKeys(""))
	}
	if got := queryKeys("widgetCommonId=" + markerWidgetID + "&cardCommonId=" + markerCardCommonID + "&page=2"); !reflect.DeepEqual(got, ([]string{"cardCommonId", "page", "widgetCommonId"})) {
		t.Errorf("queryKeys(\"widgetCommonId=\" + markerWidgetID + \"&cardCommonId=\" + markerCardCommonID + \"&page=2\") = %v, want %v", got, []string{"cardCommonId", "page", "widgetCommonId"})
	}

	// A query Favro would never send, and url.ParseQuery rejects: the
	// names it did parse still come back, because a debug line that
	// disappears on malformed input is a debug line that vanishes
	// exactly when it is needed.
	got := queryKeys("sequentialId=" + markerSequentialID + "&%zz=broken")
	if got := got; !reflect.DeepEqual(got, ([]string{"sequentialId"})) {
		t.Errorf("got = %v, want %v", got, []string{"sequentialId"})
	}
	if strings.Contains(strings.Join(got, ","), markerSequentialID) {
		t.Errorf("strings.Join(got, \",\") unexpectedly contains %q", markerSequentialID)
	}
}
