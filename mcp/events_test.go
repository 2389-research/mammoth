// ABOUTME: Tests for the MCP event handlers that track live pipeline activity.
// ABOUTME: Validates current node, current activity, completed nodes, and buffer rotation.
package mcp

import (
	"testing"
	"time"

	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
)

func TestPipelineEventHandlerUpdatesCurrentNode(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]RunEvent, 0, maxEventBuffer),
	}
	handler := newPipelineEventHandler(run)
	handler.HandlePipelineEvent(pipeline.PipelineEvent{
		Type:      pipeline.EventStageStarted,
		NodeID:    "build_step",
		Timestamp: time.Now(),
	})
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.CurrentNode != "build_step" {
		t.Errorf("CurrentNode: got %q, want %q", run.CurrentNode, "build_step")
	}
}

func TestPipelineEventHandlerTracksCompletedNodes(t *testing.T) {
	run := &ActiveRun{
		ID:             "test",
		Status:         StatusRunning,
		CompletedNodes: make([]string, 0),
		EventBuffer:    make([]RunEvent, 0, maxEventBuffer),
	}
	handler := newPipelineEventHandler(run)
	handler.HandlePipelineEvent(pipeline.PipelineEvent{Type: pipeline.EventStageCompleted, NodeID: "step_1", Timestamp: time.Now()})
	handler.HandlePipelineEvent(pipeline.PipelineEvent{Type: pipeline.EventStageCompleted, NodeID: "step_2", Timestamp: time.Now()})
	run.mu.RLock()
	defer run.mu.RUnlock()
	if len(run.CompletedNodes) != 2 {
		t.Errorf("expected 2 completed nodes, got %d", len(run.CompletedNodes))
	}
}

func TestPipelineEventHandlerTracksActivity(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]RunEvent, 0, maxEventBuffer),
	}
	handler := newPipelineEventHandler(run)
	handler.HandlePipelineEvent(pipeline.PipelineEvent{
		Type:      pipeline.EventPipelineCompleted,
		Timestamp: time.Now(),
	})
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.CurrentActivity != "pipeline completed" {
		t.Errorf("CurrentActivity: got %q", run.CurrentActivity)
	}
}

func TestPipelineEventHandlerBufferRotation(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]RunEvent, 0, maxEventBuffer),
	}
	handler := newPipelineEventHandler(run)
	for i := 0; i < maxEventBuffer+100; i++ {
		handler.HandlePipelineEvent(pipeline.PipelineEvent{
			Type:      pipeline.EventStageStarted,
			NodeID:    "node",
			Timestamp: time.Now(),
		})
	}
	run.mu.RLock()
	defer run.mu.RUnlock()
	if len(run.EventBuffer) != maxEventBuffer {
		t.Errorf("buffer size: got %d, want %d", len(run.EventBuffer), maxEventBuffer)
	}
}

func TestAgentEventHandlerAppendsEvents(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]RunEvent, 0, maxEventBuffer),
	}
	handler := newAgentEventHandler(run)
	handler.HandleEvent(agent.Event{
		Type:      "tool_call_start",
		ToolName:  "write_file",
		Timestamp: time.Now(),
	})
	run.mu.RLock()
	defer run.mu.RUnlock()
	if len(run.EventBuffer) != 1 {
		t.Errorf("expected 1 event, got %d", len(run.EventBuffer))
	}
	if run.CurrentActivity != "calling tool: write_file" {
		t.Errorf("CurrentActivity: got %q", run.CurrentActivity)
	}
}
