package favro

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type testEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestPageEnvelope_HasNextPage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  PageEnvelope[testEntity]
		want bool
	}{
		{"single page", PageEnvelope[testEntity]{Page: 0, Pages: 1}, false},
		{"first of two", PageEnvelope[testEntity]{Page: 0, Pages: 2}, true},
		{"last of two", PageEnvelope[testEntity]{Page: 1, Pages: 2}, false},
		{"middle of three", PageEnvelope[testEntity]{Page: 1, Pages: 3}, true},
		{"empty (no pages)", PageEnvelope[testEntity]{Page: 0, Pages: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.env.HasNextPage())
		})
	}
}

func TestPageEnvelope_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	body := `{
		"limit": 100,
		"page": 1,
		"pages": 3,
		"requestId": "req-abc",
		"entities": [{"id": "e1", "name": "first"}, {"id": "e2", "name": "second"}]
	}`

	var env PageEnvelope[testEntity]
	require.NoError(t, json.Unmarshal([]byte(body), &env))

	require.Equal(t, 100, env.Limit)
	require.Equal(t, 1, env.Page)
	require.Equal(t, 3, env.Pages)
	require.Equal(t, "req-abc", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "first", env.Entities[0].Name)
	require.True(t, env.HasNextPage())
}
