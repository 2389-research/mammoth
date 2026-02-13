// ABOUTME: Implements the emit_diff_summary tool for finishing an agent step with a change summary.
// ABOUTME: Sends a FinishAgentStep command to mark the end of an agent's work cycle.
package tools

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

// EmitDiffSummaryTool emits a summary of changes made during an agent step.
type EmitDiffSummaryTool struct {
	Actor        *core.SpecActorHandle
	AgentID      string
	StepFinished *atomic.Bool // set to true when this tool executes, so caller skips fallback
}

func (t *EmitDiffSummaryTool) Name() string {
	return "emit_diff_summary"
}

func (t *EmitDiffSummaryTool) Description() string {
	return "Emit a summary of changes made during this agent step. Used to describe what was added, modified, or removed."
}

func (t *EmitDiffSummaryTool) RequiresApproval(_ map[string]any) bool {
	return false
}

func (t *EmitDiffSummaryTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "A concise summary of the changes made in this step.",
			},
		},
		"required": []any{"summary"},
	}
}

func (t *EmitDiffSummaryTool) Execute(_ context.Context, params map[string]any) (*tool.Result, error) {
	summaryRaw, ok := params["summary"]
	if !ok {
		return nil, fmt.Errorf("missing 'summary' parameter")
	}
	summary, ok := summaryRaw.(string)
	if !ok {
		return nil, fmt.Errorf("'summary' parameter must be a string")
	}

	_, err := t.Actor.SendCommand(core.FinishAgentStepCommand{
		AgentID:     t.AgentID,
		DiffSummary: summary,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to finish agent step: %w", err)
	}

	// Signal that this tool already sent FinishAgentStep so the caller
	// does not emit a duplicate fallback event.
	if t.StepFinished != nil {
		t.StepFinished.Store(true)
	}

	return tool.NewResult("emit_diff_summary", true, "Step finished", ""), nil
}
