// ABOUTME: Tests for SpecState.Apply() event-sourcing state reducer.
// ABOUTME: Ports all tests from barnstormer's state.rs to verify identical behavior.
package core_test

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/core"
)

// makeEvent builds an Event with the given ID, spec ID, and payload.
// Timestamp is set to the current time.
func makeEvent(eventID uint64, specID ulid.ULID, payload core.EventPayload) *core.Event {
	return &core.Event{
		EventID:   eventID,
		SpecID:    specID,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

func TestApplySpecCreatedSetsCoreFields(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	event := makeEvent(1, specID, core.SpecCreatedPayload{
		Title:    "My Spec",
		OneLiner: "A thing",
		Goal:     "Build it",
	})

	state.Apply(event)

	if state.Core == nil {
		t.Fatal("core should be set after SpecCreated")
	}
	if state.Core.SpecID != specID {
		t.Errorf("spec_id: got %s, want %s", state.Core.SpecID, specID)
	}
	if state.Core.Title != "My Spec" {
		t.Errorf("title: got %q, want %q", state.Core.Title, "My Spec")
	}
	if state.Core.OneLiner != "A thing" {
		t.Errorf("one_liner: got %q, want %q", state.Core.OneLiner, "A thing")
	}
	if state.Core.Goal != "Build it" {
		t.Errorf("goal: got %q, want %q", state.Core.Goal, "Build it")
	}
	if state.Core.Description != nil {
		t.Errorf("description should be nil, got %q", *state.Core.Description)
	}
	if state.LastEventID != 1 {
		t.Errorf("last_event_id: got %d, want 1", state.LastEventID)
	}
}

func TestApplySpecCoreUpdatedModifiesFields(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()

	// First create the spec
	state.Apply(makeEvent(1, specID, core.SpecCreatedPayload{
		Title:    "Original",
		OneLiner: "First",
		Goal:     "Initial goal",
	}))

	// Then update only title and description; leave one_liner and goal unchanged
	updatedTitle := "Updated Title"
	description := "A description"
	state.Apply(makeEvent(2, specID, core.SpecCoreUpdatedPayload{
		Title:       &updatedTitle,
		Description: &description,
	}))

	if state.Core == nil {
		t.Fatal("core should exist after update")
	}
	if state.Core.Title != "Updated Title" {
		t.Errorf("title: got %q, want %q", state.Core.Title, "Updated Title")
	}
	if state.Core.OneLiner != "First" {
		t.Errorf("one_liner should be unchanged: got %q, want %q", state.Core.OneLiner, "First")
	}
	if state.Core.Description == nil || *state.Core.Description != "A description" {
		t.Errorf("description: got %v, want %q", state.Core.Description, "A description")
	}
	if state.LastEventID != 2 {
		t.Errorf("last_event_id: got %d, want 2", state.LastEventID)
	}
}

func TestApplyCardCreatedAddsCard(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	card := core.NewCard("idea", "Test Card", "agent-1")
	cardID := card.CardID

	state.Apply(makeEvent(1, specID, core.CardCreatedPayload{Card: card}))

	if state.Cards.Len() != 1 {
		t.Fatalf("cards.Len(): got %d, want 1", state.Cards.Len())
	}
	got, ok := state.Cards.Get(cardID)
	if !ok {
		t.Fatal("card should be found by card_id")
	}
	if got.Title != "Test Card" {
		t.Errorf("card title: got %q, want %q", got.Title, "Test Card")
	}
}

func TestApplyCardUpdatedModifiesCard(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	card := core.NewCard("idea", "Original Title", "agent-1")
	cardID := card.CardID

	state.Apply(makeEvent(1, specID, core.CardCreatedPayload{Card: card}))

	newTitle := "Renamed Card"
	state.Apply(makeEvent(2, specID, core.CardUpdatedPayload{
		CardID:   cardID,
		Title:    &newTitle,
		Body:     core.Present("New body"),
		CardType: nil,
	}))

	got, ok := state.Cards.Get(cardID)
	if !ok {
		t.Fatal("card should still exist after update")
	}
	if got.Title != "Renamed Card" {
		t.Errorf("title: got %q, want %q", got.Title, "Renamed Card")
	}
	if got.Body == nil || *got.Body != "New body" {
		t.Errorf("body: got %v, want %q", got.Body, "New body")
	}
	if got.CardType != "idea" {
		t.Errorf("card_type should be unchanged: got %q, want %q", got.CardType, "idea")
	}
}

func TestApplyCardMovedChangesLaneAndOrder(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	card := core.NewCard("task", "Move Me", "human")
	cardID := card.CardID

	state.Apply(makeEvent(1, specID, core.CardCreatedPayload{Card: card}))

	state.Apply(makeEvent(2, specID, core.CardMovedPayload{
		CardID: cardID,
		Lane:   "Spec",
		Order:  3.5,
	}))

	got, ok := state.Cards.Get(cardID)
	if !ok {
		t.Fatal("card should exist after move")
	}
	if got.Lane != "Spec" {
		t.Errorf("lane: got %q, want %q", got.Lane, "Spec")
	}
	if got.Order != 3.5 {
		t.Errorf("order: got %f, want 3.5", got.Order)
	}
}

func TestApplyCardDeletedRemovesCard(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	card := core.NewCard("idea", "Delete Me", "human")
	cardID := card.CardID

	state.Apply(makeEvent(1, specID, core.CardCreatedPayload{Card: card}))
	if state.Cards.Len() != 1 {
		t.Fatalf("cards.Len() after create: got %d, want 1", state.Cards.Len())
	}

	state.Apply(makeEvent(2, specID, core.CardDeletedPayload{CardID: cardID}))
	if state.Cards.Len() != 0 {
		t.Errorf("cards.Len() after delete: got %d, want 0", state.Cards.Len())
	}
	if _, ok := state.Cards.Get(cardID); ok {
		t.Error("card should not be found after deletion")
	}
}

func TestApplyQuestionAskedSetsPending(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	defaultVal := true
	question := core.BooleanQuestion{
		QID:      core.NewULID(),
		Question: "Continue?",
		Default:  &defaultVal,
	}

	state.Apply(makeEvent(1, specID, core.QuestionAskedPayload{
		Question: question,
	}))

	if state.PendingQuestion == nil {
		t.Error("pending_question should be set after QuestionAsked")
	}
}

func TestApplyQuestionAnsweredClearsPending(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	qID := core.NewULID()
	question := core.BooleanQuestion{
		QID:      qID,
		Question: "Continue?",
	}

	state.Apply(makeEvent(1, specID, core.QuestionAskedPayload{
		Question: question,
	}))
	if state.PendingQuestion == nil {
		t.Fatal("pending_question should be set after QuestionAsked")
	}

	state.Apply(makeEvent(2, specID, core.QuestionAnsweredPayload{
		QuestionID: qID,
		Answer:     "Yes",
	}))
	if state.PendingQuestion != nil {
		t.Error("pending_question should be nil after QuestionAnswered")
	}
	// The answer should be appended to the transcript
	if len(state.Transcript) != 1 {
		t.Fatalf("transcript length: got %d, want 1", len(state.Transcript))
	}
	if state.Transcript[0].Content != "Yes" {
		t.Errorf("transcript content: got %q, want %q", state.Transcript[0].Content, "Yes")
	}
}

func TestUndoEntryCreatedOnCardMutation(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	card := core.NewCard("idea", "Test", "human")
	cardID := card.CardID

	// CardCreated should push an undo entry
	state.Apply(makeEvent(1, specID, core.CardCreatedPayload{Card: card}))
	if len(state.UndoStack) != 1 {
		t.Fatalf("undo_stack length after CardCreated: got %d, want 1", len(state.UndoStack))
	}
	if state.UndoStack[0].EventID != 1 {
		t.Errorf("undo_stack[0].event_id: got %d, want 1", state.UndoStack[0].EventID)
	}

	// CardUpdated should push another undo entry
	newTitle := "Renamed"
	state.Apply(makeEvent(2, specID, core.CardUpdatedPayload{
		CardID: cardID,
		Title:  &newTitle,
	}))
	if len(state.UndoStack) != 2 {
		t.Fatalf("undo_stack length after CardUpdated: got %d, want 2", len(state.UndoStack))
	}
	if state.UndoStack[1].EventID != 2 {
		t.Errorf("undo_stack[1].event_id: got %d, want 2", state.UndoStack[1].EventID)
	}

	// CardMoved should push another undo entry
	state.Apply(makeEvent(3, specID, core.CardMovedPayload{
		CardID: cardID,
		Lane:   "Plan",
		Order:  1.0,
	}))
	if len(state.UndoStack) != 3 {
		t.Fatalf("undo_stack length after CardMoved: got %d, want 3", len(state.UndoStack))
	}

	// CardDeleted should push another undo entry
	state.Apply(makeEvent(4, specID, core.CardDeletedPayload{CardID: cardID}))
	if len(state.UndoStack) != 4 {
		t.Fatalf("undo_stack length after CardDeleted: got %d, want 4", len(state.UndoStack))
	}
}

func TestUndoAppliedPopsUndoStack(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()

	// Create a card (pushes 1 undo entry)
	card := core.NewCard("idea", "Undo Test", "human")
	cardID := card.CardID
	state.Apply(makeEvent(1, specID, core.CardCreatedPayload{Card: card}))
	if len(state.UndoStack) != 1 {
		t.Fatalf("undo_stack should have 1 entry after card creation, got %d", len(state.UndoStack))
	}

	// Apply UndoApplied (should apply inverse and pop the entry)
	state.Apply(makeEvent(2, specID, core.UndoAppliedPayload{
		TargetEventID: 1,
		InverseEvents: []core.EventPayload{
			core.CardDeletedPayload{CardID: cardID},
		},
	}))

	if state.Cards.Len() != 0 {
		t.Errorf("card should be removed after undo, cards.Len() = %d", state.Cards.Len())
	}
	if len(state.UndoStack) != 0 {
		t.Errorf("undo_stack should be empty after UndoApplied, got %d", len(state.UndoStack))
	}
}

func TestApplyAgentStepStartedSetsStepStartedKind(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	state.Apply(makeEvent(1, specID, core.AgentStepStartedPayload{
		AgentID:     "manager-01HTEST",
		Description: "Manager reasoning step",
	}))

	if len(state.Transcript) != 1 {
		t.Fatalf("transcript length: got %d, want 1", len(state.Transcript))
	}
	if state.Transcript[0].Kind != core.MessageKindStepStarted {
		t.Errorf("kind: got %q, want %q", state.Transcript[0].Kind, core.MessageKindStepStarted)
	}
	if state.Transcript[0].Content != "Manager reasoning step" {
		t.Errorf("content: got %q, want %q", state.Transcript[0].Content, "Manager reasoning step")
	}
	// Content should NOT contain the prefix -- the prefix is for display only
	if state.Transcript[0].Content != "Manager reasoning step" {
		t.Error("content should not contain [step started] prefix")
	}
}

func TestApplyAgentStepFinishedSetsStepFinishedKind(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()
	state.Apply(makeEvent(1, specID, core.AgentStepFinishedPayload{
		AgentID:     "manager-01HTEST",
		DiffSummary: "Updated goal and added 3 cards",
	}))

	if len(state.Transcript) != 1 {
		t.Fatalf("transcript length: got %d, want 1", len(state.Transcript))
	}
	if state.Transcript[0].Kind != core.MessageKindStepFinished {
		t.Errorf("kind: got %q, want %q", state.Transcript[0].Kind, core.MessageKindStepFinished)
	}
	if state.Transcript[0].Content != "Updated goal and added 3 cards" {
		t.Errorf("content: got %q, want %q", state.Transcript[0].Content, "Updated goal and added 3 cards")
	}
	// Content should NOT contain the prefix -- the prefix is for display only
	if state.Transcript[0].Content != "Updated goal and added 3 cards" {
		t.Error("content should not contain [step finished] prefix")
	}
}

func TestApplyMultipleEventsBuildsFullState(t *testing.T) {
	state := core.NewSpecState()
	specID := core.NewULID()

	// Create a spec
	state.Apply(makeEvent(1, specID, core.SpecCreatedPayload{
		Title:    "Full Spec",
		OneLiner: "Complete test",
		Goal:     "Verify full state build",
	}))

	// Add two cards
	cardA := core.NewCard("idea", "Card A", "human")
	cardB := core.NewCard("task", "Card B", "agent-1")
	cardAID := cardA.CardID

	state.Apply(makeEvent(2, specID, core.CardCreatedPayload{Card: cardA}))
	state.Apply(makeEvent(3, specID, core.CardCreatedPayload{Card: cardB}))

	// Move card A
	state.Apply(makeEvent(4, specID, core.CardMovedPayload{
		CardID: cardAID,
		Lane:   "Plan",
		Order:  1.0,
	}))

	// Append transcript
	msg := core.NewTranscriptMessage("system", "Spec initialized")
	state.Apply(makeEvent(5, specID, core.TranscriptAppendedPayload{Message: msg}))

	// Verify full state
	if state.Core == nil {
		t.Fatal("core should be set")
	}
	if state.Core.Title != "Full Spec" {
		t.Errorf("core.title: got %q, want %q", state.Core.Title, "Full Spec")
	}
	if state.Cards.Len() != 2 {
		t.Errorf("cards.Len(): got %d, want 2", state.Cards.Len())
	}
	gotA, ok := state.Cards.Get(cardAID)
	if !ok {
		t.Fatal("card A should be in cards map")
	}
	if gotA.Lane != "Plan" {
		t.Errorf("card A lane: got %q, want %q", gotA.Lane, "Plan")
	}
	if len(state.Transcript) != 1 {
		t.Errorf("transcript length: got %d, want 1", len(state.Transcript))
	}
	if state.LastEventID != 5 {
		t.Errorf("last_event_id: got %d, want 5", state.LastEventID)
	}
	// 2 card creates + 1 move = 3 undo entries
	if len(state.UndoStack) != 3 {
		t.Errorf("undo_stack length: got %d, want 3", len(state.UndoStack))
	}
	expectedLanes := []string{"Ideas", "Plan", "Spec"}
	if len(state.Lanes) != len(expectedLanes) {
		t.Fatalf("lanes length: got %d, want %d", len(state.Lanes), len(expectedLanes))
	}
	for i, want := range expectedLanes {
		if state.Lanes[i] != want {
			t.Errorf("lanes[%d]: got %q, want %q", i, state.Lanes[i], want)
		}
	}
}
