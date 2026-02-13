// ABOUTME: Command is a tagged union representing all mutations to a spec.
// ABOUTME: 12 variants with custom JSON marshal/unmarshal using "type" discriminator.
package core

import (
	"encoding/json"
	"fmt"

	"github.com/oklog/ulid/v2"
)

// Command represents a mutation intent for a spec. Tagged union with 12 variants.
type Command interface {
	CommandType() string
	commandSeal()
}

// CreateSpecCommand creates a new spec with required fields.
type CreateSpecCommand struct {
	Title    string `json:"title"`
	OneLiner string `json:"one_liner"`
	Goal     string `json:"goal"`
}

func (c CreateSpecCommand) CommandType() string { return "CreateSpec" }
func (c CreateSpecCommand) commandSeal()        {}

// UpdateSpecCoreCommand updates optional spec metadata fields.
type UpdateSpecCoreCommand struct {
	Title           *string `json:"title,omitempty"`
	OneLiner        *string `json:"one_liner,omitempty"`
	Goal            *string `json:"goal,omitempty"`
	Description     *string `json:"description,omitempty"`
	Constraints     *string `json:"constraints,omitempty"`
	SuccessCriteria *string `json:"success_criteria,omitempty"`
	Risks           *string `json:"risks,omitempty"`
	Notes           *string `json:"notes,omitempty"`
}

func (c UpdateSpecCoreCommand) CommandType() string { return "UpdateSpecCore" }
func (c UpdateSpecCoreCommand) commandSeal()        {}

// CreateCardCommand creates a new card on the board.
type CreateCardCommand struct {
	CardType  string  `json:"card_type"`
	Title     string  `json:"title"`
	Body      *string `json:"body,omitempty"`
	Lane      *string `json:"lane,omitempty"`
	CreatedBy string  `json:"created_by"`
}

func (c CreateCardCommand) CommandType() string { return "CreateCard" }
func (c CreateCardCommand) commandSeal()        {}

// UpdateCardCommand updates an existing card's fields.
// Body uses OptionalField for 3-state semantics (absent/null/value).
type UpdateCardCommand struct {
	CardID    ulid.ULID             `json:"card_id"`
	Title     *string               `json:"title,omitempty"`
	Body      OptionalField[string] `json:"-"` // Custom marshal handles this
	CardType  *string               `json:"card_type,omitempty"`
	Refs      *[]string             `json:"refs,omitempty"`
	UpdatedBy string                `json:"updated_by"`
}

func (c UpdateCardCommand) CommandType() string { return "UpdateCard" }
func (c UpdateCardCommand) commandSeal()        {}

// updateCardJSON is the wire format for UpdateCardCommand.
type updateCardJSON struct {
	Type      string           `json:"type"`
	CardID    ulid.ULID        `json:"card_id"`
	Title     *string          `json:"title,omitempty"`
	Body      *json.RawMessage `json:"body,omitempty"`
	CardType  *string          `json:"card_type,omitempty"`
	Refs      *[]string        `json:"refs,omitempty"`
	UpdatedBy string           `json:"updated_by"`
}

// MoveCardCommand moves a card to a different lane/position.
type MoveCardCommand struct {
	CardID    ulid.ULID `json:"card_id"`
	Lane      string    `json:"lane"`
	Order     float64   `json:"order"`
	UpdatedBy string    `json:"updated_by"`
}

func (c MoveCardCommand) CommandType() string { return "MoveCard" }
func (c MoveCardCommand) commandSeal()        {}

// DeleteCardCommand removes a card from the board.
type DeleteCardCommand struct {
	CardID    ulid.ULID `json:"card_id"`
	UpdatedBy string    `json:"updated_by"`
}

func (c DeleteCardCommand) CommandType() string { return "DeleteCard" }
func (c DeleteCardCommand) commandSeal()        {}

// AppendTranscriptCommand adds a message to the transcript.
type AppendTranscriptCommand struct {
	Sender  string `json:"sender"`
	Content string `json:"content"`
}

func (c AppendTranscriptCommand) CommandType() string { return "AppendTranscript" }
func (c AppendTranscriptCommand) commandSeal()        {}

// AskQuestionCommand presents a question to the human user.
type AskQuestionCommand struct {
	Question UserQuestion `json:"-"` // Custom marshal handles this
}

func (c AskQuestionCommand) CommandType() string { return "AskQuestion" }
func (c AskQuestionCommand) commandSeal()        {}

// AnswerQuestionCommand answers a pending question.
type AnswerQuestionCommand struct {
	QuestionID ulid.ULID `json:"question_id"`
	Answer     string    `json:"answer"`
}

func (c AnswerQuestionCommand) CommandType() string { return "AnswerQuestion" }
func (c AnswerQuestionCommand) commandSeal()        {}

// StartAgentStepCommand marks the beginning of an agent's work step.
type StartAgentStepCommand struct {
	AgentID     string `json:"agent_id"`
	Description string `json:"description"`
}

func (c StartAgentStepCommand) CommandType() string { return "StartAgentStep" }
func (c StartAgentStepCommand) commandSeal()        {}

// FinishAgentStepCommand marks the end of an agent's work step.
type FinishAgentStepCommand struct {
	AgentID     string `json:"agent_id"`
	DiffSummary string `json:"diff_summary"`
}

func (c FinishAgentStepCommand) CommandType() string { return "FinishAgentStep" }
func (c FinishAgentStepCommand) commandSeal()        {}

// UndoCommand reverts the last undoable operation.
type UndoCommand struct{}

func (c UndoCommand) CommandType() string { return "Undo" }
func (c UndoCommand) commandSeal()        {}

// MarshalCommand serializes a Command with a "type" discriminator field.
func MarshalCommand(c Command) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("cannot marshal nil command")
	}

	switch v := c.(type) {
	case UpdateCardCommand:
		return marshalUpdateCard(v)
	case AskQuestionCommand:
		return marshalAskQuestion(v)
	case UndoCommand:
		return json.Marshal(map[string]string{"type": "Undo"})
	default:
		return marshalTagged(c.CommandType(), c)
	}
}

// UnmarshalCommand deserializes a Command from JSON with a "type" discriminator.
func UnmarshalCommand(data []byte) (Command, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal command type: %w", err)
	}

	switch envelope.Type {
	case "CreateSpec":
		var c CreateSpecCommand
		return c, json.Unmarshal(data, &c)
	case "UpdateSpecCore":
		var c UpdateSpecCoreCommand
		return c, json.Unmarshal(data, &c)
	case "CreateCard":
		var c CreateCardCommand
		return c, json.Unmarshal(data, &c)
	case "UpdateCard":
		return unmarshalUpdateCard(data)
	case "MoveCard":
		var c MoveCardCommand
		return c, json.Unmarshal(data, &c)
	case "DeleteCard":
		var c DeleteCardCommand
		return c, json.Unmarshal(data, &c)
	case "AppendTranscript":
		var c AppendTranscriptCommand
		return c, json.Unmarshal(data, &c)
	case "AskQuestion":
		return unmarshalAskQuestion(data)
	case "AnswerQuestion":
		var c AnswerQuestionCommand
		return c, json.Unmarshal(data, &c)
	case "StartAgentStep":
		var c StartAgentStepCommand
		return c, json.Unmarshal(data, &c)
	case "FinishAgentStep":
		var c FinishAgentStepCommand
		return c, json.Unmarshal(data, &c)
	case "Undo":
		return UndoCommand{}, nil
	default:
		return nil, fmt.Errorf("unknown command type: %q", envelope.Type)
	}
}

// marshalTagged marshals a struct with an injected "type" field.
func marshalTagged(typeName string, v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	typeJSON, _ := json.Marshal(typeName)
	m["type"] = typeJSON
	return json.Marshal(m)
}

// marshalUpdateCard handles the special OptionalField[string] body field.
func marshalUpdateCard(c UpdateCardCommand) ([]byte, error) {
	j := updateCardJSON{
		Type:      "UpdateCard",
		CardID:    c.CardID,
		Title:     c.Title,
		CardType:  c.CardType,
		Refs:      c.Refs,
		UpdatedBy: c.UpdatedBy,
	}
	if c.Body.Set {
		if c.Body.Valid {
			bodyJSON, _ := json.Marshal(c.Body.Value)
			raw := json.RawMessage(bodyJSON)
			j.Body = &raw
		} else {
			raw := json.RawMessage("null")
			j.Body = &raw
		}
	}
	return json.Marshal(j)
}

// unmarshalUpdateCard handles the special OptionalField[string] body field.
// Uses raw JSON map to distinguish "body":null from absent body, since
// *json.RawMessage with omitempty collapses both to nil on unmarshal.
func unmarshalUpdateCard(data []byte) (UpdateCardCommand, error) {
	var j updateCardJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return UpdateCardCommand{}, err
	}

	cmd := UpdateCardCommand{
		CardID:    j.CardID,
		Title:     j.Title,
		CardType:  j.CardType,
		Refs:      j.Refs,
		UpdatedBy: j.UpdatedBy,
	}

	// Check the raw JSON map to distinguish absent body from null body,
	// since *json.RawMessage sets both to nil during standard unmarshal.
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return UpdateCardCommand{}, err
	}
	if bodyRaw, present := rawMap["body"]; present {
		cmd.Body.Set = true
		if string(bodyRaw) == "null" {
			cmd.Body.Valid = false
		} else {
			cmd.Body.Valid = true
			if err := json.Unmarshal(bodyRaw, &cmd.Body.Value); err != nil {
				return UpdateCardCommand{}, fmt.Errorf("unmarshal UpdateCard body: %w", err)
			}
		}
	}

	return cmd, nil
}

// marshalAskQuestion handles the UserQuestion interface field.
func marshalAskQuestion(c AskQuestionCommand) ([]byte, error) {
	qJSON, err := MarshalUserQuestion(c.Question)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{
		"type": json.RawMessage(`"AskQuestion"`),
	}
	m["question"] = qJSON
	return json.Marshal(m)
}

// unmarshalAskQuestion handles the UserQuestion interface field.
func unmarshalAskQuestion(data []byte) (AskQuestionCommand, error) {
	var raw struct {
		Question json.RawMessage `json:"question"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return AskQuestionCommand{}, err
	}
	q, err := UnmarshalUserQuestion(raw.Question)
	if err != nil {
		return AskQuestionCommand{}, err
	}
	return AskQuestionCommand{Question: q}, nil
}
