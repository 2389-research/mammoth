// ABOUTME: Tests for the EventBridge, RunPipelineCmd, WaitForHumanGateCmd, and TickCmd.
// ABOUTME: Validates the bridge layer connecting tracker engine events to the Bubble Tea message loop.
package tui

import (
	"sync"
	"testing"
	"time"

	"github.com/2389-research/tracker/agent"
	"github.com/2389-research/tracker/pipeline"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewEventBridge(t *testing.T) {
	called := false
	send := func(msg tea.Msg) {
		called = true
	}

	bridge := NewEventBridge(send)
	if bridge == nil {
		t.Fatal("NewEventBridge returned nil")
	}
	if bridge.send == nil {
		t.Fatal("EventBridge.send is nil")
	}

	// Verify the send function is wired correctly
	bridge.send(nil)
	if !called {
		t.Error("send function was not called")
	}
}

func TestEventBridgePipelineHandler(t *testing.T) {
	var received tea.Msg
	send := func(msg tea.Msg) {
		received = msg
	}

	bridge := NewEventBridge(send)
	handler := bridge.PipelineHandler()

	evt := pipeline.PipelineEvent{
		Type:      pipeline.EventStageStarted,
		NodeID:    "codergen_1",
		Message:   "executing node codergen_1",
		Timestamp: time.Date(2026, 2, 9, 12, 0, 0, 0, time.UTC),
	}

	handler(evt)

	msg, ok := received.(EngineEventMsg)
	if !ok {
		t.Fatalf("received message is %T, want EngineEventMsg", received)
	}
	if msg.PipelineEvent == nil {
		t.Fatal("PipelineEvent is nil")
	}
	if msg.PipelineEvent.Type != pipeline.EventStageStarted {
		t.Errorf("PipelineEvent.Type = %q, want %q", msg.PipelineEvent.Type, pipeline.EventStageStarted)
	}
	if msg.PipelineEvent.NodeID != "codergen_1" {
		t.Errorf("PipelineEvent.NodeID = %q, want %q", msg.PipelineEvent.NodeID, "codergen_1")
	}
	if msg.AgentEvent != nil {
		t.Error("AgentEvent should be nil for pipeline event")
	}
}

func TestEventBridgeAgentHandler(t *testing.T) {
	var received tea.Msg
	send := func(msg tea.Msg) {
		received = msg
	}

	bridge := NewEventBridge(send)
	handler := bridge.AgentHandler()

	evt := agent.Event{
		Type:      agent.EventToolCallStart,
		Timestamp: time.Now(),
		ToolName:  "read_file",
	}

	handler(evt)

	msg, ok := received.(EngineEventMsg)
	if !ok {
		t.Fatalf("received message is %T, want EngineEventMsg", received)
	}
	if msg.AgentEvent == nil {
		t.Fatal("AgentEvent is nil")
	}
	if msg.AgentEvent.Type != agent.EventToolCallStart {
		t.Errorf("AgentEvent.Type = %q, want %q", msg.AgentEvent.Type, agent.EventToolCallStart)
	}
	if msg.PipelineEvent != nil {
		t.Error("PipelineEvent should be nil for agent event")
	}
}

func TestEventBridgeHandleMultipleEvents(t *testing.T) {
	var mu sync.Mutex
	var received []EngineEventMsg
	send := func(msg tea.Msg) {
		mu.Lock()
		defer mu.Unlock()
		if m, ok := msg.(EngineEventMsg); ok {
			received = append(received, m)
		}
	}

	bridge := NewEventBridge(send)
	pHandler := bridge.PipelineHandler()

	events := []pipeline.PipelineEvent{
		{Type: pipeline.EventPipelineStarted, Timestamp: time.Now()},
		{Type: pipeline.EventStageStarted, NodeID: "node_a", Timestamp: time.Now()},
		{Type: pipeline.EventStageCompleted, NodeID: "node_a", Timestamp: time.Now()},
		{Type: pipeline.EventStageStarted, NodeID: "node_b", Timestamp: time.Now()},
		{Type: pipeline.EventStageFailed, NodeID: "node_b", Timestamp: time.Now()},
		{Type: pipeline.EventPipelineFailed, Timestamp: time.Now()},
	}

	for _, evt := range events {
		pHandler(evt)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != len(events) {
		t.Fatalf("received %d messages, want %d", len(received), len(events))
	}

	for i, msg := range received {
		if msg.PipelineEvent == nil {
			t.Errorf("message[%d].PipelineEvent is nil", i)
			continue
		}
		if msg.PipelineEvent.Type != events[i].Type {
			t.Errorf("message[%d].PipelineEvent.Type = %q, want %q", i, msg.PipelineEvent.Type, events[i].Type)
		}
		if msg.PipelineEvent.NodeID != events[i].NodeID {
			t.Errorf("message[%d].PipelineEvent.NodeID = %q, want %q", i, msg.PipelineEvent.NodeID, events[i].NodeID)
		}
	}
}

func TestWaitForHumanGateCmdReceivesRequest(t *testing.T) {
	ch := make(chan gateRequest, 1)
	ch <- gateRequest{
		question: "Approve deployment?",
		options:  []string{"yes", "no"},
	}

	cmd := WaitForHumanGateCmd(ch)
	if cmd == nil {
		t.Fatal("WaitForHumanGateCmd returned nil")
	}

	msg := cmd()
	gate, ok := msg.(HumanGateRequestMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want HumanGateRequestMsg", msg)
	}
	if gate.Question != "Approve deployment?" {
		t.Errorf("Question = %q, want %q", gate.Question, "Approve deployment?")
	}
	if len(gate.Options) != 2 {
		t.Fatalf("Options length = %d, want 2", len(gate.Options))
	}
	if gate.Options[0] != "yes" {
		t.Errorf("Options[0] = %q, want %q", gate.Options[0], "yes")
	}
	if gate.Options[1] != "no" {
		t.Errorf("Options[1] = %q, want %q", gate.Options[1], "no")
	}
}

func TestTickCmdSendsAfterInterval(t *testing.T) {
	interval := 10 * time.Millisecond
	cmd := TickCmd(interval)
	if cmd == nil {
		t.Fatal("TickCmd returned nil")
	}

	before := time.Now()
	msg := cmd()
	elapsed := time.Since(before)

	tick, ok := msg.(TickMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want TickMsg", msg)
	}
	if tick.Time.IsZero() {
		t.Error("TickMsg.Time is zero")
	}

	// Should have slept at least the interval
	if elapsed < interval {
		t.Errorf("elapsed = %v, want >= %v", elapsed, interval)
	}
}

func TestTickCmdTimingApproximate(t *testing.T) {
	interval := 50 * time.Millisecond
	cmd := TickCmd(interval)

	before := time.Now()
	msg := cmd()
	elapsed := time.Since(before)

	tick, ok := msg.(TickMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want TickMsg", msg)
	}

	// TickMsg.Time should be approximately when the tick fired
	timeDrift := tick.Time.Sub(before)
	if timeDrift < interval {
		t.Errorf("tick.Time is %v after start, want >= %v", timeDrift, interval)
	}

	// Should not take excessively long (allow 3x the interval for CI slack)
	maxElapsed := 3 * interval
	if elapsed > maxElapsed {
		t.Errorf("elapsed = %v, want <= %v", elapsed, maxElapsed)
	}
}
