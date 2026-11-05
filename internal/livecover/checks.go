package livecover

// Check is one check the live driver's -writes run makes. A code comment
// cites it by its name, as it cites a step's, so the driver names a
// check by one of the constants below and never by a string.
type Check int

// The checks the -writes run makes, in the order it makes them.
const (
	CheckTagDeleteWithoutConfirm Check = iota
	CheckTagStillThere
	CheckTagDeleteWithConfirm
	CheckTagGone
	CheckPublicCollectionCreate
	CheckRefusedCollectionNotCreated
	CheckWidenWithAnotherName
	CheckCollectionStillUsers
	CheckWidenOpensAndReadsBack
	CheckCollectionReadsOrganization
	CheckUpdateNarrowsToUsers
	CheckCollectionDeleteWithoutConfirm
	CheckCollectionDeleteWithConfirm
	CheckCollectionGone
	CheckNothingAsked

	checkCount
)

// checkNames is each check's name, by the check.
var checkNames = [checkCount]string{
	CheckTagDeleteWithoutConfirm:        "favro_delete_tag without confirm is [blocked]",
	CheckTagStillThere:                  "the tag is still there",
	CheckTagDeleteWithConfirm:           "favro_delete_tag with confirm deletes it",
	CheckTagGone:                        "the tag is gone",
	CheckPublicCollectionCreate:         "favro_create_collection with public_sharing public is [blocked]",
	CheckRefusedCollectionNotCreated:    "the refused collection was not created",
	CheckWidenWithAnotherName:           "favro_widen_collection_sharing with another name is [conflict]",
	CheckCollectionStillUsers:           "the collection is still shared with users only",
	CheckWidenOpensAndReadsBack:         "favro_widen_collection_sharing opens it to the organization and reads it back",
	CheckCollectionReadsOrganization:    "the collection reads back as organization",
	CheckUpdateNarrowsToUsers:           "favro_update_collection narrows it to users again",
	CheckCollectionDeleteWithoutConfirm: "favro_delete_collection without confirm is [blocked]",
	CheckCollectionDeleteWithConfirm:    "favro_delete_collection with confirm deletes it",
	CheckCollectionGone:                 "the collection is gone",
	CheckNothingAsked:                   "the server asked the person nothing",
}

// Name is what the driver prints on the check's ok or FAIL line.
func (c Check) Name() string { return checkNames[c] }

// Names is every name a code comment can cite as a live step: each
// step's, then each -writes check's.
func Names() []string {
	steps := Steps()
	names := make([]string, 0, len(steps)+int(checkCount))
	for _, s := range steps {
		names = append(names, s.Name)
	}
	for c := range checkCount {
		names = append(names, c.Name())
	}
	return names
}
