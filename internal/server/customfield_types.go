package server

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
	cfTypeText           = "Text"
	cfTypeNumber         = "Number"
	cfTypeDate           = "Date"
	cfTypeDateCreated    = "Date created"
	cfTypeCheckbox       = "Checkbox"
	cfTypeLink           = "Link"
	cfTypeSingleSelect   = "Single select"
	cfTypeMultipleSelect = "Multiple select"
	cfTypeMembers        = "Members"
	cfTypeTags           = "Tags"
	cfTypeRating         = "Rating"
	cfTypeTimeline       = "Timeline"
	cfTypeVoting         = "Voting"
	cfTypeProgress       = "Progress"
	cfTypeRelations      = "Relations"
	cfTypeSequentialID   = "Sequential ID"

	// Documented but not yet observed live. Status is the docs' name
	// for what may be the same thing as Single select; Vote is the
	// docs' spelling of Voting. Time and Color have no observed
	// counterpart at all — they may need a Favro plan or feature this
	// organization doesn't have.
	cfTypeStatus = "Status"
	cfTypeVote   = "Vote"
	cfTypeTime   = "Time"
	cfTypeColor  = "Color"
)
