package favroapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mmedum/favro-mcp/internal/favro"
)

func TestListGroups_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Group]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-g",
			Entities: []favro.Group{
				{
					GroupID: "g-1",
					Name:    "Engineers",
					Members: []favro.GroupMember{
						{UserID: "u-1", Role: "administrator"},
						{UserID: "u-2", Role: "member"},
					},
				},
				{
					GroupID: "g-2",
					Name:    "Read-only",
					// Members may be absent on some groups; omitempty drops it.
				},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	env, err := c.ListGroups(context.Background(), 0, "")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := env.RequestID; got != "req-g" {
		t.Errorf("env.RequestID = %v, want %v", got, "req-g")
	}
	if len(env.Entities) != 2 {
		t.Fatalf("len(env.Entities) = %d, want 2", len(env.Entities))
	}
	if got := env.Entities[0].Name; got != "Engineers" {
		t.Errorf("env.Entities[0].Name = %v, want %v", got, "Engineers")
	}
	if len(env.Entities[0].Members) != 2 {
		t.Fatalf("len(env.Entities[0].Members) = %d, want 2", len(env.Entities[0].Members))
	}
	if got := env.Entities[0].Members[0].Role; got != "administrator" {
		t.Errorf("env.Entities[0].Members[0].Role = %v, want %v", got, "administrator")
	}
	if len(env.Entities[1].Members) != 0 {
		t.Errorf("groups without members must NOT carry the field (omitempty drops it): got %v", env.Entities[1].Members)
	}

	rec := h.seen()
	if len(rec) != 1 {
		t.Fatalf("len(rec) = %d, want 1", len(rec))
	}
	if got := rec[0].Path; got != "/groups" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/groups")
	}
	if len(rec[0].Query.Encode()) != 0 {
		t.Errorf("no filter or page query expected on first-page list: got %v", rec[0].Query.Encode())
	}
}

func TestListGroups_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.PageEnvelope[favro.Group]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListGroups(context.Background(), 2, "req-prior")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}

	rec := h.seen()
	if got := rec[0].Query.Get("page"); got != "2" {
		t.Errorf("rec[0].Query.Get(\"page\") = %v, want %v", got, "2")
	}
	if got := rec[0].Headers.Get(headerRequestID); got != "req-prior" {
		t.Errorf("rec[0].Headers.Get(headerRequestID) = %v, want %v", got, "req-prior")
	}
}

func TestGetGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(favro.Group{
			GroupID: "g-zzz",
			Name:    "looked up",
			Members: []favro.GroupMember{
				{UserID: "u-only", Role: "member"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	g, err := c.GetGroup(context.Background(), "g-zzz")
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := g.GroupID; got != "g-zzz" {
		t.Errorf("g.GroupID = %v, want %v", got, "g-zzz")
	}
	if got := g.Name; got != "looked up" {
		t.Errorf("g.Name = %v, want %v", got, "looked up")
	}
	if len(g.Members) != 1 {
		t.Fatalf("len(g.Members) = %d, want 1", len(g.Members))
	}
	if got := g.Members[0].UserID; got != "u-only" {
		t.Errorf("g.Members[0].UserID = %v, want %v", got, "u-only")
	}

	rec := h.seen()
	if got := rec[0].Path; got != "/groups/g-zzz" {
		t.Errorf("rec[0].Path = %v, want %v", got, "/groups/g-zzz")
	}
}

func TestGetGroup_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetGroup(context.Background(), "")
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestGetGroup_NotFound(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.GetGroup(context.Background(), "missing")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %v, want nf", err)
	}
}

// TestCreateGroup_HappyPath pins POST /groups — name + members,
// response decoded as favro.Group.
func TestCreateGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPost {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
		}
		if got := rec.Path; got != "/groups" {
			t.Errorf("rec.Path = %v, want %v", got, "/groups")
		}
		requireJSONEq(t, `{"name":"Eng","members":[{"userId":"u-1","role":"administrator"}]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groupId":"g-new","name":"Eng","members":[{"userId":"u-1","role":"administrator"}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateGroup(context.Background(), favro.CreateGroupRequest{
		Name:    "Eng",
		Members: []favro.GroupMember{{UserID: "u-1", Role: "administrator"}},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.GroupID; got != "g-new" {
		t.Errorf("got.GroupID = %v, want %v", got, "g-new")
	}
}

func TestCreateGroup_EmptyName_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateGroup(context.Background(), favro.CreateGroupRequest{Name: ""})
	if err == nil {
		t.Fatal("err should have failed")
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestCreateGroup_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateGroup(WithDryRun(context.Background()), favro.CreateGroupRequest{Name: "x"})
	if !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	var rec *DryRunRecord
	if !errors.As(err, &rec) {
		t.Fatalf("got %v, want rec", err)
	}
	if got := rec.Method; got != http.MethodPost {
		t.Errorf("rec.Method = %v, want %v", got, http.MethodPost)
	}
}

func TestUpdateGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodPut {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodPut)
		}
		if got := rec.Path; got != "/groups/g-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/groups/g-1")
		}
		requireJSONEq(t, `{"name":"renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groupId":"g-1","name":"renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateGroup(context.Background(), "g-1", favro.UpdateGroupRequest{Name: "renamed"})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := got.Name; got != "renamed" {
		t.Errorf("got.Name = %v, want %v", got, "renamed")
	}
}

func TestUpdateGroup_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateGroup(context.Background(), "", favro.UpdateGroupRequest{Name: "x"})
	if !errors.Is(err, errMissingID) {
		t.Fatalf("got %v, want errMissingID", err)
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

func TestDeleteGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		if got := rec.Method; got != http.MethodDelete {
			t.Errorf("rec.Method = %v, want %v", got, http.MethodDelete)
		}
		if got := rec.Path; got != "/groups/g-1" {
			t.Errorf("rec.Path = %v, want %v", got, "/groups/g-1")
		}
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if err := c.DeleteGroup(context.Background(), "g-1"); err != nil {
		t.Fatalf("c.DeleteGroup(context.Background(), \"g-1\"): %v", err)
	}
}

func TestDeleteGroup_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	if !errors.Is(c.DeleteGroup(context.Background(), ""), errMissingID) {
		t.Fatalf("got %v, want errMissingID", c.DeleteGroup(context.Background(), ""))
	}
	if len(h.seen()) != 0 {
		t.Errorf("h.seen() = %v, want empty", h.seen())
	}
}

// TestUpdateCard_CustomFieldsValuePassthrough pins that
// favro.UpdateCardRequest.CustomFields marshals onto the wire in the
// per-type shapes documented under "favro.Card custom field parameters"
// at https://favro.com/developer/ — notably that Number and Rating
// travel in `total` (not `value`), select-flavored types put their
// item ids in `value`, and Members / Tags / Timeline / Link each
// have a dedicated sibling object.
func TestUpdateCard_CustomFieldsValuePassthrough(t *testing.T) {
	t.Parallel()

	num := 42.0
	rating := 3.0
	logged := 50400000.0

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		requireJSONEq(t, `{"customFields":[
			{"customFieldId":"cf-text","value":"hello"},
			{"customFieldId":"cf-num","total":42},
			{"customFieldId":"cf-bool","value":true},
			{"customFieldId":"cf-select","value":["item-1"]},
			{"customFieldId":"cf-rating","total":3},
			{"customFieldId":"cf-members","members":{"addUserIds":["u-1"],"removeUserIds":["u-2"]}},
			{"customFieldId":"cf-tags","tags":{"addTagIds":["t-1"]}},
			{"customFieldId":"cf-timeline","timeline":{"startDate":"2026-01-01","dueDate":"2026-02-01"}},
			{"customFieldId":"cf-link","link":{"url":"https://example.com","text":"docs"}},
			{"customFieldId":"cf-color","color":"blue-300"},
			{"customFieldId":"cf-time","addUserReports":[{"value":50400000,"description":"work"}]}
		]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cardId":"ci-1"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateCard(context.Background(), "ci-1", favro.UpdateCardRequest{
		CustomFields: []favro.CardCustomFieldUpdate{
			{CustomFieldID: "cf-text", Value: "hello"},
			{CustomFieldID: "cf-num", Total: &num},
			{CustomFieldID: "cf-bool", Value: true},
			{CustomFieldID: "cf-select", Value: []string{"item-1"}},
			{CustomFieldID: "cf-rating", Total: &rating},
			{CustomFieldID: "cf-members", Members: &favro.CustomFieldMembersUpdate{
				AddUserIDs:    []string{"u-1"},
				RemoveUserIDs: []string{"u-2"},
			}},
			{CustomFieldID: "cf-tags", Tags: &favro.CustomFieldTagsUpdate{AddTagIDs: []string{"t-1"}}},
			{CustomFieldID: "cf-timeline", Timeline: &favro.CustomFieldTimeline{
				StartDate: "2026-01-01",
				DueDate:   "2026-02-01",
			}},
			{CustomFieldID: "cf-link", Link: &favro.CustomFieldLink{URL: "https://example.com", Text: "docs"}},
			{CustomFieldID: "cf-color", Color: "blue-300"},
			{CustomFieldID: "cf-time", AddUserReports: []favro.CustomFieldTimeReport{
				{Value: &logged, Description: "work"},
			}},
		},
	})
	if err := err; err != nil {
		t.Fatalf("err: %v", err)
	}
}
