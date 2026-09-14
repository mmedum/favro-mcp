package favro

import (
	"encoding/json"
	"testing"
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
			if got := tc.env.HasNextPage(); got != tc.want {
				t.Errorf("HasNextPage() = %v, want %v (page %d of %d)",
					got, tc.want, tc.env.Page, tc.env.Pages)
			}
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
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if env.Limit != 100 {
		t.Errorf("Limit = %d, want 100", env.Limit)
	}
	if env.Page != 1 {
		t.Errorf("Page = %d, want 1", env.Page)
	}
	if env.Pages != 3 {
		t.Errorf("Pages = %d, want 3", env.Pages)
	}
	if env.RequestID != "req-abc" {
		t.Errorf("RequestID = %q, want %q", env.RequestID, "req-abc")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("Entities: got %d, want 2", len(env.Entities))
	}
	if env.Entities[0].Name != "first" {
		t.Errorf("Entities[0].Name = %q, want %q", env.Entities[0].Name, "first")
	}
	if !env.HasNextPage() {
		t.Error("HasNextPage() = false, want true for page 1 of 3")
	}
}
