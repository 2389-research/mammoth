// ABOUTME: Tests for the MCP event handler that tracks live pipeline activity.
// ABOUTME: Validates current node, current activity, completed nodes, and buffer rotation.
package mcp

import (
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

func TestEventHandlerUpdatesCurrentNode(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]attractor.EngineEvent, 0, maxEventBuffer),
	}
	handler := newEventHandler(run)
	handler(attractor.EngineEvent{
		Type:   attractor.EventStageStarted,
		NodeID: "build_step",
	})
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.CurrentNode != "build_step" {
		t.Errorf("CurrentNode: got %q, want %q", run.CurrentNode, "build_step")
	}
}

func TestEventHandlerTracksCompletedNodes(t *testing.T) {
	run := &ActiveRun{
		ID:             "test",
		Status:         StatusRunning,
		CompletedNodes: make([]string, 0),
		EventBuffer:    make([]attractor.EngineEvent, 0, maxEventBuffer),
	}
	handler := newEventHandler(run)
	handler(attractor.EngineEvent{Type: attractor.EventStageCompleted, NodeID: "step_1"})
	handler(attractor.EngineEvent{Type: attractor.EventStageCompleted, NodeID: "step_2"})
	run.mu.RLock()
	defer run.mu.RUnlock()
	if len(run.CompletedNodes) != 2 {
		t.Errorf("expected 2 completed nodes, got %d", len(run.CompletedNodes))
	}
}

func TestEventHandlerTracksActivity(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]attractor.EngineEvent, 0, maxEventBuffer),
	}
	handler := newEventHandler(run)
	handler(attractor.EngineEvent{
		Type: attractor.EventAgentToolCallStart,
		Data: map[string]any{"tool_name": "write_file"},
	})
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.CurrentActivity != "calling tool: write_file" {
		t.Errorf("CurrentActivity: got %q", run.CurrentActivity)
	}
}

func TestEventHandlerBufferRotation(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]attractor.EngineEvent, 0, maxEventBuffer),
	}
	handler := newEventHandler(run)
	for i := 0; i < maxEventBuffer+100; i++ {
		handler(attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
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
