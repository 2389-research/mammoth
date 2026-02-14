// ABOUTME: Goroutine-based actor for processing spec commands and broadcasting events.
// ABOUTME: Provides SpecActorHandle for sending commands, subscribing to events, and reading state.
package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// EventBroadcaster provides a fan-out mechanism for events to multiple subscribers.
// Each subscriber gets a buffered channel. Broadcast is non-blocking (drops if full).
type EventBroadcaster struct {
	mu          sync.RWMutex
	subscribers []chan Event
}

// NewEventBroadcaster creates a broadcaster with no initial subscribers.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{}
}

// Subscribe creates a new buffered channel for receiving broadcast events.
func (b *EventBroadcaster) Subscribe() chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 4096)
	b.subscribers = append(b.subscribers, ch)
	return ch
}

// Unsubscribe removes a channel from the subscriber list and closes it.
func (b *EventBroadcaster) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, sub := range b.subscribers {
		if sub == ch {
			b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Broadcast sends an event to all subscribers. Non-blocking: drops if a subscriber's buffer is full.
func (b *EventBroadcaster) Broadcast(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Drop if subscriber buffer is full
		}
	}
}

// commandMessage pairs a Command with a reply channel for the result.
type commandMessage struct {
	cmd   Command
	reply chan commandResult
}

// commandResult is the result of processing a command.
type commandResult struct {
	events []Event
	err    error
}

// SpecActorHandle is the public interface for interacting with a spec actor.
// It is safe for concurrent use.
type SpecActorHandle struct {
	cmdCh       chan commandMessage
	broadcaster *EventBroadcaster
	state       *SpecState
	mu          sync.RWMutex // protects state
	SpecID      ulid.ULID
}

// SendCommand sends a command to the actor and waits for the result.
func (h *SpecActorHandle) SendCommand(cmd Command) ([]Event, error) {
	reply := make(chan commandResult, 1)
	msg := commandMessage{cmd: cmd, reply: reply}

	select {
	case h.cmdCh <- msg:
	default:
		return nil, ErrActorBusy
	}

	result := <-reply
	return result.events, result.err
}

// Subscribe returns a channel that receives broadcast events.
func (h *SpecActorHandle) Subscribe() chan Event {
	return h.broadcaster.Subscribe()
}

// Unsubscribe removes a channel from the broadcast subscriber list and closes it.
func (h *SpecActorHandle) Unsubscribe(ch chan Event) {
	h.broadcaster.Unsubscribe(ch)
}

// ReadState calls the given function with a read lock on the current state.
// The function should not modify the state or hold references after returning.
func (h *SpecActorHandle) ReadState(fn func(s *SpecState)) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	fn(h.state)
}

// SpawnActor creates a new SpecActor goroutine and returns the handle.
func SpawnActor(specID ulid.ULID, initialState *SpecState) *SpecActorHandle {
	cmdCh := make(chan commandMessage, 64)
	broadcaster := NewEventBroadcaster()

	handle := &SpecActorHandle{
		cmdCh:       cmdCh,
		broadcaster: broadcaster,
		state:       initialState,
		SpecID:      specID,
	}

	actor := &specActor{
		handle:      handle,
		cmdCh:       cmdCh,
		nextEventID: initialState.LastEventID + 1,
		specID:      specID,
	}

	go actor.run()

	return handle
}

// specActor is the internal goroutine that processes commands sequentially.
type specActor struct {
	handle      *SpecActorHandle
	cmdCh       chan commandMessage
	nextEventID uint64
	specID      ulid.ULID
}

func (a *specActor) run() {
	for msg := range a.cmdCh {
		result := a.processCommand(msg.cmd)
		msg.reply <- result
	}
}

func (a *specActor) processCommand(cmd Command) commandResult {
	events, err := a.commandToEvents(cmd)
	if err != nil {
		return commandResult{err: err}
	}

	// Apply events to state under write lock
	a.handle.mu.Lock()
	for i := range events {
		a.handle.state.Apply(&events[i])
	}
	a.handle.mu.Unlock()

	// Broadcast events to subscribers
	for _, event := range events {
		a.handle.broadcaster.Broadcast(event)
	}

	return commandResult{events: events}
}

// commandToEvents validates a command against the current state and converts it to events.
func (a *specActor) commandToEvents(cmd Command) ([]Event, error) {
	a.handle.mu.RLock()
	state := a.handle.state
	var payloads []EventPayload

	switch c := cmd.(type) {
	case CreateSpecCommand:
		payloads = []EventPayload{
			SpecCreatedPayload(c),
		}

	case UpdateSpecCoreCommand:
		if state.Core == nil {
			a.handle.mu.RUnlock()
			return nil, ErrSpecNotCreated
		}
		payloads = []EventPayload{
			SpecCoreUpdatedPayload(c),
		}

	case CreateCardCommand:
		now := time.Now().UTC()
		lane := "Ideas"
		if c.Lane != nil {
			lane = *c.Lane
		}
		card := Card{
			CardID:    NewULID(),
			CardType:  c.CardType,
			Title:     c.Title,
			Body:      c.Body,
			Lane:      lane,
			Order:     0.0,
			Refs:      []string{},
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: c.CreatedBy,
			UpdatedBy: c.CreatedBy,
		}
		payloads = []EventPayload{
			CardCreatedPayload{Card: card},
		}

	case UpdateCardCommand:
		if _, ok := state.Cards.Get(c.CardID); !ok {
			a.handle.mu.RUnlock()
			return nil, &CardNotFoundError{CardID: c.CardID}
		}
		payloads = []EventPayload{
			CardUpdatedPayload(c),
		}

	case MoveCardCommand:
		if _, ok := state.Cards.Get(c.CardID); !ok {
			a.handle.mu.RUnlock()
			return nil, &CardNotFoundError{CardID: c.CardID}
		}
		payloads = []EventPayload{
			CardMovedPayload(c),
		}

	case DeleteCardCommand:
		if _, ok := state.Cards.Get(c.CardID); !ok {
			a.handle.mu.RUnlock()
			return nil, &CardNotFoundError{CardID: c.CardID}
		}
		payloads = []EventPayload{
			CardDeletedPayload(c),
		}

	case AppendTranscriptCommand:
		msg := NewTranscriptMessage(c.Sender, c.Content)
		payloads = []EventPayload{
			TranscriptAppendedPayload{Message: msg},
		}

	case AskQuestionCommand:
		if state.PendingQuestion != nil {
			a.handle.mu.RUnlock()
			return nil, ErrQuestionAlreadyPending
		}
		payloads = []EventPayload{
			QuestionAskedPayload(c),
		}

	case AnswerQuestionCommand:
		if state.PendingQuestion == nil {
			a.handle.mu.RUnlock()
			return nil, ErrNoPendingQuestion
		}
		pendingID := state.PendingQuestion.QuestionID()
		if pendingID != c.QuestionID {
			a.handle.mu.RUnlock()
			return nil, &QuestionIDMismatchError{Expected: pendingID, Got: c.QuestionID}
		}
		payloads = []EventPayload{
			QuestionAnsweredPayload(c),
		}

	case StartAgentStepCommand:
		payloads = []EventPayload{
			AgentStepStartedPayload(c),
		}

	case FinishAgentStepCommand:
		payloads = []EventPayload{
			AgentStepFinishedPayload(c),
		}

	case UndoCommand:
		if len(state.UndoStack) == 0 {
			a.handle.mu.RUnlock()
			return nil, ErrNothingToUndo
		}
		entry := state.UndoStack[len(state.UndoStack)-1]
		inverseCopy := make([]EventPayload, len(entry.Inverse))
		copy(inverseCopy, entry.Inverse)
		payloads = []EventPayload{
			UndoAppliedPayload{
				TargetEventID: entry.EventID,
				InverseEvents: inverseCopy,
			},
		}

	default:
		a.handle.mu.RUnlock()
		return nil, fmt.Errorf("%w: %T", ErrUnknownCommand, cmd)
	}

	a.handle.mu.RUnlock()

	// Create event envelopes
	now := time.Now().UTC()
	events := make([]Event, len(payloads))
	for i, payload := range payloads {
		events[i] = Event{
			EventID:   a.nextEventID,
			SpecID:    a.specID,
			Timestamp: now,
			Payload:   payload,
		}
		a.nextEventID++
	}

	return events, nil
}
