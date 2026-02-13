// ABOUTME: Implements the ask_user_freeform tool for asking open-ended questions via mux Tool interface.
// ABOUTME: Gates on an atomic.Bool to prevent multiple concurrent questions from different agents.
package tools

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

// AskFreeformTool asks the user a free-form question.
type AskFreeformTool struct {
	Actor           *core.SpecActorHandle
	QuestionPending *atomic.Bool
	AgentID         string
}

func (t *AskFreeformTool) Name() string {
	return "ask_user_freeform"
}

func (t *AskFreeformTool) Description() string {
	return "Ask the user a free-form question. Use when you need detailed or unstructured input."
}

func (t *AskFreeformTool) RequiresApproval(_ map[string]any) bool {
	return false
}

func (t *AskFreeformTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "The question to ask the user.",
			},
			"placeholder": map[string]any{
				"type":        "string",
				"description": "Optional placeholder text for the input field.",
			},
			"validation_hint": map[string]any{
				"type":        "string",
				"description": "Optional hint about expected format or content.",
			},
		},
		"required": []any{"question"},
	}
}

func (t *AskFreeformTool) Execute(_ context.Context, params map[string]any) (*tool.Result, error) {
	// Validate params BEFORE setting the flag so invalid calls don't block future questions.
	questionRaw, ok := params["question"]
	if !ok {
		return nil, fmt.Errorf("missing 'question' parameter")
	}
	questionText, ok := questionRaw.(string)
	if !ok {
		return nil, fmt.Errorf("'question' parameter must be a string")
	}

	var placeholder *string
	if placeholderRaw, exists := params["placeholder"]; exists {
		if s, ok := placeholderRaw.(string); ok {
			placeholder = &s
		}
	}

	var validationHint *string
	if hintRaw, exists := params["validation_hint"]; exists {
		if s, ok := hintRaw.(string); ok {
			validationHint = &s
		}
	}

	// Atomically check-and-set to avoid TOCTOU race between agents.
	if !t.QuestionPending.CompareAndSwap(false, true) {
		return tool.NewResult("ask_user_freeform", true, "Question already pending, skipping", ""), nil
	}

	question := core.FreeformQuestion{
		QID:            core.NewULID(),
		Question:       questionText,
		Placeholder:    placeholder,
		ValidationHint: validationHint,
	}

	_, err := t.Actor.SendCommand(core.AskQuestionCommand{Question: question})
	if err != nil {
		// Reset flag on failure so another agent can retry.
		t.QuestionPending.Store(false)
		return nil, fmt.Errorf("failed to ask question: %w", err)
	}

	return tool.NewResult("ask_user_freeform", true, "Question asked", ""), nil
}
