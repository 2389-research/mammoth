// ABOUTME: Event is the envelope for all spec mutations, wrapping EventPayload variants.
// ABOUTME: 13 EventPayload variants with tagged union JSON serialization via "type" discriminator.
package core

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Event is the immutable envelope for a spec mutation.
type Event struct {
	EventID   uint64       `json:"event_id"`
	SpecID    ulid.ULID    `json:"spec_id"`
	Timestamp time.Time    `json:"timestamp"`
	Payload   EventPayload `json:"-"` // Custom marshal/unmarshal
}

// eventJSON is the wire format for Event.
type eventJSON struct {
	EventID   uint64          `json:"event_id"`
	SpecID    ulid.ULID       `json:"spec_id"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// MarshalJSON serializes the Event with its payload inlined.
func (e Event) MarshalJSON() ([]byte, error) {
	payloadJSON, err := MarshalEventPayload(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("marshal event payload: %w", err)
	}
	j := eventJSON{
		EventID:   e.EventID,
		SpecID:    e.SpecID,
		Timestamp: e.Timestamp,
		Payload:   payloadJSON,
	}
	return json.Marshal(j)
}

// UnmarshalJSON deserializes the Event with its payload.
func (e *Event) UnmarshalJSON(data []byte) error {
	var j eventJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	payload, err := UnmarshalEventPayload(j.Payload)
	if err != nil {
		return fmt.Errorf("unmarshal event payload: %w", err)
	}
	e.EventID = j.EventID
	e.SpecID = j.SpecID
	e.Timestamp = j.Timestamp
	e.Payload = payload
	return nil
}

// EventPayload is a tagged union representing the 13 event variants.
type EventPayload interface {
	EventPayloadType() string
	eventPayloadSeal()
}

// SpecCreatedPayload indicates a new spec was created.
type SpecCreatedPayload struct {
	Title    string `json:"title"`
	OneLiner string `json:"one_liner"`
	Goal     string `json:"goal"`
}

func (p SpecCreatedPayload) EventPayloadType() string { return "SpecCreated" }
func (p SpecCreatedPayload) eventPayloadSeal()        {}

// SpecCoreUpdatedPayload indicates spec metadata was updated.
type SpecCoreUpdatedPayload struct {
	Title           *string `json:"title,omitempty"`
	OneLiner        *string `json:"one_liner,omitempty"`
	Goal            *string `json:"goal,omitempty"`
	Description     *string `json:"description,omitempty"`
	Constraints     *string `json:"constraints,omitempty"`
	SuccessCriteria *string `json:"success_criteria,omitempty"`
	Risks           *string `json:"risks,omitempty"`
	Notes           *string `json:"notes,omitempty"`
}

func (p SpecCoreUpdatedPayload) EventPayloadType() string { return "SpecCoreUpdated" }
func (p SpecCoreUpdatedPayload) eventPayloadSeal()        {}

// CardCreatedPayload indicates a new card was created.
type CardCreatedPayload struct {
	Card Card `json:"card"`
}

func (p CardCreatedPayload) EventPayloadType() string { return "CardCreated" }
func (p CardCreatedPayload) eventPayloadSeal()        {}

// CardUpdatedPayload indicates a card was updated.
// Body uses OptionalField for 3-state semantics.
type CardUpdatedPayload struct {
	CardID   ulid.ULID             `json:"card_id"`
	Title    *string               `json:"title,omitempty"`
	Body     OptionalField[string] `json:"-"` // Custom marshal
	CardType *string               `json:"card_type,omitempty"`
	Refs     *[]string             `json:"refs,omitempty"`
}

func (p CardUpdatedPayload) EventPayloadType() string { return "CardUpdated" }
func (p CardUpdatedPayload) eventPayloadSeal()        {}

// cardUpdatedJSON is the wire format for CardUpdatedPayload.
type cardUpdatedJSON struct {
	Type     string           `json:"type"`
	CardID   ulid.ULID        `json:"card_id"`
	Title    *string          `json:"title,omitempty"`
	Body     *json.RawMessage `json:"body,omitempty"`
	CardType *string          `json:"card_type,omitempty"`
	Refs     *[]string        `json:"refs,omitempty"`
}

// CardMovedPayload indicates a card was moved to a new lane/position.
type CardMovedPayload struct {
	CardID ulid.ULID `json:"card_id"`
	Lane   string    `json:"lane"`
	Order  float64   `json:"order"`
}

func (p CardMovedPayload) EventPayloadType() string { return "CardMoved" }
func (p CardMovedPayload) eventPayloadSeal()        {}

// CardDeletedPayload indicates a card was removed.
type CardDeletedPayload struct {
	CardID ulid.ULID `json:"card_id"`
}

func (p CardDeletedPayload) EventPayloadType() string { return "CardDeleted" }
func (p CardDeletedPayload) eventPayloadSeal()        {}

// TranscriptAppendedPayload indicates a message was added to the transcript.
type TranscriptAppendedPayload struct {
	Message TranscriptMessage `json:"message"`
}

func (p TranscriptAppendedPayload) EventPayloadType() string { return "TranscriptAppended" }
func (p TranscriptAppendedPayload) eventPayloadSeal()        {}

// QuestionAskedPayload indicates a question was posed to the user.
type QuestionAskedPayload struct {
	Question UserQuestion `json:"-"` // Custom marshal
}

func (p QuestionAskedPayload) EventPayloadType() string { return "QuestionAsked" }
func (p QuestionAskedPayload) eventPayloadSeal()        {}

// QuestionAnsweredPayload indicates a pending question was answered.
type QuestionAnsweredPayload struct {
	QuestionID ulid.ULID `json:"question_id"`
	Answer     string    `json:"answer"`
}

func (p QuestionAnsweredPayload) EventPayloadType() string { return "QuestionAnswered" }
func (p QuestionAnsweredPayload) eventPayloadSeal()        {}

// AgentStepStartedPayload indicates an agent began a work step.
type AgentStepStartedPayload struct {
	AgentID     string `json:"agent_id"`
	Description string `json:"description"`
}

func (p AgentStepStartedPayload) EventPayloadType() string { return "AgentStepStarted" }
func (p AgentStepStartedPayload) eventPayloadSeal()        {}

// AgentStepFinishedPayload indicates an agent completed a work step.
type AgentStepFinishedPayload struct {
	AgentID     string `json:"agent_id"`
	DiffSummary string `json:"diff_summary"`
}

func (p AgentStepFinishedPayload) EventPayloadType() string { return "AgentStepFinished" }
func (p AgentStepFinishedPayload) eventPayloadSeal()        {}

// UndoAppliedPayload indicates a previous operation was reversed.
type UndoAppliedPayload struct {
	TargetEventID uint64         `json:"target_event_id"`
	InverseEvents []EventPayload `json:"-"` // Custom marshal
}

func (p UndoAppliedPayload) EventPayloadType() string { return "UndoApplied" }
func (p UndoAppliedPayload) eventPayloadSeal()        {}

// SnapshotWrittenPayload indicates a state snapshot was saved.
type SnapshotWrittenPayload struct {
	SnapshotID uint64 `json:"snapshot_id"`
}

func (p SnapshotWrittenPayload) EventPayloadType() string { return "SnapshotWritten" }
func (p SnapshotWrittenPayload) eventPayloadSeal()        {}

// MarshalEventPayload serializes an EventPayload with a "type" discriminator.
func MarshalEventPayload(p EventPayload) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("cannot marshal nil event payload")
	}

	switch v := p.(type) {
	case CardUpdatedPayload:
		return marshalCardUpdated(v)
	case QuestionAskedPayload:
		return marshalQuestionAsked(v)
	case UndoAppliedPayload:
		return marshalUndoApplied(v)
	default:
		return marshalTagged(p.EventPayloadType(), p)
	}
}

// UnmarshalEventPayload deserializes an EventPayload from JSON with a "type" discriminator.
func UnmarshalEventPayload(data []byte) (EventPayload, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal event payload type: %w", err)
	}

	switch envelope.Type {
	case "SpecCreated":
		var p SpecCreatedPayload
		return p, json.Unmarshal(data, &p)
	case "SpecCoreUpdated":
		var p SpecCoreUpdatedPayload
		return p, json.Unmarshal(data, &p)
	case "CardCreated":
		var p CardCreatedPayload
		return p, json.Unmarshal(data, &p)
	case "CardUpdated":
		return unmarshalCardUpdated(data)
	case "CardMoved":
		var p CardMovedPayload
		return p, json.Unmarshal(data, &p)
	case "CardDeleted":
		var p CardDeletedPayload
		return p, json.Unmarshal(data, &p)
	case "TranscriptAppended":
		var p TranscriptAppendedPayload
		return p, json.Unmarshal(data, &p)
	case "QuestionAsked":
		return unmarshalQuestionAsked(data)
	case "QuestionAnswered":
		var p QuestionAnsweredPayload
		return p, json.Unmarshal(data, &p)
	case "AgentStepStarted":
		var p AgentStepStartedPayload
		return p, json.Unmarshal(data, &p)
	case "AgentStepFinished":
		var p AgentStepFinishedPayload
		return p, json.Unmarshal(data, &p)
	case "UndoApplied":
		return unmarshalUndoApplied(data)
	case "SnapshotWritten":
		var p SnapshotWrittenPayload
		return p, json.Unmarshal(data, &p)
	default:
		return nil, fmt.Errorf("unknown event payload type: %q", envelope.Type)
	}
}

func marshalCardUpdated(p CardUpdatedPayload) ([]byte, error) {
	j := cardUpdatedJSON{
		Type:     "CardUpdated",
		CardID:   p.CardID,
		Title:    p.Title,
		CardType: p.CardType,
		Refs:     p.Refs,
	}
	if p.Body.Set {
		if p.Body.Valid {
			bodyJSON, _ := json.Marshal(p.Body.Value)
			raw := json.RawMessage(bodyJSON)
			j.Body = &raw
		} else {
			raw := json.RawMessage("null")
			j.Body = &raw
		}
	}
	return json.Marshal(j)
}

// unmarshalCardUpdated uses raw JSON map to distinguish "body":null from absent body,
// since *json.RawMessage with omitempty collapses both to nil on unmarshal.
func unmarshalCardUpdated(data []byte) (CardUpdatedPayload, error) {
	var j cardUpdatedJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return CardUpdatedPayload{}, err
	}

	p := CardUpdatedPayload{
		CardID:   j.CardID,
		Title:    j.Title,
		CardType: j.CardType,
		Refs:     j.Refs,
	}

	// Check the raw JSON map to distinguish absent body from null body,
	// since *json.RawMessage sets both to nil during standard unmarshal.
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return CardUpdatedPayload{}, err
	}
	if bodyRaw, present := rawMap["body"]; present {
		p.Body.Set = true
		if string(bodyRaw) == "null" {
			p.Body.Valid = false
		} else {
			p.Body.Valid = true
			if err := json.Unmarshal(bodyRaw, &p.Body.Value); err != nil {
				return CardUpdatedPayload{}, fmt.Errorf("unmarshal CardUpdated body: %w", err)
			}
		}
	}

	return p, nil
}

func marshalQuestionAsked(p QuestionAskedPayload) ([]byte, error) {
	qJSON, err := MarshalUserQuestion(p.Question)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{
		"type":     json.RawMessage(`"QuestionAsked"`),
		"question": qJSON,
	}
	return json.Marshal(m)
}

func unmarshalQuestionAsked(data []byte) (QuestionAskedPayload, error) {
	var raw struct {
		Question json.RawMessage `json:"question"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return QuestionAskedPayload{}, err
	}
	q, err := UnmarshalUserQuestion(raw.Question)
	if err != nil {
		return QuestionAskedPayload{}, err
	}
	return QuestionAskedPayload{Question: q}, nil
}

func marshalUndoApplied(p UndoAppliedPayload) ([]byte, error) {
	inverseJSON := make([]json.RawMessage, len(p.InverseEvents))
	for i, inv := range p.InverseEvents {
		data, err := MarshalEventPayload(inv)
		if err != nil {
			return nil, fmt.Errorf("marshal inverse event %d: %w", i, err)
		}
		inverseJSON[i] = data
	}
	m := map[string]any{
		"type":            "UndoApplied",
		"target_event_id": p.TargetEventID,
		"inverse_events":  inverseJSON,
	}
	return json.Marshal(m)
}

func unmarshalUndoApplied(data []byte) (UndoAppliedPayload, error) {
	var raw struct {
		TargetEventID uint64            `json:"target_event_id"`
		InverseEvents []json.RawMessage `json:"inverse_events"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return UndoAppliedPayload{}, err
	}

	inverseEvents := make([]EventPayload, len(raw.InverseEvents))
	for i, invData := range raw.InverseEvents {
		inv, err := UnmarshalEventPayload(invData)
		if err != nil {
			return UndoAppliedPayload{}, fmt.Errorf("unmarshal inverse event %d: %w", i, err)
		}
		inverseEvents[i] = inv
	}

	return UndoAppliedPayload{
		TargetEventID: raw.TargetEventID,
		InverseEvents: inverseEvents,
	}, nil
}
