// ABOUTME: Event handler factory for wiring attractor engine events to ActiveRun state.
// ABOUTME: Updates current node, activity, completed nodes, and maintains a rolling event buffer.
package mcp

import (
	"fmt"

	"github.com/2389-research/mammoth/attractor"
)

// newEventHandler returns an event handler function that updates the given
// ActiveRun's state as engine events arrive.
func newEventHandler(run *ActiveRun) func(attractor.EngineEvent) {
	return func(evt attractor.EngineEvent) {
		run.mu.Lock()
		defer run.mu.Unlock()

		// Append to rolling buffer.
		if len(run.EventBuffer) >= maxEventBuffer {
			copy(run.EventBuffer, run.EventBuffer[1:])
			run.EventBuffer = run.EventBuffer[:maxEventBuffer-1]
		}
		run.EventBuffer = append(run.EventBuffer, evt)

		switch evt.Type {
		case attractor.EventStageStarted:
			run.CurrentNode = evt.NodeID
			run.CurrentActivity = fmt.Sprintf("executing node: %s", evt.NodeID)
		case attractor.EventStageCompleted:
			run.CompletedNodes = append(run.CompletedNodes, evt.NodeID)
			run.CurrentActivity = fmt.Sprintf("completed node: %s", evt.NodeID)
		case attractor.EventStageFailed:
			reason := ""
			if r, ok := evt.Data["reason"]; ok {
				reason = fmt.Sprintf(": %v", r)
			}
			run.CurrentActivity = fmt.Sprintf("node failed: %s%s", evt.NodeID, reason)
		case attractor.EventStageRetrying:
			attempt := ""
			if a, ok := evt.Data["attempt"]; ok {
				attempt = fmt.Sprintf(" (attempt %v)", a)
			}
			run.CurrentActivity = fmt.Sprintf("retrying node: %s%s", evt.NodeID, attempt)
		case attractor.EventAgentToolCallStart:
			toolName := "unknown"
			if tn, ok := evt.Data["tool_name"]; ok {
				toolName = fmt.Sprintf("%v", tn)
			}
			run.CurrentActivity = fmt.Sprintf("calling tool: %s", toolName)
		case attractor.EventAgentToolCallEnd:
			run.CurrentActivity = "tool call completed"
		case attractor.EventAgentLLMTurn:
			run.CurrentActivity = "LLM generating response"
		case attractor.EventAgentTextStart, attractor.EventAgentTextDelta:
			run.CurrentActivity = "LLM streaming text"
		case attractor.EventAgentSteering:
			run.CurrentActivity = "applying steering"
		case attractor.EventPipelineCompleted:
			run.CurrentActivity = "pipeline completed"
		case attractor.EventPipelineFailed:
			errMsg := ""
			if e, ok := evt.Data["error"]; ok {
				errMsg = fmt.Sprintf(": %v", e)
			}
			run.CurrentActivity = fmt.Sprintf("pipeline failed%s", errMsg)
		case attractor.EventCheckpointSaved:
			run.CurrentActivity = "checkpoint saved"
		}
	}
}
