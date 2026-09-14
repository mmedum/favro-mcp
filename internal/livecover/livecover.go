// Package livecover is what "the live driver covers the surface" means.
//
// It holds the steps, so that the driver that runs them and the gate
// that checks them are reading the same list. The alternative — a gate
// that parses the driver's source — makes the claim depend on a parser
// agreeing with a program, and the two drift the first time somebody
// writes a step in a loop.
//
// What decays here is the claim, not the code. A driver covers the
// surface on the day it is written and stops the moment the surface
// grows: the standard records one repository where the same assertion
// ran at 14 of 28 tools, and another where a later phase added 67 tool
// options with no step. So coverage is measured against the binary's
// own published schema, per option and not per tool, and the gate fails
// on anything no step exercises.
package livecover

import (
	"fmt"
	"sort"
)

// Step is one call the live driver makes.
type Step struct {
	// Tool is the MCP tool name.
	Tool string
	// Args are the arguments to send. Placeholder values are resolved
	// at run time — see Placeholder.
	Args map[string]any
	// Why says what this step is for, and is printed in the transcript.
	// A step whose purpose is not obvious from its arguments is a step
	// nobody can tell has regressed.
	Why string
	// ExpectError marks a step that is supposed to fail, and names the
	// error class it must fail with. Exercising an error path is
	// coverage too: §6.2's vocabulary is only worth something if the
	// classes come back from a real API.
	ExpectError string
}

// Placeholder is an argument value the driver fills in from earlier
// results, because the ids in a real organization are not known until
// it has read some.
type Placeholder string

// The values the driver knows how to resolve. Anything else in an
// argument is sent literally.
const (
	AnyCollectionID   Placeholder = "{collection_id}"
	AnyWidgetCommonID Placeholder = "{widget_common_id}"
	AnyColumnID       Placeholder = "{column_id}"
	AnyCardID         Placeholder = "{card_id}"
	AnyCardCommonID   Placeholder = "{card_common_id}"
	AnyUserID         Placeholder = "{user_id}"
	AnyTagID          Placeholder = "{tag_id}"
	AnyCustomFieldID  Placeholder = "{custom_field_id}"
	AnyGroupID        Placeholder = "{group_id}"
	AnyCommentID      Placeholder = "{comment_id}"
	AnyOrganizationID Placeholder = "{organization_id}"
	AnyTaskID         Placeholder = "{task_id}"
	AnyTaskListID     Placeholder = "{task_list_id}"
	// AnySequentialID is a number rather than an id string — the
	// integer inside a reference like the ones people paste from the
	// UI. The driver keeps a placeholder's value at its original type,
	// so this arrives as an integer and not as "42".
	AnySequentialID Placeholder = "{sequential_id}"
)

// Placeholders is every value the driver resolves, for the test that
// requires each one to be both resolvable and used.
var Placeholders = []Placeholder{
	AnyCollectionID, AnyWidgetCommonID, AnyColumnID, AnyCardID,
	AnyCardCommonID, AnyUserID, AnyTagID, AnyCustomFieldID,
	AnyGroupID, AnyCommentID, AnyOrganizationID,
	AnyTaskID, AnyTaskListID, AnySequentialID,
}

// Coverage is what the gate reports.
type Coverage struct {
	// Tools is every tool the schema publishes.
	Tools int
	// ToolsCovered is how many have at least one step.
	ToolsCovered int
	// Options is every input property across every tool.
	Options int
	// OptionsCovered is how many a step actually sets.
	OptionsCovered int
	// MissingTools are the tools no step calls.
	MissingTools []string
	// MissingOptions are "tool.option" pairs no step sets.
	MissingOptions []string
}

// Shortfall renders the gap as a sentence, or empty when there is none.
func (c Coverage) Shortfall() string {
	if len(c.MissingTools) == 0 && len(c.MissingOptions) == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d tools and %d of %d options have no step",
		len(c.MissingTools), c.Tools, len(c.MissingOptions), c.Options)
}

// Measure compares the steps against a tool surface.
//
// schema is tool name -> input property names, which is what the
// binary's own --dump-schemas output reduces to. Passing it in rather
// than reading it here keeps this package free of file formats and
// makes the comparison testable against a surface somebody wrote down.
func Measure(steps []Step, schema map[string][]string, waived map[string]string) Coverage {
	calledTool := map[string]bool{}
	setOption := map[string]bool{}
	for _, s := range steps {
		calledTool[s.Tool] = true
		for arg := range s.Args {
			setOption[s.Tool+"."+arg] = true
		}
	}

	c := Coverage{Tools: len(schema)}
	for tool, options := range schema {
		if calledTool[tool] {
			c.ToolsCovered++
		} else if _, ok := waived[tool]; !ok {
			c.MissingTools = append(c.MissingTools, tool)
		}
		for _, opt := range options {
			c.Options++
			key := tool + "." + opt
			switch {
			case setOption[key]:
				c.OptionsCovered++
			case waived[key] != "":
			case waived[tool] != "":
			default:
				c.MissingOptions = append(c.MissingOptions, key)
			}
		}
	}
	sort.Strings(c.MissingTools)
	sort.Strings(c.MissingOptions)
	return c
}
