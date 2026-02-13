// ABOUTME: Implements the ask_user_multiple_choice tool for presenting choices to the user.
// ABOUTME: Gates on an atomic.Bool to prevent multiple concurrent questions from different agents.
package tools

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

// AskMultipleChoiceTool asks the user to choose from a list of options.
type AskMultipleChoiceTool struct {
	Actor           *core.SpecActorHandle
	QuestionPending *atomic.Bool
	AgentID         string
}

func (t *AskMultipleChoiceTool) Name() string {
	return "ask_user_multiple_choice"
}

func (t *AskMultipleChoiceTool) Description() string {
	return "Ask the user to choose from a list of options. Use when you have specific alternatives to present."
}

func (t *AskMultipleChoiceTool) RequiresApproval(_ map[string]any) bool {
	return false
}

func (t *AskMultipleChoiceTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "The question to present along with the choices.",
			},
			"choices": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "List of choices for the user to select from.",
			},
			"allow_multi": map[string]any{
				"type":        "boolean",
				"description": "Whether the user can select multiple choices. Defaults to false.",
			},
		},
		"required": []any{"question", "choices"},
	}
}

func (t *AskMultipleChoiceTool) Execute(_ context.Context, params map[string]any) (*tool.Result, error) {
	// Validate params BEFORE setting the flag so invalid calls don't block future questions.
	questionRaw, ok := params["question"]
	if !ok {
		return nil, fmt.Errorf("missing 'question' parameter")
	}
	questionText, ok := questionRaw.(string)
	if !ok {
		return nil, fmt.Errorf("'question' parameter must be a string")
	}

	choicesRaw, ok := params["choices"]
	if !ok {
		return nil, fmt.Errorf("missing 'choices' parameter")
	}

	choicesArray, ok := choicesRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("'choices' must be an array")
	}

	var choices []string
	for _, c := range choicesArray {
		if s, ok := c.(string); ok {
			choices = append(choices, s)
		}
	}

	allowMulti := false
	if allowMultiRaw, exists := params["allow_multi"]; exists {
		if b, ok := allowMultiRaw.(bool); ok {
			allowMulti = b
		}
	}

	// Atomically check-and-set to avoid TOCTOU race between agents.
	if !t.QuestionPending.CompareAndSwap(false, true) {
		return tool.NewResult("ask_user_multiple_choice", true, "Question already pending, skipping", ""), nil
	}

	question := core.MultipleChoiceQuestion{
		QID:        core.NewULID(),
		Question:   questionText,
		Choices:    choices,
		AllowMulti: allowMulti,
	}

	_, err := t.Actor.SendCommand(core.AskQuestionCommand{Question: question})
	if err != nil {
		// Reset flag on failure so another agent can retry.
		t.QuestionPending.Store(false)
		return nil, fmt.Errorf("failed to ask question: %w", err)
	}

	return tool.NewResult("ask_user_multiple_choice", true, "Question asked", ""), nil
}
