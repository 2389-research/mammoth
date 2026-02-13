// ABOUTME: Implements the ask_user_boolean tool for asking yes/no questions via mux Tool interface.
// ABOUTME: Gates on an atomic.Bool to prevent multiple concurrent questions from different agents.
package tools

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

// AskBooleanTool asks the user a yes/no question.
type AskBooleanTool struct {
	Actor           *core.SpecActorHandle
	QuestionPending *atomic.Bool
	AgentID         string
}

func (t *AskBooleanTool) Name() string {
	return "ask_user_boolean"
}

func (t *AskBooleanTool) Description() string {
	return "Ask the user a yes/no question. Use when you need a simple binary decision from the human."
}

func (t *AskBooleanTool) RequiresApproval(_ map[string]any) bool {
	return false
}

func (t *AskBooleanTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "The yes/no question to ask the user.",
			},
			"default": map[string]any{
				"type":        "boolean",
				"description": "Optional default answer (true for yes, false for no).",
			},
		},
		"required": []any{"question"},
	}
}

func (t *AskBooleanTool) Execute(_ context.Context, params map[string]any) (*tool.Result, error) {
	// Validate params BEFORE setting the flag so invalid calls don't block future questions.
	questionRaw, ok := params["question"]
	if !ok {
		return nil, fmt.Errorf("missing 'question' parameter")
	}
	questionText, ok := questionRaw.(string)
	if !ok {
		return nil, fmt.Errorf("'question' parameter must be a string")
	}

	var defaultVal *bool
	if defaultRaw, exists := params["default"]; exists {
		if b, ok := defaultRaw.(bool); ok {
			defaultVal = &b
		}
	}

	// Atomically check-and-set to avoid TOCTOU race between agents.
	if !t.QuestionPending.CompareAndSwap(false, true) {
		return tool.NewResult("ask_user_boolean", true, "Question already pending, skipping", ""), nil
	}

	question := core.BooleanQuestion{
		QID:      core.NewULID(),
		Question: questionText,
		Default:  defaultVal,
	}

	_, err := t.Actor.SendCommand(core.AskQuestionCommand{Question: question})
	if err != nil {
		// Reset flag on failure so another agent can retry.
		t.QuestionPending.Store(false)
		return nil, fmt.Errorf("failed to ask question: %w", err)
	}

	return tool.NewResult("ask_user_boolean", true, "Question asked", ""), nil
}
