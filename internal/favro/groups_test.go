package favro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListGroups_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Group]{
			Limit:     100,
			Page:      0,
			Pages:     1,
			RequestID: "req-g",
			Entities: []Group{
				{
					GroupID: "g-1",
					Name:    "Engineers",
					Members: []GroupMember{
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
	require.NoError(t, err)
	require.Equal(t, "req-g", env.RequestID)
	require.Len(t, env.Entities, 2)
	require.Equal(t, "Engineers", env.Entities[0].Name)
	require.Len(t, env.Entities[0].Members, 2)
	require.Equal(t, "administrator", env.Entities[0].Members[0].Role)
	require.Empty(t, env.Entities[1].Members,
		"groups without members must NOT carry the field (omitempty drops it)")

	rec := h.seen()
	require.Len(t, rec, 1)
	require.Equal(t, "/groups", rec[0].Path)
	require.Empty(t, rec[0].Query.Encode(), "no filter or page query expected on first-page list")
}

func TestListGroups_WithPageForwardsRequestID(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PageEnvelope[Group]{Page: 2, Pages: 3})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.ListGroups(context.Background(), 2, "req-prior")
	require.NoError(t, err)

	rec := h.seen()
	require.Equal(t, "2", rec[0].Query.Get("page"))
	require.Equal(t, "req-prior", rec[0].Headers.Get(headerRequestID))
}

func TestGetGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Group{
			GroupID: "g-zzz",
			Name:    "looked up",
			Members: []GroupMember{
				{UserID: "u-only", Role: "member"},
			},
		})
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	g, err := c.GetGroup(context.Background(), "g-zzz")
	require.NoError(t, err)
	require.Equal(t, "g-zzz", g.GroupID)
	require.Equal(t, "looked up", g.Name)
	require.Len(t, g.Members, 1)
	require.Equal(t, "u-only", g.Members[0].UserID)

	rec := h.seen()
	require.Equal(t, "/groups/g-zzz", rec[0].Path)
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
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
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
	require.ErrorAs(t, err, &nf)
}

// TestCreateGroup_HappyPath pins POST /groups — name + members,
// response decoded as Group.
func TestCreateGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPost, rec.Method)
		require.Equal(t, "/groups", rec.Path)
		require.JSONEq(t, `{"name":"Eng","members":[{"userId":"u-1","role":"administrator"}]}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groupId":"g-new","name":"Eng","members":[{"userId":"u-1","role":"administrator"}]}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.CreateGroup(context.Background(), CreateGroupRequest{
		Name:    "Eng",
		Members: []GroupMember{{UserID: "u-1", Role: "administrator"}},
	})
	require.NoError(t, err)
	require.Equal(t, "g-new", got.GroupID)
}

func TestCreateGroup_EmptyName_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.CreateGroup(context.Background(), CreateGroupRequest{Name: ""})
	require.Error(t, err)
	require.Empty(t, h.seen())
}

func TestCreateGroup_DryRun_ReturnsRecord(t *testing.T) {
	t.Parallel()

	c := NewClient(fixtureToken())
	c.BaseURL = "https://favro.invalid"
	c.HTTPClient = &http.Client{Transport: &failingRoundTripper{t: t}}

	_, err := c.CreateGroup(WithDryRun(context.Background()), CreateGroupRequest{Name: "x"})
	require.ErrorIs(t, err, ErrDryRun)
	var rec *DryRunRecord
	require.ErrorAs(t, err, &rec)
	require.Equal(t, http.MethodPost, rec.Method)
}

func TestUpdateGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodPut, rec.Method)
		require.Equal(t, "/groups/g-1", rec.Path)
		require.JSONEq(t, `{"name":"renamed"}`, rec.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"groupId":"g-1","name":"renamed"}`))
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	got, err := c.UpdateGroup(context.Background(), "g-1", UpdateGroupRequest{Name: "renamed"})
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)
}

func TestUpdateGroup_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	_, err := c.UpdateGroup(context.Background(), "", UpdateGroupRequest{Name: "x"})
	require.ErrorIs(t, err, errMissingID)
	require.Empty(t, h.seen())
}

func TestDeleteGroup_HappyPath(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(rec recordedRequest, w http.ResponseWriter) {
		require.Equal(t, http.MethodDelete, rec.Method)
		require.Equal(t, "/groups/g-1", rec.Path)
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.NoError(t, c.DeleteGroup(context.Background(), "g-1"))
}

func TestDeleteGroup_EmptyID_NoNetworkCall(t *testing.T) {
	t.Parallel()

	h := &recordingHandler{respond: func(_ recordedRequest, _ http.ResponseWriter) {}}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := newTestClient(srv)

	require.ErrorIs(t, c.DeleteGroup(context.Background(), ""), errMissingID)
	require.Empty(t, h.seen())
}

// TestUpdateCard_CustomFieldsValuePassthrough pins that
// UpdateCardRequest.CustomFields marshals onto the wire in the
// per-type shapes documented under "Card custom field parameters"
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
		require.JSONEq(t, `{"customFields":[
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

	_, err := c.UpdateCard(context.Background(), "ci-1", UpdateCardRequest{
		CustomFields: []CardCustomFieldUpdate{
			{CustomFieldID: "cf-text", Value: "hello"},
			{CustomFieldID: "cf-num", Total: &num},
			{CustomFieldID: "cf-bool", Value: true},
			{CustomFieldID: "cf-select", Value: []string{"item-1"}},
			{CustomFieldID: "cf-rating", Total: &rating},
			{CustomFieldID: "cf-members", Members: &CustomFieldMembersUpdate{
				AddUserIDs:    []string{"u-1"},
				RemoveUserIDs: []string{"u-2"},
			}},
			{CustomFieldID: "cf-tags", Tags: &CustomFieldTagsUpdate{AddTagIDs: []string{"t-1"}}},
			{CustomFieldID: "cf-timeline", Timeline: &CustomFieldTimeline{
				StartDate: "2026-01-01",
				DueDate:   "2026-02-01",
			}},
			{CustomFieldID: "cf-link", Link: &CustomFieldLink{URL: "https://example.com", Text: "docs"}},
			{CustomFieldID: "cf-color", Color: "blue-300"},
			{CustomFieldID: "cf-time", AddUserReports: []CustomFieldTimeReport{
				{Value: &logged, Description: "work"},
			}},
		},
	})
	require.NoError(t, err)
}
