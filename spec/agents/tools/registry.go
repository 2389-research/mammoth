// ABOUTME: Factory function that creates and registers all 7 domain tools.
// ABOUTME: BuildRegistry returns a mux tool.Registry with tools bound to a spec actor and agent ID.
package tools

import (
	"sync/atomic"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

// BuildRegistry creates a tool registry with all 7 domain tools registered.
// The returned registry contains: read_state, write_commands, emit_narration,
// emit_diff_summary, ask_user_boolean, ask_user_multiple_choice, ask_user_freeform.
// The stepFinished flag is set by emit_diff_summary so the caller can skip
// sending a duplicate FinishAgentStep event.
func BuildRegistry(
	actor *core.SpecActorHandle,
	questionPending *atomic.Bool,
	agentID string,
	stepFinished *atomic.Bool,
) *tool.Registry {
	registry := tool.NewRegistry()

	registry.Register(&ReadStateTool{
		Actor: actor,
	})

	registry.Register(&WriteCommandsTool{
		Actor:   actor,
		AgentID: agentID,
	})

	registry.Register(&EmitNarrationTool{
		Actor:   actor,
		AgentID: agentID,
	})

	registry.Register(&EmitDiffSummaryTool{
		Actor:        actor,
		AgentID:      agentID,
		StepFinished: stepFinished,
	})

	registry.Register(&AskBooleanTool{
		Actor:           actor,
		QuestionPending: questionPending,
		AgentID:         agentID,
	})

	registry.Register(&AskMultipleChoiceTool{
		Actor:           actor,
		QuestionPending: questionPending,
		AgentID:         agentID,
	})

	registry.Register(&AskFreeformTool{
		Actor:           actor,
		QuestionPending: questionPending,
		AgentID:         agentID,
	})

	return registry
}
