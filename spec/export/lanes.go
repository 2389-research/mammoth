// ABOUTME: Shared lane ordering and card grouping logic for all exporters.
// ABOUTME: Produces deterministic lane order: Ideas, Plan, Spec first, then extra lanes alphabetically.
package export

import (
	"sort"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

// defaultLanes defines the fixed-priority lanes that appear before any extras.
var defaultLanes = []string{"Ideas", "Plan", "Spec"}

// groupCardsByLane groups cards by lane name, sorting each group by (order, card_id string).
func groupCardsByLane(state *core.SpecState) map[string][]core.Card {
	byLane := make(map[string][]core.Card)
	state.Cards.Range(func(_ ulid.ULID, card core.Card) bool {
		byLane[card.Lane] = append(byLane[card.Lane], card)
		return true
	})
	// Sort each lane's cards by order, then card_id as tiebreaker
	for lane := range byLane {
		cards := byLane[lane]
		sort.SliceStable(cards, func(i, j int) bool {
			if cards[i].Order != cards[j].Order {
				return cards[i].Order < cards[j].Order
			}
			return cards[i].CardID.String() < cards[j].CardID.String()
		})
		byLane[lane] = cards
	}
	return byLane
}

// orderedLaneNames produces the deterministic lane ordering: Ideas, Plan, Spec
// first (if they exist in state.Lanes or have cards), then any additional lanes
// sorted alphabetically.
func orderedLaneNames(state *core.SpecState, cardsByLane map[string][]core.Card) []string {
	var lanes []string

	// Add default lanes that either are in state.Lanes or have cards
	for _, dl := range defaultLanes {
		_, hasCards := cardsByLane[dl]
		isDefault := containsString(state.Lanes, dl)
		if hasCards || isDefault {
			lanes = append(lanes, dl)
		}
	}

	// Collect additional non-default lanes that have cards, sorted alphabetically
	var extraLanes []string
	for lane := range cardsByLane {
		if !containsString(defaultLanes, lane) {
			extraLanes = append(extraLanes, lane)
		}
	}
	sort.Strings(extraLanes)
	lanes = append(lanes, extraLanes...)

	return lanes
}

// containsString checks if a string slice contains a given value.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
