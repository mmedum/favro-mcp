package favro

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

// Organization is a Favro organization. Fields outside this struct in
// the API response are ignored on decode (forward-compatible).
type Organization struct {
	OrganizationID string      `json:"organizationId"`
	Name           string      `json:"name"`
	SharedToUsers  []OrgMember `json:"sharedToUsers,omitempty"`
}

// OrgMember is a single user-role mapping inside an Organization.
type OrgMember struct {
	UserID string `json:"userId"`
	Role   string `json:"role,omitempty"`
}

// ListOrganizations returns one page of organizations the API token
// can see. page is 1-indexed; pass 0 for "first page".
//
// requestID is the Favro requestId echoed back by a prior page; pass
// it on every page > 0 so Favro routes the call to the same backend
// that holds the cursor (X-Favro-Backend-Identifier header). Empty
// requestID is fine on the first page.
//
// Pagination is intentionally not auto-aggregated — the caller is
// responsible for advancing pages. Auto-aggregation silently burns
// the per-organization rate-limit budget.
func (c *Client) ListOrganizations(ctx context.Context, page int, requestID string) (PageEnvelope[Organization], error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var opts []RequestOption
	if requestID != "" {
		opts = append(opts, WithHeader(headerRequestID, requestID))
	}
	var env PageEnvelope[Organization]
	if err := c.GetJSON(ctx, "/organizations", q, &env, opts...); err != nil {
		return PageEnvelope[Organization]{}, err
	}
	return env, nil
}

// errMissingID is returned by Get<Resource> methods when the caller
// passes an empty id. The MCP layer relies on the SDK's own schema
// validation (required fields), so this sentinel mainly catches
// direct in-process callers.
var errMissingID = errors.New("favro: id is required")

// GetOrganization returns a single organization by id. Returns
// *NotFoundError if no such organization exists for the active token.
func (c *Client) GetOrganization(ctx context.Context, organizationID string) (Organization, error) {
	if organizationID == "" {
		return Organization{}, errMissingID
	}
	var org Organization
	if err := c.GetJSON(ctx, "/organizations/"+url.PathEscape(organizationID), nil, &org); err != nil {
		return Organization{}, err
	}
	return org, nil
}
