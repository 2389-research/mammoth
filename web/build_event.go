// ABOUTME: Unified SSE wire format for pipeline build events.
// ABOUTME: Maps tracker's pipeline.PipelineEvent and agent.Event into a single BuildEvent type.
package web

import (
	"time"

	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
)

// BuildEventType identifies the kind of build event for SSE consumers.
type BuildEventType string

const (
	// Pipeline lifecycle (mapped from pipeline.PipelineEvent).
	// Tracker uses "stage_*" naming; we map to "node_*" for the SSE wire format.
	BuildEventPipelineStarted   BuildEventType = "pipeline_started"
	BuildEventPipelineCompleted BuildEventType = "pipeline_completed"
	BuildEventPipelineFailed    BuildEventType = "pipeline_failed"
	BuildEventNodeStarted       BuildEventType = "node_started"
	BuildEventNodeCompleted     BuildEventType = "node_completed"
	BuildEventNodeFailed        BuildEventType = "node_failed"
	BuildEventNodeRetrying      BuildEventType = "node_retrying"
	BuildEventCheckpointSaved   BuildEventType = "checkpoint_saved"
	BuildEventParallelStarted   BuildEventType = "parallel_started"
	BuildEventParallelCompleted BuildEventType = "parallel_completed"
	BuildEventLoopRestart       BuildEventType = "loop_restart"

	// Agent activity (mapped from agent.Event).
	// Only a subset of tracker's agent event types are surfaced.
	BuildEventToolCallStart BuildEventType = "tool_call_start"
	BuildEventToolCallEnd   BuildEventType = "tool_call_end"
	BuildEventTextDelta     BuildEventType = "text_delta"
	BuildEventSessionStart  BuildEventType = "session_start"
	BuildEventSessionEnd    BuildEventType = "session_end"
	BuildEventAgentError    BuildEventType = "agent_error"

	// Human gates.
	BuildEventHumanGateChoice   BuildEventType = "human_gate_choice"
	BuildEventHumanGateFreeform BuildEventType = "human_gate_freeform"
	BuildEventHumanGateAnswered BuildEventType = "human_gate_answered"
)

// sseEventNameMap translates internal BuildEventType names to the dotted SSE
// event names expected by the browser. This preserves backward compatibility
// with the existing JavaScript event listeners (e.g. "stage.started").
var sseEventNameMap = map[BuildEventType]string{
	BuildEventPipelineStarted:   "pipeline.started",
	BuildEventPipelineCompleted: "pipeline.completed",
	BuildEventPipelineFailed:    "pipeline.failed",
	BuildEventNodeStarted:       "stage.started",
	BuildEventNodeCompleted:     "stage.completed",
	BuildEventNodeFailed:        "stage.failed",
	BuildEventNodeRetrying:      "stage.retrying",
	BuildEventCheckpointSaved:   "checkpoint.saved",
	BuildEventParallelStarted:   "parallel.started",
	BuildEventParallelCompleted: "parallel.completed",
	BuildEventLoopRestart:       "loop.restart",
	BuildEventToolCallStart:     "agent.tool_call.start",
	BuildEventToolCallEnd:       "agent.tool_call.end",
	BuildEventTextDelta:         "agent.text_delta",
	BuildEventSessionStart:      "agent.session.start",
	BuildEventSessionEnd:        "agent.session.end",
	BuildEventAgentError:        "agent.error",
	BuildEventHumanGateChoice:   "human_gate.choice",
	BuildEventHumanGateFreeform: "human_gate.freeform",
	BuildEventHumanGateAnswered: "human_gate.answered",
}

// SSEEventName returns the dotted SSE event name for this BuildEventType.
// Falls back to the raw string if no mapping exists.
func (t BuildEventType) SSEEventName() string {
	if name, ok := sseEventNameMap[t]; ok {
		return name
	}
	return string(t)
}

// BuildEvent is the unified SSE wire format for build progress.
type BuildEvent struct {
	Type      BuildEventType `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	NodeID    string         `json:"node_id,omitempty"`
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// pipelineEventMap maps tracker stage_* events to mammoth node_* events.
var pipelineEventMap = map[pipeline.PipelineEventType]BuildEventType{
	pipeline.EventPipelineStarted:   BuildEventPipelineStarted,
	pipeline.EventPipelineCompleted: BuildEventPipelineCompleted,
	pipeline.EventPipelineFailed:    BuildEventPipelineFailed,
	pipeline.EventStageStarted:      BuildEventNodeStarted,
	pipeline.EventStageCompleted:    BuildEventNodeCompleted,
	pipeline.EventStageFailed:       BuildEventNodeFailed,
	pipeline.EventStageRetrying:     BuildEventNodeRetrying,
	pipeline.EventCheckpointSaved:   BuildEventCheckpointSaved,
	pipeline.EventParallelStarted:   BuildEventParallelStarted,
	pipeline.EventParallelCompleted: BuildEventParallelCompleted,
	pipeline.EventLoopRestart:       BuildEventLoopRestart,
}

// buildEventFromPipeline maps a tracker PipelineEvent to a BuildEvent.
func buildEventFromPipeline(evt pipeline.PipelineEvent) BuildEvent {
	typ, ok := pipelineEventMap[evt.Type]
	if !ok {
		typ = BuildEventType(evt.Type)
	}
	be := BuildEvent{
		Type:      typ,
		Timestamp: evt.Timestamp,
		NodeID:    evt.NodeID,
		Message:   evt.Message,
	}
	if evt.Err != nil {
		be.Data = map[string]any{"error": evt.Err.Error()}
	}
	return be
}

// agentEventMap maps tracker agent events to BuildEvent types.
// Events not in this map are dropped (internal detail not needed by UI).
var agentEventMap = map[agent.EventType]BuildEventType{
	agent.EventToolCallStart: BuildEventToolCallStart,
	agent.EventToolCallEnd:   BuildEventToolCallEnd,
	agent.EventTextDelta:     BuildEventTextDelta,
	agent.EventSessionStart:  BuildEventSessionStart,
	agent.EventSessionEnd:    BuildEventSessionEnd,
	agent.EventError:         BuildEventAgentError,
}

// buildEventFromAgent maps a tracker agent.Event to a BuildEvent.
// Returns a zero-value BuildEvent for dropped event types.
func buildEventFromAgent(evt agent.Event) BuildEvent {
	typ, ok := agentEventMap[evt.Type]
	if !ok {
		return BuildEvent{}
	}
	be := BuildEvent{
		Type:      typ,
		Timestamp: evt.Timestamp,
		NodeID:    evt.SessionID,
	}
	data := make(map[string]any)
	switch evt.Type {
	case agent.EventToolCallStart:
		data["tool_name"] = evt.ToolName
		if evt.ToolInput != "" {
			data["arguments"] = evt.ToolInput
		}
	case agent.EventToolCallEnd:
		data["tool_name"] = evt.ToolName
		if evt.ToolError != "" {
			data["error"] = evt.ToolError
		}
		if evt.ToolOutput != "" {
			// Truncate to 200 chars for the UI snippet.
			snippet := evt.ToolOutput
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			data["output_snippet"] = snippet
		}
	case agent.EventTextDelta:
		data["text"] = evt.Text
	case agent.EventError:
		if evt.Err != nil {
			data["error"] = evt.Err.Error()
		}
	}
	if len(data) > 0 {
		be.Data = data
	}
	return be
}
