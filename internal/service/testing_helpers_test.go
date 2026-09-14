package service

import (
	"net/http"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favroapi"
	"github.com/mmedum/favro-mcp/internal/favroapi/favroapitest"
)

// favroFixture forwards to the one shared client fixture; see
// favroapitest for why there is only one.
func favroFixture(t *testing.T, handler http.Handler) *favroapi.Client {
	t.Helper()
	return favroapitest.Client(t, handler)
}
