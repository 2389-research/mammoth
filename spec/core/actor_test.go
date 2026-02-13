// ABOUTME: Tests for SpecActorHandle goroutine-based actor.
// ABOUTME: Covers command processing, broadcasting, undo, error cases, and state recovery.
package core_test

import (
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/core"
)

// newTestSpecID returns a fresh ULID for use as a spec identifier in tests.
func newTestSpecID() ulid.ULID {
	return core.NewULID()
}

func TestSpawnActor_SendCommand(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	events, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Test Spec",
		OneLiner: "A test specification",
		Goal:     "Validate the actor",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Payload.EventPayloadType() != "SpecCreated" {
		t.Errorf("expected SpecCreated payload, got %s", events[0].Payload.EventPayloadType())
	}
}

func TestCreateSpec_ProducesSpecCreatedPayload_UpdatesState(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	events, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "My Spec",
		OneLiner: "Short description",
		Goal:     "Build something great",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	payload, ok := events[0].Payload.(core.SpecCreatedPayload)
	if !ok {
		t.Fatalf("expected SpecCreatedPayload, got %T", events[0].Payload)
	}
	if payload.Title != "My Spec" {
		t.Errorf("expected title 'My Spec', got %q", payload.Title)
	}
	if payload.OneLiner != "Short description" {
		t.Errorf("expected one_liner 'Short description', got %q", payload.OneLiner)
	}
	if payload.Goal != "Build something great" {
		t.Errorf("expected goal 'Build something great', got %q", payload.Goal)
	}

	// Verify state was updated
	handle.ReadState(func(s *core.SpecState) {
		if s.Core == nil {
			t.Fatal("expected Core to be populated after CreateSpec")
		}
		if s.Core.Title != "My Spec" {
			t.Errorf("state title: expected 'My Spec', got %q", s.Core.Title)
		}
		if s.Core.OneLiner != "Short description" {
			t.Errorf("state one_liner: expected 'Short description', got %q", s.Core.OneLiner)
		}
		if s.Core.Goal != "Build something great" {
			t.Errorf("state goal: expected 'Build something great', got %q", s.Core.Goal)
		}
		if s.Core.SpecID != specID {
			t.Errorf("state spec_id: expected %s, got %s", specID, s.Core.SpecID)
		}
		if s.LastEventID != 1 {
			t.Errorf("state last_event_id: expected 1, got %d", s.LastEventID)
		}
	})
}

func TestCreateCard_DefaultLaneIsIdeas(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	events, err := handle.SendCommand(core.CreateCardCommand{
		CardType:  "feature",
		Title:     "A new feature",
		CreatedBy: "agent-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	payload, ok := events[0].Payload.(core.CardCreatedPayload)
	if !ok {
		t.Fatalf("expected CardCreatedPayload, got %T", events[0].Payload)
	}
	if payload.Card.Lane != "Ideas" {
		t.Errorf("expected default lane 'Ideas', got %q", payload.Card.Lane)
	}
	if payload.Card.CardType != "feature" {
		t.Errorf("expected card_type 'feature', got %q", payload.Card.CardType)
	}
	if payload.Card.Title != "A new feature" {
		t.Errorf("expected title 'A new feature', got %q", payload.Card.Title)
	}
	if payload.Card.CreatedBy != "agent-1" {
		t.Errorf("expected created_by 'agent-1', got %q", payload.Card.CreatedBy)
	}

	// Verify the card landed in state
	handle.ReadState(func(s *core.SpecState) {
		if s.Cards.Len() != 1 {
			t.Fatalf("expected 1 card in state, got %d", s.Cards.Len())
		}
		card, ok := s.Cards.Get(payload.Card.CardID)
		if !ok {
			t.Fatal("card not found in state by its ID")
		}
		if card.Lane != "Ideas" {
			t.Errorf("state card lane: expected 'Ideas', got %q", card.Lane)
		}
	})
}

func TestCreateCard_ExplicitLane(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	lane := "Plan"
	events, err := handle.SendCommand(core.CreateCardCommand{
		CardType:  "task",
		Title:     "Planned task",
		Lane:      &lane,
		CreatedBy: "agent-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := events[0].Payload.(core.CardCreatedPayload)
	if payload.Card.Lane != "Plan" {
		t.Errorf("expected lane 'Plan', got %q", payload.Card.Lane)
	}
}

func TestUpdateCard_NonexistentCard_ReturnsCardNotFoundError(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	fakeCardID := core.NewULID()
	newTitle := "Updated Title"
	_, err := handle.SendCommand(core.UpdateCardCommand{
		CardID:    fakeCardID,
		Title:     &newTitle,
		UpdatedBy: "agent-1",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent card, got nil")
	}

	var cardErr *core.CardNotFoundError
	if !errors.As(err, &cardErr) {
		t.Fatalf("expected CardNotFoundError, got %T: %v", err, err)
	}
	if cardErr.CardID != fakeCardID {
		t.Errorf("expected card_id %s in error, got %s", fakeCardID, cardErr.CardID)
	}
}

func TestBroadcast_SubscriberReceivesEventsAfterCommand(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	sub := handle.Subscribe()

	_, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Broadcast Test",
		OneLiner: "Testing broadcast",
		Goal:     "Verify subscriber gets events",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The subscriber should receive the event
	select {
	case event := <-sub:
		if event.Payload.EventPayloadType() != "SpecCreated" {
			t.Errorf("expected SpecCreated, got %s", event.Payload.EventPayloadType())
		}
		if event.SpecID != specID {
			t.Errorf("expected spec_id %s, got %s", specID, event.SpecID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for broadcast event")
	}
}

func TestBroadcast_MultipleSubscribers(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	sub1 := handle.Subscribe()
	sub2 := handle.Subscribe()

	_, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Multi Sub Test",
		OneLiner: "Testing multiple subscribers",
		Goal:     "Both subs get events",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, sub := range []chan core.Event{sub1, sub2} {
		select {
		case event := <-sub:
			if event.Payload.EventPayloadType() != "SpecCreated" {
				t.Errorf("subscriber %d: expected SpecCreated, got %s", i, event.Payload.EventPayloadType())
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("subscriber %d: timed out waiting for broadcast event", i)
		}
	}
}

func TestAskQuestion_AlreadyPending_ReturnsErrQuestionAlreadyPending(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	question := core.FreeformQuestion{
		QID:      core.NewULID(),
		Question: "What should we build?",
	}

	// First question should succeed
	_, err := handle.SendCommand(core.AskQuestionCommand{Question: question})
	if err != nil {
		t.Fatalf("first AskQuestion: unexpected error: %v", err)
	}

	// Second question while first is pending should fail
	question2 := core.FreeformQuestion{
		QID:      core.NewULID(),
		Question: "Another question?",
	}
	_, err = handle.SendCommand(core.AskQuestionCommand{Question: question2})
	if !errors.Is(err, core.ErrQuestionAlreadyPending) {
		t.Fatalf("expected ErrQuestionAlreadyPending, got %v", err)
	}
}

func TestAskQuestion_AfterAnswer_AllowsNewQuestion(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	questionID := core.NewULID()
	question := core.FreeformQuestion{
		QID:      questionID,
		Question: "What should we build?",
	}

	// Ask the first question
	_, err := handle.SendCommand(core.AskQuestionCommand{Question: question})
	if err != nil {
		t.Fatalf("ask question: unexpected error: %v", err)
	}

	// Answer it
	_, err = handle.SendCommand(core.AnswerQuestionCommand{
		QuestionID: questionID,
		Answer:     "A web app",
	})
	if err != nil {
		t.Fatalf("answer question: unexpected error: %v", err)
	}

	// Now a new question should be allowed
	question2 := core.FreeformQuestion{
		QID:      core.NewULID(),
		Question: "What framework?",
	}
	_, err = handle.SendCommand(core.AskQuestionCommand{Question: question2})
	if err != nil {
		t.Fatalf("second AskQuestion after answer: unexpected error: %v", err)
	}
}

func TestEventIDs_ContinueFromRecoveredState(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	state.LastEventID = 50

	handle := core.SpawnActor(specID, state)

	events, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Recovered Spec",
		OneLiner: "Testing event ID continuity",
		Goal:     "Event IDs pick up from recovered state",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventID != 51 {
		t.Errorf("expected event_id 51 (last_event_id=50 + 1), got %d", events[0].EventID)
	}

	// Send another command to verify sequential incrementing
	events2, err := handle.SendCommand(core.AppendTranscriptCommand{
		Sender:  "agent-1",
		Content: "Hello",
	})
	if err != nil {
		t.Fatalf("unexpected error on second command: %v", err)
	}
	if events2[0].EventID != 52 {
		t.Errorf("expected event_id 52, got %d", events2[0].EventID)
	}
}

func TestUndo_ReversesCardCreation(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	// Create a card
	createEvents, err := handle.SendCommand(core.CreateCardCommand{
		CardType:  "feature",
		Title:     "Card to undo",
		CreatedBy: "agent-1",
	})
	if err != nil {
		t.Fatalf("create card: unexpected error: %v", err)
	}

	createdCard := createEvents[0].Payload.(core.CardCreatedPayload).Card

	// Verify the card exists in state
	handle.ReadState(func(s *core.SpecState) {
		if s.Cards.Len() != 1 {
			t.Fatalf("expected 1 card before undo, got %d", s.Cards.Len())
		}
	})

	// Undo the card creation
	undoEvents, err := handle.SendCommand(core.UndoCommand{})
	if err != nil {
		t.Fatalf("undo: unexpected error: %v", err)
	}
	if len(undoEvents) != 1 {
		t.Fatalf("expected 1 undo event, got %d", len(undoEvents))
	}

	undoPayload, ok := undoEvents[0].Payload.(core.UndoAppliedPayload)
	if !ok {
		t.Fatalf("expected UndoAppliedPayload, got %T", undoEvents[0].Payload)
	}
	if undoPayload.TargetEventID != createEvents[0].EventID {
		t.Errorf("undo target_event_id: expected %d, got %d",
			createEvents[0].EventID, undoPayload.TargetEventID)
	}

	// Verify the card is gone from state
	handle.ReadState(func(s *core.SpecState) {
		if s.Cards.Len() != 0 {
			t.Errorf("expected 0 cards after undo, got %d", s.Cards.Len())
		}
		_, found := s.Cards.Get(createdCard.CardID)
		if found {
			t.Error("card should not be found in state after undo")
		}
	})
}

func TestDoubleUndo_ReturnsErrNothingToUndo(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	// Create a card (pushes one undo entry)
	_, err := handle.SendCommand(core.CreateCardCommand{
		CardType:  "feature",
		Title:     "Only card",
		CreatedBy: "agent-1",
	})
	if err != nil {
		t.Fatalf("create card: unexpected error: %v", err)
	}

	// First undo should succeed
	_, err = handle.SendCommand(core.UndoCommand{})
	if err != nil {
		t.Fatalf("first undo: unexpected error: %v", err)
	}

	// Second undo should fail because the stack is empty
	_, err = handle.SendCommand(core.UndoCommand{})
	if !errors.Is(err, core.ErrNothingToUndo) {
		t.Fatalf("expected ErrNothingToUndo, got %v", err)
	}
}

func TestUndoOnEmptyStack_ReturnsErrNothingToUndo(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	_, err := handle.SendCommand(core.UndoCommand{})
	if !errors.Is(err, core.ErrNothingToUndo) {
		t.Fatalf("expected ErrNothingToUndo on fresh state, got %v", err)
	}
}

func TestReadState_ProvidesCurrent(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	// ReadState on empty state
	handle.ReadState(func(s *core.SpecState) {
		if s.Core != nil {
			t.Error("expected Core to be nil on fresh state")
		}
		if s.Cards.Len() != 0 {
			t.Errorf("expected 0 cards on fresh state, got %d", s.Cards.Len())
		}
		if len(s.Transcript) != 0 {
			t.Errorf("expected 0 transcript messages, got %d", len(s.Transcript))
		}
		if s.PendingQuestion != nil {
			t.Error("expected nil PendingQuestion on fresh state")
		}
		if len(s.UndoStack) != 0 {
			t.Errorf("expected 0 undo entries, got %d", len(s.UndoStack))
		}
		if s.LastEventID != 0 {
			t.Errorf("expected last_event_id 0, got %d", s.LastEventID)
		}
	})

	// Create spec, then read state again
	_, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Readable Spec",
		OneLiner: "For reading",
		Goal:     "Test ReadState",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handle.ReadState(func(s *core.SpecState) {
		if s.Core == nil {
			t.Fatal("expected Core to be populated after CreateSpec")
		}
		if s.Core.Title != "Readable Spec" {
			t.Errorf("expected title 'Readable Spec', got %q", s.Core.Title)
		}
		if s.LastEventID != 1 {
			t.Errorf("expected last_event_id 1, got %d", s.LastEventID)
		}
	})
}

func TestCreateCard_WithBody(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	body := "This is the card body"
	events, err := handle.SendCommand(core.CreateCardCommand{
		CardType:  "requirement",
		Title:     "Card with body",
		Body:      &body,
		CreatedBy: "agent-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := events[0].Payload.(core.CardCreatedPayload)
	if payload.Card.Body == nil {
		t.Fatal("expected card body to be set")
	}
	if *payload.Card.Body != body {
		t.Errorf("expected body %q, got %q", body, *payload.Card.Body)
	}
}

func TestMoveCard_NonexistentCard_ReturnsCardNotFoundError(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	_, err := handle.SendCommand(core.MoveCardCommand{
		CardID:    core.NewULID(),
		Lane:      "Plan",
		Order:     1.0,
		UpdatedBy: "agent-1",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent card, got nil")
	}

	var cardErr *core.CardNotFoundError
	if !errors.As(err, &cardErr) {
		t.Fatalf("expected CardNotFoundError, got %T: %v", err, err)
	}
}

func TestDeleteCard_NonexistentCard_ReturnsCardNotFoundError(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	_, err := handle.SendCommand(core.DeleteCardCommand{
		CardID:    core.NewULID(),
		UpdatedBy: "agent-1",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent card, got nil")
	}

	var cardErr *core.CardNotFoundError
	if !errors.As(err, &cardErr) {
		t.Fatalf("expected CardNotFoundError, got %T: %v", err, err)
	}
}

func TestUpdateSpecCore_WithoutCreateSpec_ReturnsErrSpecNotCreated(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	newTitle := "Updated Title"
	_, err := handle.SendCommand(core.UpdateSpecCoreCommand{
		Title: &newTitle,
	})
	if !errors.Is(err, core.ErrSpecNotCreated) {
		t.Fatalf("expected ErrSpecNotCreated, got %v", err)
	}
}

func TestAnswerQuestion_NoPending_ReturnsErrNoPendingQuestion(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	_, err := handle.SendCommand(core.AnswerQuestionCommand{
		QuestionID: core.NewULID(),
		Answer:     "Some answer",
	})
	if !errors.Is(err, core.ErrNoPendingQuestion) {
		t.Fatalf("expected ErrNoPendingQuestion, got %v", err)
	}
}

func TestAnswerQuestion_WrongID_ReturnsQuestionIDMismatchError(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	questionID := core.NewULID()
	question := core.BooleanQuestion{
		QID:      questionID,
		Question: "Is this right?",
	}

	_, err := handle.SendCommand(core.AskQuestionCommand{Question: question})
	if err != nil {
		t.Fatalf("ask question: unexpected error: %v", err)
	}

	wrongID := core.NewULID()
	_, err = handle.SendCommand(core.AnswerQuestionCommand{
		QuestionID: wrongID,
		Answer:     "yes",
	})
	if err == nil {
		t.Fatal("expected error for wrong question ID, got nil")
	}

	var mismatchErr *core.QuestionIDMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected QuestionIDMismatchError, got %T: %v", err, err)
	}
	if mismatchErr.Expected != questionID {
		t.Errorf("expected question ID %s, got %s", questionID, mismatchErr.Expected)
	}
	if mismatchErr.Got != wrongID {
		t.Errorf("expected wrong ID %s, got %s", wrongID, mismatchErr.Got)
	}
}

func TestMultipleCommandsSequential(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	// Create spec
	events1, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Sequential Test",
		OneLiner: "Testing sequential commands",
		Goal:     "Verify event IDs increment",
	})
	if err != nil {
		t.Fatalf("create spec: unexpected error: %v", err)
	}
	if events1[0].EventID != 1 {
		t.Errorf("first event_id: expected 1, got %d", events1[0].EventID)
	}

	// Create card
	events2, err := handle.SendCommand(core.CreateCardCommand{
		CardType:  "feature",
		Title:     "First Card",
		CreatedBy: "agent-1",
	})
	if err != nil {
		t.Fatalf("create card: unexpected error: %v", err)
	}
	if events2[0].EventID != 2 {
		t.Errorf("second event_id: expected 2, got %d", events2[0].EventID)
	}

	// Append transcript
	events3, err := handle.SendCommand(core.AppendTranscriptCommand{
		Sender:  "agent-1",
		Content: "Working on it",
	})
	if err != nil {
		t.Fatalf("append transcript: unexpected error: %v", err)
	}
	if events3[0].EventID != 3 {
		t.Errorf("third event_id: expected 3, got %d", events3[0].EventID)
	}

	// Verify final state
	handle.ReadState(func(s *core.SpecState) {
		if s.LastEventID != 3 {
			t.Errorf("last_event_id: expected 3, got %d", s.LastEventID)
		}
		if s.Core == nil {
			t.Fatal("expected Core to be set")
		}
		if s.Cards.Len() != 1 {
			t.Errorf("expected 1 card, got %d", s.Cards.Len())
		}
		if len(s.Transcript) != 1 {
			t.Errorf("expected 1 transcript message, got %d", len(s.Transcript))
		}
	})
}

func TestEventSpecID_MatchesActorSpecID(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	events, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Spec ID Check",
		OneLiner: "Verify spec_id on events",
		Goal:     "Events carry the actor's spec_id",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events[0].SpecID != specID {
		t.Errorf("event spec_id: expected %s, got %s", specID, events[0].SpecID)
	}
}

func TestEventTimestamp_IsReasonable(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	before := time.Now().UTC().Add(-1 * time.Second)
	events, err := handle.SendCommand(core.CreateSpecCommand{
		Title:    "Timestamp Check",
		OneLiner: "Verify timestamp",
		Goal:     "Events have reasonable timestamps",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now().UTC().Add(1 * time.Second)

	ts := events[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("event timestamp %v not within expected range [%v, %v]", ts, before, after)
	}
}

func TestAppendTranscript_AddsToState(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	events, err := handle.SendCommand(core.AppendTranscriptCommand{
		Sender:  "human",
		Content: "Hello, agent!",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload, ok := events[0].Payload.(core.TranscriptAppendedPayload)
	if !ok {
		t.Fatalf("expected TranscriptAppendedPayload, got %T", events[0].Payload)
	}
	if payload.Message.Sender != "human" {
		t.Errorf("expected sender 'human', got %q", payload.Message.Sender)
	}
	if payload.Message.Content != "Hello, agent!" {
		t.Errorf("expected content 'Hello, agent!', got %q", payload.Message.Content)
	}

	handle.ReadState(func(s *core.SpecState) {
		if len(s.Transcript) != 1 {
			t.Fatalf("expected 1 transcript message, got %d", len(s.Transcript))
		}
		if s.Transcript[0].Sender != "human" {
			t.Errorf("state transcript sender: expected 'human', got %q", s.Transcript[0].Sender)
		}
	})
}

func TestAgentStepCommands(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	// Start step
	events, err := handle.SendCommand(core.StartAgentStepCommand{
		AgentID:     "agent-1",
		Description: "Implementing feature X",
	})
	if err != nil {
		t.Fatalf("start step: unexpected error: %v", err)
	}
	startPayload, ok := events[0].Payload.(core.AgentStepStartedPayload)
	if !ok {
		t.Fatalf("expected AgentStepStartedPayload, got %T", events[0].Payload)
	}
	if startPayload.AgentID != "agent-1" {
		t.Errorf("expected agent_id 'agent-1', got %q", startPayload.AgentID)
	}

	// Finish step
	events2, err := handle.SendCommand(core.FinishAgentStepCommand{
		AgentID:     "agent-1",
		DiffSummary: "+10 lines, -3 lines",
	})
	if err != nil {
		t.Fatalf("finish step: unexpected error: %v", err)
	}
	finishPayload, ok := events2[0].Payload.(core.AgentStepFinishedPayload)
	if !ok {
		t.Fatalf("expected AgentStepFinishedPayload, got %T", events2[0].Payload)
	}
	if finishPayload.DiffSummary != "+10 lines, -3 lines" {
		t.Errorf("expected diff_summary '+10 lines, -3 lines', got %q", finishPayload.DiffSummary)
	}

	// Verify both messages in transcript
	handle.ReadState(func(s *core.SpecState) {
		if len(s.Transcript) != 2 {
			t.Fatalf("expected 2 transcript messages, got %d", len(s.Transcript))
		}
	})
}

func TestHandleSpecID_Accessible(t *testing.T) {
	specID := newTestSpecID()
	state := core.NewSpecState()
	handle := core.SpawnActor(specID, state)

	if handle.SpecID != specID {
		t.Errorf("handle.SpecID: expected %s, got %s", specID, handle.SpecID)
	}
}
