// Package service is the orchestration between the MCP tools and the
// Favro client: resolving a name to an id, searching a scope, fanning
// out a card's references, and editing a description in place.
//
// It returns plain Go values and imports no MCP, which is what makes it
// testable without a protocol session and what keeps the tool layer
// thin enough to read as a list of tools. The caches live here too,
// because a cache that several tools share has to outlive any one of
// them.
package service

// Custom-field type strings as Favro returns them on CustomField.Type.
// Centralized so the read-side dispatch (formatCustomFieldValue) and
// write-side dispatch (setCFOptions) reference one source of truth —
// a typo in either site would silently desync the two layers.
//
// The set is drawn from two sources that do not agree, so both are
// carried:
//
//   - Favro's REST docs (https://favro.com/developer/, "Custom field
//     types").
//   - A live `GET /customfields` listing, which returns type strings
//     the docs never mention and, in one case, contradicts them.
//
// The contradiction is Vote: the docs call the type "Vote", live
// payloads say "Voting". Both are accepted and route to the same
// read formatter and write applicator, with the observed spelling
// treated as the real one.
const (
	// Confirmed against a live /customfields listing.
	CFTypeText           = "Text"
	CFTypeNumber         = "Number"
	CFTypeDate           = "Date"
	cfTypeDateCreated    = "Date created"
	CFTypeCheckbox       = "Checkbox"
	CFTypeLink           = "Link"
	CFTypeSingleSelect   = "Single select"
	CFTypeMultipleSelect = "Multiple select"
	CFTypeMembers        = "Members"
	CFTypeTags           = "Tags"
	CFTypeRating         = "Rating"
	CFTypeTimeline       = "Timeline"
	CFTypeVoting         = "Voting"
	cfTypeProgress       = "Progress"
	cfTypeRelations      = "Relations"
	cfTypeSequentialID   = "Sequential ID"

	// Documented but not yet observed live. Status is the docs' name
	// for what may be the same thing as Single select; Vote is the
	// docs' spelling of Voting. Time and Color have no observed
	// counterpart at all — they may need a Favro plan or feature this
	// organization doesn't have.
	CFTypeStatus = "Status"
	CFTypeVote   = "Vote"
	CFTypeTime   = "Time"
	CFTypeColor  = "Color"
)
