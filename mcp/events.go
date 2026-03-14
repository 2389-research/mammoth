// ABOUTME: Event handler factories for wiring tracker pipeline and agent events to ActiveRun state.
// ABOUTME: Updates current node, activity, completed nodes, and maintains a rolling event buffer.
package mcp

import (
	"fmt"

	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
)

// newPipelineEventHandler returns a pipeline event handler that updates the
// given ActiveRun's state as pipeline events arrive.
func newPipelineEventHandler(run *ActiveRun) pipeline.PipelineEventHandlerFunc {
	return func(evt pipeline.PipelineEvent) {
		re := RunEvent{
			Type:      string(evt.Type),
			NodeID:    evt.NodeID,
			Timestamp: evt.Timestamp,
			Message:   evt.Message,
		}
		if evt.Err != nil {
			re.Data = map[string]any{"error": evt.Err.Error()}
		}

		run.mu.Lock()
		defer run.mu.Unlock()

		// Append to rolling buffer.
		if len(run.EventBuffer) >= maxEventBuffer {
			copy(run.EventBuffer, run.EventBuffer[1:])
			run.EventBuffer = run.EventBuffer[:maxEventBuffer-1]
		}
		run.EventBuffer = append(run.EventBuffer, re)

		switch evt.Type {
		case pipeline.EventStageStarted:
			run.CurrentNode = evt.NodeID
			run.CurrentActivity = fmt.Sprintf("executing node: %s", evt.NodeID)
		case pipeline.EventStageCompleted:
			run.CompletedNodes = append(run.CompletedNodes, evt.NodeID)
			run.CurrentActivity = fmt.Sprintf("completed node: %s", evt.NodeID)
		case pipeline.EventStageFailed:
			run.CurrentActivity = fmt.Sprintf("node failed: %s", evt.NodeID)
			if evt.Err != nil {
				run.CurrentActivity = fmt.Sprintf("node failed: %s: %v", evt.NodeID, evt.Err)
			}
		case pipeline.EventStageRetrying:
			run.CurrentActivity = fmt.Sprintf("retrying node: %s", evt.NodeID)
		case pipeline.EventPipelineCompleted:
			run.CurrentActivity = "pipeline completed"
		case pipeline.EventPipelineFailed:
			run.CurrentActivity = "pipeline failed"
			if evt.Err != nil {
				run.CurrentActivity = fmt.Sprintf("pipeline failed: %v", evt.Err)
			}
		case pipeline.EventCheckpointSaved:
			run.CurrentActivity = "checkpoint saved"
		}
	}
}

// newAgentEventHandler returns an agent event handler that appends agent
// events to the ActiveRun's event buffer and updates activity.
func newAgentEventHandler(run *ActiveRun) agent.EventHandlerFunc {
	return func(evt agent.Event) {
		re := RunEvent{
			Type:      string(evt.Type),
			NodeID:    evt.SessionID,
			Timestamp: evt.Timestamp,
		}
		data := make(map[string]any)
		if evt.ToolName != "" {
			data["tool_name"] = evt.ToolName
		}
		if evt.Text != "" {
			data["text"] = evt.Text
		}
		if evt.Err != nil {
			data["error"] = evt.Err.Error()
		}
		if len(data) > 0 {
			re.Data = data
		}

		run.mu.Lock()
		defer run.mu.Unlock()

		// Append to rolling buffer.
		if len(run.EventBuffer) >= maxEventBuffer {
			copy(run.EventBuffer, run.EventBuffer[1:])
			run.EventBuffer = run.EventBuffer[:maxEventBuffer-1]
		}
		run.EventBuffer = append(run.EventBuffer, re)

		// Update current activity for agent events.
		switch evt.Type {
		case "tool_call_start":
			toolName := evt.ToolName
			if toolName == "" {
				toolName = "unknown"
			}
			run.CurrentActivity = fmt.Sprintf("calling tool: %s", toolName)
		case "tool_call_end":
			run.CurrentActivity = "tool call completed"
		case "text_delta":
			run.CurrentActivity = "LLM streaming text"
		}
	}
}
