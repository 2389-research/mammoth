// ABOUTME: Implements the emit_narration tool for posting agent narration to the spec transcript.
// ABOUTME: Sends an AppendTranscript command with the agent's identity as the sender.
package tools

import (
	"context"
	"fmt"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

// EmitNarrationTool emits a narration message to the spec transcript.
type EmitNarrationTool struct {
	Actor   *core.SpecActorHandle
	AgentID string
}

func (t *EmitNarrationTool) Name() string {
	return "emit_narration"
}

func (t *EmitNarrationTool) Description() string {
	return "Emit a narration message to the spec transcript. Use to explain your reasoning or share observations with the user."
}

func (t *EmitNarrationTool) RequiresApproval(_ map[string]any) bool {
	return false
}

func (t *EmitNarrationTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "The narration text to add to the transcript.",
			},
		},
		"required": []any{"message"},
	}
}

func (t *EmitNarrationTool) Execute(_ context.Context, params map[string]any) (*tool.Result, error) {
	messageRaw, ok := params["message"]
	if !ok {
		return nil, fmt.Errorf("missing 'message' parameter")
	}
	message, ok := messageRaw.(string)
	if !ok {
		return nil, fmt.Errorf("'message' parameter must be a string")
	}

	_, err := t.Actor.SendCommand(core.AppendTranscriptCommand{
		Sender:  t.AgentID,
		Content: message,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to append transcript: %w", err)
	}

	return tool.NewResult("emit_narration", true, "Narration posted", ""), nil
}
