// ABOUTME: Tests for the Markdown exporter covering header, optional fields, lane ordering, and card rendering.
// ABOUTME: Uses external test package (export_test) to test the public API surface.
package export_test

import (
	"strings"
	"testing"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/core/export"
)

func makeStateWithCore() *core.SpecState {
	sc := core.NewSpecCore("Test Spec", "A test specification", "Verify the markdown exporter")
	state := core.NewSpecState()
	state.Core = &sc
	return state
}

func makeCard(cardType, title, lane string, order float64, createdBy string) core.Card {
	now := time.Now().UTC()
	return core.Card{
		CardID:    core.NewULID(),
		CardType:  cardType,
		Title:     title,
		Lane:      lane,
		Order:     order,
		Refs:      []string{},
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: createdBy,
		UpdatedBy: createdBy,
	}
}

func TestExportMarkdownIncludesTitleAndGoal(t *testing.T) {
	state := makeStateWithCore()
	md := export.ExportMarkdown(state)

	if !strings.Contains(md, "# Test Spec") {
		t.Error("missing title")
	}
	if !strings.Contains(md, "> A test specification") {
		t.Error("missing one_liner")
	}
	if !strings.Contains(md, "## Goal") {
		t.Error("missing Goal section")
	}
	if !strings.Contains(md, "Verify the markdown exporter") {
		t.Error("missing goal text")
	}
}

func TestExportMarkdownGroupsCardsByLane(t *testing.T) {
	state := makeStateWithCore()

	cardIdeas := makeCard("idea", "Brainstorm", "Ideas", 1.0, "human")
	cardPlan := makeCard("plan", "Roadmap", "Plan", 1.0, "human")
	cardSpec := makeCard("task", "Shipped", "Spec", 1.0, "human")

	state.Cards.Set(cardIdeas.CardID, cardIdeas)
	state.Cards.Set(cardPlan.CardID, cardPlan)
	state.Cards.Set(cardSpec.CardID, cardSpec)

	md := export.ExportMarkdown(state)

	// Verify lane sections exist
	if !strings.Contains(md, "## Ideas") {
		t.Error("missing Ideas lane")
	}
	if !strings.Contains(md, "## Plan") {
		t.Error("missing Plan lane")
	}
	if !strings.Contains(md, "## Spec") {
		t.Error("missing Spec lane")
	}

	// Verify cards are under the correct lane by checking ordering in the output
	ideasPos := strings.Index(md, "## Ideas")
	planPos := strings.Index(md, "## Plan")
	specPos := strings.Index(md, "## Spec")
	brainstormPos := strings.Index(md, "### Brainstorm (idea)")
	roadmapPos := strings.Index(md, "### Roadmap (plan)")
	shippedPos := strings.Index(md, "### Shipped (task)")

	if brainstormPos <= ideasPos || brainstormPos >= planPos {
		t.Error("Brainstorm card not between Ideas and Plan lanes")
	}
	if roadmapPos <= planPos || roadmapPos >= specPos {
		t.Error("Roadmap card not between Plan and Spec lanes")
	}
	if shippedPos <= specPos {
		t.Error("Shipped card not after Spec lane")
	}
}

func TestExportMarkdownOrdersCardsByOrderField(t *testing.T) {
	state := makeStateWithCore()

	cardB := makeCard("idea", "Second Idea", "Ideas", 2.0, "human")
	cardA := makeCard("idea", "First Idea", "Ideas", 1.0, "human")
	cardC := makeCard("idea", "Third Idea", "Ideas", 3.0, "human")

	// Insert in non-sorted order
	state.Cards.Set(cardC.CardID, cardC)
	state.Cards.Set(cardA.CardID, cardA)
	state.Cards.Set(cardB.CardID, cardB)

	md := export.ExportMarkdown(state)

	posFirst := strings.Index(md, "### First Idea")
	posSecond := strings.Index(md, "### Second Idea")
	posThird := strings.Index(md, "### Third Idea")

	if posFirst >= posSecond {
		t.Error("First Idea should come before Second Idea")
	}
	if posSecond >= posThird {
		t.Error("Second Idea should come before Third Idea")
	}
}

func TestExportMarkdownIncludesOptionalFields(t *testing.T) {
	state := makeStateWithCore()
	desc := "A detailed description"
	constr := "Must be fast"
	sc := "All tests pass"
	risks := "Scope creep"
	notes := "Remember to review"
	state.Core.Description = &desc
	state.Core.Constraints = &constr
	state.Core.SuccessCriteria = &sc
	state.Core.Risks = &risks
	state.Core.Notes = &notes

	md := export.ExportMarkdown(state)

	if !strings.Contains(md, "## Description") {
		t.Error("missing Description section")
	}
	if !strings.Contains(md, "A detailed description") {
		t.Error("missing description text")
	}
	if !strings.Contains(md, "## Constraints") {
		t.Error("missing Constraints section")
	}
	if !strings.Contains(md, "Must be fast") {
		t.Error("missing constraints text")
	}
	if !strings.Contains(md, "## Success Criteria") {
		t.Error("missing Success Criteria section")
	}
	if !strings.Contains(md, "All tests pass") {
		t.Error("missing success criteria text")
	}
	if !strings.Contains(md, "## Risks") {
		t.Error("missing Risks section")
	}
	if !strings.Contains(md, "Scope creep") {
		t.Error("missing risks text")
	}
	if !strings.Contains(md, "## Notes") {
		t.Error("missing Notes section")
	}
	if !strings.Contains(md, "Remember to review") {
		t.Error("missing notes text")
	}
}

func TestExportMarkdownOmitsEmptyOptionalFields(t *testing.T) {
	state := makeStateWithCore()
	md := export.ExportMarkdown(state)

	// Optional fields are nil, so their sections should not appear
	if strings.Contains(md, "## Description") {
		t.Error("Description section should not appear when nil")
	}
	if strings.Contains(md, "## Constraints") {
		t.Error("Constraints section should not appear when nil")
	}
	if strings.Contains(md, "## Success Criteria") {
		t.Error("Success Criteria section should not appear when nil")
	}
	if strings.Contains(md, "## Risks") {
		t.Error("Risks section should not appear when nil")
	}
	if strings.Contains(md, "## Notes") {
		t.Error("Notes section should not appear when nil")
	}
}

func TestExportMarkdownDeterministic(t *testing.T) {
	state := makeStateWithCore()

	cardA := makeCard("idea", "Alpha", "Ideas", 1.0, "human")
	cardB := makeCard("task", "Beta", "Plan", 2.0, "agent")

	state.Cards.Set(cardA.CardID, cardA)
	state.Cards.Set(cardB.CardID, cardB)

	md1 := export.ExportMarkdown(state)
	md2 := export.ExportMarkdown(state)

	if md1 != md2 {
		t.Error("Markdown export must be deterministic")
	}
}

func TestExportMarkdownExtraLanesAppearAlphabeticallyAfterDefaults(t *testing.T) {
	state := makeStateWithCore()

	cardZ := makeCard("idea", "Zulu Card", "Zulu", 1.0, "human")
	cardA := makeCard("idea", "Alpha Card", "Alpha", 1.0, "human")
	cardIdeas := makeCard("idea", "Idea Card", "Ideas", 1.0, "human")

	state.Cards.Set(cardZ.CardID, cardZ)
	state.Cards.Set(cardA.CardID, cardA)
	state.Cards.Set(cardIdeas.CardID, cardIdeas)

	md := export.ExportMarkdown(state)

	ideasPos := strings.Index(md, "## Ideas")
	planPos := strings.Index(md, "## Plan")
	specPos := strings.Index(md, "## Spec")
	alphaPos := strings.Index(md, "## Alpha")
	zuluPos := strings.Index(md, "## Zulu")

	if ideasPos == -1 || planPos == -1 || specPos == -1 || alphaPos == -1 || zuluPos == -1 {
		t.Fatalf("Missing lane section in output:\n%s", md)
	}

	// Default lanes first in order
	if ideasPos >= planPos {
		t.Error("Ideas should come before Plan")
	}
	if planPos >= specPos {
		t.Error("Plan should come before Spec")
	}
	// Extra lanes alphabetically after defaults
	if specPos >= alphaPos {
		t.Error("Spec should come before Alpha (extra lane)")
	}
	if alphaPos >= zuluPos {
		t.Error("Alpha should come before Zulu")
	}
}

func TestExportMarkdownCardWithBodyAndRefs(t *testing.T) {
	state := makeStateWithCore()

	card := makeCard("idea", "Rich Card", "Ideas", 1.0, "human")
	body := "This card has a body."
	card.Body = &body
	card.Refs = []string{"ref-1", "ref-2"}
	state.Cards.Set(card.CardID, card)

	md := export.ExportMarkdown(state)

	if !strings.Contains(md, "This card has a body.") {
		t.Error("missing card body")
	}
	if !strings.Contains(md, "Refs: ref-1, ref-2") {
		t.Error("missing card refs")
	}
	if !strings.Contains(md, "Created by: human at") {
		t.Error("missing card creation info")
	}
}
