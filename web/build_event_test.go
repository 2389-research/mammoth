// ABOUTME: Tests for BuildEvent type and mapper functions from tracker event types.
// ABOUTME: Validates pipeline event mapping, agent event mapping, and field preservation.
package web

import (
	"testing"
	"time"

	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
)

func TestBuildEventFromPipeline_StageStarted(t *testing.T) {
	evt := pipeline.PipelineEvent{
		Type:      pipeline.EventStageStarted,
		Timestamp: time.Now(),
		NodeID:    "build_ui",
		Message:   "starting node",
	}
	be := buildEventFromPipeline(evt)
	if be.Type != BuildEventNodeStarted {
		t.Errorf("expected %q, got %q", BuildEventNodeStarted, be.Type)
	}
	if be.NodeID != "build_ui" {
		t.Errorf("expected node_id build_ui, got %q", be.NodeID)
	}
}

func TestBuildEventFromPipeline_PipelineCompleted(t *testing.T) {
	evt := pipeline.PipelineEvent{
		Type: pipeline.EventPipelineCompleted,
	}
	be := buildEventFromPipeline(evt)
	if be.Type != BuildEventPipelineCompleted {
		t.Errorf("expected %q, got %q", BuildEventPipelineCompleted, be.Type)
	}
}

func TestBuildEventFromPipeline_UnmappedType(t *testing.T) {
	evt := pipeline.PipelineEvent{
		Type: pipeline.PipelineEventType("unknown_future_type"),
	}
	be := buildEventFromPipeline(evt)
	if be.Type != BuildEventType("unknown_future_type") {
		t.Errorf("unmapped types should pass through, got %q", be.Type)
	}
}

func TestBuildEventFromAgent_ToolCallStart(t *testing.T) {
	evt := agent.Event{
		Type:     agent.EventToolCallStart,
		ToolName: "bash",
	}
	be := buildEventFromAgent(evt)
	if be.Type != BuildEventToolCallStart {
		t.Errorf("expected %q, got %q", BuildEventToolCallStart, be.Type)
	}
	if be.Data["tool_name"] != "bash" {
		t.Errorf("expected tool_name=bash, got %v", be.Data["tool_name"])
	}
}

func TestBuildEventFromAgent_TextDelta(t *testing.T) {
	evt := agent.Event{
		Type: agent.EventTextDelta,
		Text: "hello world",
	}
	be := buildEventFromAgent(evt)
	if be.Type != BuildEventTextDelta {
		t.Errorf("expected %q, got %q", BuildEventTextDelta, be.Type)
	}
	if be.Data["text"] != "hello world" {
		t.Errorf("expected text in data")
	}
}

func TestBuildEventFromAgent_DroppedType(t *testing.T) {
	evt := agent.Event{
		Type: agent.EventLLMReasoning,
	}
	be := buildEventFromAgent(evt)
	if be.Type != "" {
		t.Errorf("expected empty type for dropped event, got %q", be.Type)
	}
}
