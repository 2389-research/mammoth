// ABOUTME: Exports a SpecState as a deterministic Markdown document.
// ABOUTME: Sections follow spec Section 9.1 ordering: header, optional fields, then lanes with cards.
package export

import (
	"fmt"
	"strings"

	"github.com/2389-research/mammoth/spec/core"
)

// ExportMarkdown renders a SpecState as a Markdown string with deterministic ordering.
//
// Lane ordering: Ideas, Plan, Spec first (in that order), then any other
// lanes sorted alphabetically. Cards within each lane are ordered by their
// order field (float64), with card_id string comparison as a tiebreaker.
func ExportMarkdown(state *core.SpecState) string {
	var out strings.Builder

	if state.Core != nil {
		c := state.Core
		fmt.Fprintf(&out, "# %s\n", c.Title)
		fmt.Fprintln(&out)
		fmt.Fprintf(&out, "> %s\n", c.OneLiner)
		fmt.Fprintln(&out)
		fmt.Fprintln(&out, "## Goal")
		fmt.Fprintln(&out)
		fmt.Fprintln(&out, c.Goal)

		if c.Description != nil {
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, "## Description")
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, *c.Description)
		}

		if c.Constraints != nil {
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, "## Constraints")
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, *c.Constraints)
		}

		if c.SuccessCriteria != nil {
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, "## Success Criteria")
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, *c.SuccessCriteria)
		}

		if c.Risks != nil {
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, "## Risks")
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, *c.Risks)
		}

		if c.Notes != nil {
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, "## Notes")
			fmt.Fprintln(&out)
			fmt.Fprintln(&out, *c.Notes)
		}
	}

	// Group cards by lane
	cardsByLane := groupCardsByLane(state)

	// Determine which lanes to show
	orderedLanes := orderedLaneNames(state, cardsByLane)

	if len(orderedLanes) > 0 {
		fmt.Fprintln(&out)
		fmt.Fprintln(&out, "---")

		for _, lane := range orderedLanes {
			fmt.Fprintln(&out)
			fmt.Fprintf(&out, "## %s\n", lane)

			if cards, ok := cardsByLane[lane]; ok {
				for _, card := range cards {
					fmt.Fprintln(&out)
					fmt.Fprintf(&out, "### %s (%s)\n", card.Title, card.CardType)

					if card.Body != nil {
						fmt.Fprintln(&out)
						fmt.Fprintln(&out, *card.Body)
					}

					if len(card.Refs) > 0 {
						fmt.Fprintln(&out)
						fmt.Fprintf(&out, "Refs: %s\n", strings.Join(card.Refs, ", "))
					}

					fmt.Fprintf(&out, "Created by: %s at %s\n",
						card.CreatedBy,
						card.CreatedAt.Format("2006-01-02T15:04:05Z"),
					)
				}
			}
		}
	}

	return out.String()
}
