package favroapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

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
	require.NoError(t, err)

	// ...and a get-one call, where the id is in the path rather than
	// the query. Driving only a list endpoint is how the first version
	// of this test passed while /cards/{cardId} was still logged whole.
	_, _ = c.GetCard(context.Background(), markerCardID)

	logged := buf.String()
	require.NotEmpty(t, logged, "captured nothing, so this test proved nothing — did logRequest stop emitting?")

	for _, forbidden := range []string{
		markerOrgID, markerCardCommonID, markerWidgetID,
		markerSequentialID, markerCardID, markerEmail, markerToken,
	} {
		require.NotContains(t, logged, forbidden,
			"a value that identifies the subject reached the log; §9 forbids it at every level")
	}

	// The floor: the debug line still has to be useful, or "no leak"
	// and "no logging" print the same sentence. The parameter names
	// are what a debug line is for.
	require.Contains(t, logged, "favro request")
	require.Contains(t, logged, "cardCommonId", "the query's parameter names are the part worth keeping")
	require.Contains(t, logged, "widgetCommonId")
	require.Contains(t, logged, "/cards/{id}", "the endpoint shape is the part of the path worth keeping")
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
		require.Equal(t, want, redactPathIDs(in))
	}

	// Fails safe: a segment the rule cannot read as a resource name is
	// redacted rather than printed.
	require.Equal(t, "/{id}", redactPathIDs("/Cards"))
	require.Equal(t, "/{id}", redactPathIDs("/cards-2024;drop"))
}

// TestQueryKeys pins the helper the line above depends on: names in,
// values never.
func TestQueryKeys(t *testing.T) {
	t.Parallel()

	require.Nil(t, queryKeys(""))
	require.Equal(t,
		[]string{"cardCommonId", "page", "widgetCommonId"},
		queryKeys("widgetCommonId="+markerWidgetID+"&cardCommonId="+markerCardCommonID+"&page=2"))

	// A query Favro would never send, and url.ParseQuery rejects: the
	// names it did parse still come back, because a debug line that
	// disappears on malformed input is a debug line that vanishes
	// exactly when it is needed.
	got := queryKeys("sequentialId=" + markerSequentialID + "&%zz=broken")
	require.Equal(t, []string{"sequentialId"}, got)
	require.NotContains(t, strings.Join(got, ","), markerSequentialID)
}
