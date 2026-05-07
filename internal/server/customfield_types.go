package server

// Custom-field type strings as Favro returns them on CustomField.Type.
// Centralized so the read-side dispatch (formatCustomFieldValue) and
// write-side dispatch (setCFOptions) reference one source of truth —
// a typo in either site would silently desync the two layers.
const (
	cfTypeText           = "Text"
	cfTypeNumber         = "Number"
	cfTypeDate           = "Date"
	cfTypeDateCreated    = "Date created"
	cfTypeCheckbox       = "Checkbox"
	cfTypeLink           = "Link"
	cfTypeSingleSelect   = "Single select"
	cfTypeMultipleSelect = "Multiple select"
	cfTypeStatus         = "Status"
	cfTypeMembers        = "Members"
	cfTypeRating         = "Rating"
	cfTypeTimeline       = "Timeline"
	cfTypeVoting         = "Voting"
	cfTypeProgress       = "Progress"
	cfTypeTags           = "Tags"
	cfTypeSequentialID   = "Sequential ID"
	cfTypeRelations      = "Relations"
)
