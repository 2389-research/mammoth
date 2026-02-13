// ABOUTME: SpecState is the materialized state of a spec, built by replaying events.
// ABOUTME: The Apply method is a pattern-matching state reducer that folds events into state.
package core

import (
	"encoding/json"
	"fmt"

	"github.com/oklog/ulid/v2"
)

// UndoEntry stores the inverse operations needed to revert a mutation.
type UndoEntry struct {
	EventID uint64         `json:"event_id"`
	Inverse []EventPayload `json:"-"` // Custom marshal for EventPayload slice
}

// undoEntryJSON is the wire format for UndoEntry.
type undoEntryJSON struct {
	EventID uint64            `json:"event_id"`
	Inverse []json.RawMessage `json:"inverse"`
}

// MarshalJSON serializes the UndoEntry with properly typed inverse events.
func (u UndoEntry) MarshalJSON() ([]byte, error) {
	inverseJSON := make([]json.RawMessage, len(u.Inverse))
	for i, inv := range u.Inverse {
		data, err := MarshalEventPayload(inv)
		if err != nil {
			return nil, fmt.Errorf("marshal inverse event %d: %w", i, err)
		}
		inverseJSON[i] = data
	}
	return json.Marshal(undoEntryJSON{
		EventID: u.EventID,
		Inverse: inverseJSON,
	})
}

// UnmarshalJSON deserializes the UndoEntry with properly typed inverse events.
func (u *UndoEntry) UnmarshalJSON(data []byte) error {
	var j undoEntryJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	u.EventID = j.EventID
	u.Inverse = make([]EventPayload, len(j.Inverse))
	for i, raw := range j.Inverse {
		inv, err := UnmarshalEventPayload(raw)
		if err != nil {
			return fmt.Errorf("unmarshal inverse event %d: %w", i, err)
		}
		u.Inverse[i] = inv
	}
	return nil
}

// SpecState is the full materialized state of a spec, built by replaying events.
type SpecState struct {
	Core            *SpecCore                    `json:"core"`
	Cards           *OrderedMap[ulid.ULID, Card] `json:"cards"`
	Transcript      []TranscriptMessage          `json:"transcript"`
	PendingQuestion UserQuestion                 `json:"-"` // Custom marshal
	UndoStack       []UndoEntry                  `json:"undo_stack"`
	LastEventID     uint64                       `json:"last_event_id"`
	Lanes           []string                     `json:"lanes"`
}

// specStateJSON is the wire format for SpecState, used for custom unmarshal.
type specStateJSON struct {
	Core            *SpecCore                  `json:"core"`
	Cards           map[string]json.RawMessage `json:"cards"`
	Transcript      []TranscriptMessage        `json:"transcript"`
	PendingQuestion json.RawMessage            `json:"pending_question,omitempty"`
	UndoStack       []UndoEntry                `json:"undo_stack"`
	LastEventID     uint64                     `json:"last_event_id"`
	Lanes           []string                   `json:"lanes"`
}

// UnmarshalJSON deserializes SpecState, handling the OrderedMap[ULID,Card] cards field
// and the UserQuestion interface for pending_question.
func (s *SpecState) UnmarshalJSON(data []byte) error {
	var j specStateJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}

	s.Core = j.Core
	s.Transcript = j.Transcript
	s.UndoStack = j.UndoStack
	s.LastEventID = j.LastEventID
	s.Lanes = j.Lanes

	// Rebuild OrderedMap from map[string]Card
	s.Cards = NewOrderedMap[ulid.ULID, Card]()
	for keyStr, raw := range j.Cards {
		id, err := ulid.Parse(keyStr)
		if err != nil {
			return fmt.Errorf("parse card key %q: %w", keyStr, err)
		}
		var card Card
		if err := json.Unmarshal(raw, &card); err != nil {
			return fmt.Errorf("unmarshal card %q: %w", keyStr, err)
		}
		s.Cards.Set(id, card)
	}

	// Unmarshal PendingQuestion if present and non-null
	if len(j.PendingQuestion) > 0 && string(j.PendingQuestion) != "null" {
		q, err := UnmarshalUserQuestion(j.PendingQuestion)
		if err != nil {
			return fmt.Errorf("unmarshal pending_question: %w", err)
		}
		s.PendingQuestion = q
	}

	return nil
}

// MarshalJSON serializes SpecState, including pending_question as a typed JSON field.
func (s SpecState) MarshalJSON() ([]byte, error) {
	type specStateMarshal struct {
		Core            *SpecCore                    `json:"core"`
		Cards           *OrderedMap[ulid.ULID, Card] `json:"cards"`
		Transcript      []TranscriptMessage          `json:"transcript"`
		PendingQuestion json.RawMessage              `json:"pending_question"`
		UndoStack       []UndoEntry                  `json:"undo_stack"`
		LastEventID     uint64                       `json:"last_event_id"`
		Lanes           []string                     `json:"lanes"`
	}

	pqJSON, err := MarshalUserQuestion(s.PendingQuestion)
	if err != nil {
		return nil, fmt.Errorf("marshal pending_question: %w", err)
	}

	m := specStateMarshal{
		Core:            s.Core,
		Cards:           s.Cards,
		Transcript:      s.Transcript,
		PendingQuestion: pqJSON,
		UndoStack:       s.UndoStack,
		LastEventID:     s.LastEventID,
		Lanes:           s.Lanes,
	}
	return json.Marshal(m)
}

// NewSpecState creates an empty SpecState with default lanes.
func NewSpecState() *SpecState {
	return &SpecState{
		Cards:      NewOrderedMap[ulid.ULID, Card](),
		Transcript: []TranscriptMessage{},
		UndoStack:  []UndoEntry{},
		Lanes:      []string{"Ideas", "Plan", "Spec"},
	}
}

// Apply folds a single event into this state. Reversible card mutations push
// undo entries. This is the heart of the event-sourcing reducer.
func (s *SpecState) Apply(event *Event) {
	s.LastEventID = event.EventID

	switch p := event.Payload.(type) {
	case SpecCreatedPayload:
		s.Core = &SpecCore{
			SpecID:    event.SpecID,
			Title:     p.Title,
			OneLiner:  p.OneLiner,
			Goal:      p.Goal,
			CreatedAt: event.Timestamp,
			UpdatedAt: event.Timestamp,
		}

	case SpecCoreUpdatedPayload:
		if s.Core != nil {
			if p.Title != nil {
				s.Core.Title = *p.Title
			}
			if p.OneLiner != nil {
				s.Core.OneLiner = *p.OneLiner
			}
			if p.Goal != nil {
				s.Core.Goal = *p.Goal
			}
			if p.Description != nil {
				s.Core.Description = p.Description
			}
			if p.Constraints != nil {
				s.Core.Constraints = p.Constraints
			}
			if p.SuccessCriteria != nil {
				s.Core.SuccessCriteria = p.SuccessCriteria
			}
			if p.Risks != nil {
				s.Core.Risks = p.Risks
			}
			if p.Notes != nil {
				s.Core.Notes = p.Notes
			}
			s.Core.UpdatedAt = event.Timestamp
		}

	case CardCreatedPayload:
		inverse := []EventPayload{
			CardDeletedPayload{CardID: p.Card.CardID},
		}
		s.UndoStack = append(s.UndoStack, UndoEntry{
			EventID: event.EventID,
			Inverse: inverse,
		})
		s.Cards.Set(p.Card.CardID, p.Card)

	case CardUpdatedPayload:
		card, ok := s.Cards.Get(p.CardID)
		if ok {
			// Build inverse from current values before mutating
			inversePayload := CardUpdatedPayload{
				CardID: p.CardID,
			}
			if p.Title != nil {
				old := card.Title
				inversePayload.Title = &old
			}
			if p.Body.Set {
				if card.Body != nil {
					inversePayload.Body = Present(*card.Body)
				} else {
					inversePayload.Body = Null[string]()
				}
			}
			if p.CardType != nil {
				old := card.CardType
				inversePayload.CardType = &old
			}
			if p.Refs != nil {
				oldRefs := make([]string, len(card.Refs))
				copy(oldRefs, card.Refs)
				inversePayload.Refs = &oldRefs
			}

			s.UndoStack = append(s.UndoStack, UndoEntry{
				EventID: event.EventID,
				Inverse: []EventPayload{inversePayload},
			})

			// Apply mutations
			if p.Title != nil {
				card.Title = *p.Title
			}
			if p.Body.Set {
				if p.Body.Valid {
					card.Body = &p.Body.Value
				} else {
					card.Body = nil
				}
			}
			if p.CardType != nil {
				card.CardType = *p.CardType
			}
			if p.Refs != nil {
				card.Refs = *p.Refs
			}
			card.UpdatedAt = event.Timestamp
			s.Cards.Set(p.CardID, card)
		}

	case CardMovedPayload:
		card, ok := s.Cards.Get(p.CardID)
		if ok {
			inverse := []EventPayload{
				CardMovedPayload{
					CardID: p.CardID,
					Lane:   card.Lane,
					Order:  card.Order,
				},
			}
			s.UndoStack = append(s.UndoStack, UndoEntry{
				EventID: event.EventID,
				Inverse: inverse,
			})

			card.Lane = p.Lane
			card.Order = p.Order
			card.UpdatedAt = event.Timestamp
			s.Cards.Set(p.CardID, card)
		}

	case CardDeletedPayload:
		card, ok := s.Cards.Get(p.CardID)
		if ok {
			inverse := []EventPayload{
				CardCreatedPayload{Card: card},
			}
			s.UndoStack = append(s.UndoStack, UndoEntry{
				EventID: event.EventID,
				Inverse: inverse,
			})
			s.Cards.Delete(p.CardID)
		}

	case TranscriptAppendedPayload:
		s.Transcript = append(s.Transcript, p.Message)

	case QuestionAskedPayload:
		s.PendingQuestion = p.Question

	case QuestionAnsweredPayload:
		s.PendingQuestion = nil
		s.Transcript = append(s.Transcript, TranscriptMessage{
			MessageID: p.QuestionID,
			Sender:    "human",
			Content:   p.Answer,
			Kind:      MessageKindChat,
			Timestamp: event.Timestamp,
		})

	case AgentStepStartedPayload:
		s.Transcript = append(s.Transcript, TranscriptMessage{
			MessageID: NewULID(),
			Sender:    p.AgentID,
			Content:   p.Description,
			Kind:      MessageKindStepStarted,
			Timestamp: event.Timestamp,
		})

	case AgentStepFinishedPayload:
		s.Transcript = append(s.Transcript, TranscriptMessage{
			MessageID: NewULID(),
			Sender:    p.AgentID,
			Content:   p.DiffSummary,
			Kind:      MessageKindStepFinished,
			Timestamp: event.Timestamp,
		})

	case UndoAppliedPayload:
		// Apply inverse events without pushing new undo entries
		for _, inversePayload := range p.InverseEvents {
			syntheticEvent := &Event{
				EventID:   event.EventID,
				SpecID:    event.SpecID,
				Timestamp: event.Timestamp,
				Payload:   inversePayload,
			}
			s.applyWithoutUndo(syntheticEvent)
		}
		if len(s.UndoStack) > 0 {
			s.UndoStack = s.UndoStack[:len(s.UndoStack)-1]
		}

	case SnapshotWrittenPayload:
		// No-op on state
	}
}

// applyWithoutUndo applies an event's payload effects without pushing undo entries.
// Used internally for applying inverse events during undo.
func (s *SpecState) applyWithoutUndo(event *Event) {
	switch p := event.Payload.(type) {
	case CardCreatedPayload:
		s.Cards.Set(p.Card.CardID, p.Card)

	case CardUpdatedPayload:
		card, ok := s.Cards.Get(p.CardID)
		if ok {
			if p.Title != nil {
				card.Title = *p.Title
			}
			if p.Body.Set {
				if p.Body.Valid {
					card.Body = &p.Body.Value
				} else {
					card.Body = nil
				}
			}
			if p.CardType != nil {
				card.CardType = *p.CardType
			}
			if p.Refs != nil {
				card.Refs = *p.Refs
			}
			card.UpdatedAt = event.Timestamp
			s.Cards.Set(p.CardID, card)
		}

	case CardMovedPayload:
		card, ok := s.Cards.Get(p.CardID)
		if ok {
			card.Lane = p.Lane
			card.Order = p.Order
			card.UpdatedAt = event.Timestamp
			s.Cards.Set(p.CardID, card)
		}

	case CardDeletedPayload:
		s.Cards.Delete(p.CardID)

	case SpecCreatedPayload:
		s.Core = &SpecCore{
			SpecID:    event.SpecID,
			Title:     p.Title,
			OneLiner:  p.OneLiner,
			Goal:      p.Goal,
			CreatedAt: event.Timestamp,
			UpdatedAt: event.Timestamp,
		}

	case SpecCoreUpdatedPayload:
		if s.Core != nil {
			if p.Title != nil {
				s.Core.Title = *p.Title
			}
			if p.OneLiner != nil {
				s.Core.OneLiner = *p.OneLiner
			}
			if p.Goal != nil {
				s.Core.Goal = *p.Goal
			}
			if p.Description != nil {
				s.Core.Description = p.Description
			}
			if p.Constraints != nil {
				s.Core.Constraints = p.Constraints
			}
			if p.SuccessCriteria != nil {
				s.Core.SuccessCriteria = p.SuccessCriteria
			}
			if p.Risks != nil {
				s.Core.Risks = p.Risks
			}
			if p.Notes != nil {
				s.Core.Notes = p.Notes
			}
			s.Core.UpdatedAt = event.Timestamp
		}

	case UndoAppliedPayload, SnapshotWrittenPayload:
		// Skip: undo-of-undo should not happen, snapshot is a no-op

	default:
		// Transcript, question, agent step events are not reversible
		// and should not appear as inverse events. Ignore them defensively.
	}
}
