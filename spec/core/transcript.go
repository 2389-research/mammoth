// ABOUTME: Transcript types for agent/human communication within a spec.
// ABOUTME: Includes MessageKind, TranscriptMessage, and UserQuestion (3 variants).
package core

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// MessageKind categorizes transcript messages for rendering.
type MessageKind string

const (
	MessageKindChat         MessageKind = "Chat"
	MessageKindStepStarted  MessageKind = "StepStarted"
	MessageKindStepFinished MessageKind = "StepFinished"
)

// IsStep returns true if this is a step-related message kind.
func (k MessageKind) IsStep() bool {
	return k == MessageKindStepStarted || k == MessageKindStepFinished
}

// Prefix returns a display prefix for the message kind.
func (k MessageKind) Prefix() string {
	switch k {
	case MessageKindStepStarted:
		return "[step started] "
	case MessageKindStepFinished:
		return "[step finished] "
	default:
		return ""
	}
}

// TranscriptMessage represents a single message in the spec's transcript.
type TranscriptMessage struct {
	MessageID ulid.ULID   `json:"message_id"`
	Sender    string      `json:"sender"`
	Content   string      `json:"content"`
	Kind      MessageKind `json:"kind"`
	Timestamp time.Time   `json:"timestamp"`
}

// NewTranscriptMessage creates a Chat-kind transcript message with a fresh ULID.
func NewTranscriptMessage(sender, content string) TranscriptMessage {
	return TranscriptMessage{
		MessageID: NewULID(),
		Sender:    sender,
		Content:   content,
		Kind:      MessageKindChat,
		Timestamp: time.Now().UTC(),
	}
}

// UserQuestion is a tagged union with 3 variants: Boolean, MultipleChoice, Freeform.
// JSON serialization uses a "type" discriminator field.
type UserQuestion interface {
	QuestionType() string
	QuestionID() ulid.ULID
	questionSeal()
}

// BooleanQuestion asks the user a yes/no question.
type BooleanQuestion struct {
	QID      ulid.ULID `json:"question_id"`
	Question string    `json:"question"`
	Default  *bool     `json:"default,omitempty"`
}

func (q BooleanQuestion) QuestionType() string  { return "Boolean" }
func (q BooleanQuestion) QuestionID() ulid.ULID { return q.QID }
func (q BooleanQuestion) questionSeal()         {}

// MultipleChoiceQuestion asks the user to pick from a list.
type MultipleChoiceQuestion struct {
	QID        ulid.ULID `json:"question_id"`
	Question   string    `json:"question"`
	Choices    []string  `json:"choices"`
	AllowMulti bool      `json:"allow_multi"`
}

func (q MultipleChoiceQuestion) QuestionType() string  { return "MultipleChoice" }
func (q MultipleChoiceQuestion) QuestionID() ulid.ULID { return q.QID }
func (q MultipleChoiceQuestion) questionSeal()         {}

// FreeformQuestion asks the user for open-ended text input.
type FreeformQuestion struct {
	QID            ulid.ULID `json:"question_id"`
	Question       string    `json:"question"`
	Placeholder    *string   `json:"placeholder,omitempty"`
	ValidationHint *string   `json:"validation_hint,omitempty"`
}

func (q FreeformQuestion) QuestionType() string  { return "Freeform" }
func (q FreeformQuestion) QuestionID() ulid.ULID { return q.QID }
func (q FreeformQuestion) questionSeal()         {}

// MarshalUserQuestion serializes a UserQuestion with a "type" discriminator.
func MarshalUserQuestion(q UserQuestion) ([]byte, error) {
	if q == nil {
		return []byte("null"), nil
	}

	var raw json.RawMessage
	var err error

	switch v := q.(type) {
	case BooleanQuestion:
		raw, err = json.Marshal(v)
	case MultipleChoiceQuestion:
		raw, err = json.Marshal(v)
	case FreeformQuestion:
		raw, err = json.Marshal(v)
	default:
		return nil, fmt.Errorf("unknown UserQuestion type: %T", q)
	}
	if err != nil {
		return nil, err
	}

	// Inject "type" field
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	typeJSON, _ := json.Marshal(q.QuestionType())
	m["type"] = typeJSON
	return json.Marshal(m)
}

// UnmarshalUserQuestion deserializes a UserQuestion from JSON with a "type" discriminator.
func UnmarshalUserQuestion(data []byte) (UserQuestion, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal question type: %w", err)
	}

	switch envelope.Type {
	case "Boolean":
		var q BooleanQuestion
		if err := json.Unmarshal(data, &q); err != nil {
			return nil, err
		}
		return q, nil
	case "MultipleChoice":
		var q MultipleChoiceQuestion
		if err := json.Unmarshal(data, &q); err != nil {
			return nil, err
		}
		return q, nil
	case "Freeform":
		var q FreeformQuestion
		if err := json.Unmarshal(data, &q); err != nil {
			return nil, err
		}
		return q, nil
	default:
		return nil, fmt.Errorf("unknown question type: %q", envelope.Type)
	}
}
