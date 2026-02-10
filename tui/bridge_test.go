// ABOUTME: Tests for the EventBridge, RunPipelineCmd, WaitForHumanGateCmd, and TickCmd.
// ABOUTME: Validates the bridge layer connecting attractor engine events to the Bubble Tea message loop.
package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/2389-research/makeatron/attractor"
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

func TestEventBridgeHandleEvent(t *testing.T) {
	var received tea.Msg
	send := func(msg tea.Msg) {
		received = msg
	}

	bridge := NewEventBridge(send)
	evt := attractor.EngineEvent{
		Type:      attractor.EventStageStarted,
		NodeID:    "codergen_1",
		Data:      map[string]any{"model": "claude-4"},
		Timestamp: time.Date(2026, 2, 9, 12, 0, 0, 0, time.UTC),
	}

	bridge.HandleEvent(evt)

	msg, ok := received.(EngineEventMsg)
	if !ok {
		t.Fatalf("received message is %T, want EngineEventMsg", received)
	}
	if msg.Event.Type != attractor.EventStageStarted {
		t.Errorf("Event.Type = %q, want %q", msg.Event.Type, attractor.EventStageStarted)
	}
	if msg.Event.NodeID != "codergen_1" {
		t.Errorf("Event.NodeID = %q, want %q", msg.Event.NodeID, "codergen_1")
	}
	if msg.Event.Data["model"] != "claude-4" {
		t.Errorf("Event.Data[model] = %v, want %q", msg.Event.Data["model"], "claude-4")
	}
	if !msg.Event.Timestamp.Equal(evt.Timestamp) {
		t.Errorf("Event.Timestamp = %v, want %v", msg.Event.Timestamp, evt.Timestamp)
	}
}

func TestEventBridgeHandleEventMultiple(t *testing.T) {
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

	events := []attractor.EngineEvent{
		{Type: attractor.EventPipelineStarted, Timestamp: time.Now()},
		{Type: attractor.EventStageStarted, NodeID: "node_a", Timestamp: time.Now()},
		{Type: attractor.EventStageCompleted, NodeID: "node_a", Timestamp: time.Now()},
		{Type: attractor.EventStageStarted, NodeID: "node_b", Timestamp: time.Now()},
		{Type: attractor.EventStageFailed, NodeID: "node_b", Timestamp: time.Now()},
		{Type: attractor.EventPipelineFailed, Timestamp: time.Now()},
	}

	for _, evt := range events {
		bridge.HandleEvent(evt)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != len(events) {
		t.Fatalf("received %d messages, want %d", len(received), len(events))
	}

	for i, msg := range received {
		if msg.Event.Type != events[i].Type {
			t.Errorf("message[%d].Event.Type = %q, want %q", i, msg.Event.Type, events[i].Type)
		}
		if msg.Event.NodeID != events[i].NodeID {
			t.Errorf("message[%d].Event.NodeID = %q, want %q", i, msg.Event.NodeID, events[i].NodeID)
		}
	}
}

func TestRunPipelineCmdSuccess(t *testing.T) {
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	source := `digraph test { start [shape=Mdiamond]; finish [shape=Msquare]; start -> finish }`

	cmd := RunPipelineCmd(context.Background(), engine, source)
	if cmd == nil {
		t.Fatal("RunPipelineCmd returned nil")
	}

	msg := cmd()
	result, ok := msg.(PipelineResultMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want PipelineResultMsg", msg)
	}
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Result == nil {
		t.Fatal("Result is nil, want non-nil")
	}
	if len(result.Result.CompletedNodes) == 0 {
		t.Error("CompletedNodes is empty, want at least one node")
	}
}

func TestRunPipelineCmdError(t *testing.T) {
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	source := `this is not valid DOT at all {{{`

	cmd := RunPipelineCmd(context.Background(), engine, source)
	if cmd == nil {
		t.Fatal("RunPipelineCmd returned nil")
	}

	msg := cmd()
	result, ok := msg.(PipelineResultMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want PipelineResultMsg", msg)
	}
	if result.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(result.Err.Error(), "parse") {
		t.Errorf("error = %q, want it to contain 'parse'", result.Err.Error())
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

func TestWireHumanGateWiresInterviewer(t *testing.T) {
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
		Handlers:     attractor.DefaultHandlerRegistry(),
	})

	gate := NewHumanGateModel()
	WireHumanGate(engine, &gate)

	handler := engine.GetHandler("wait.human")
	if handler == nil {
		t.Fatal("expected wait.human handler")
	}
	hh, ok := handler.(*attractor.WaitForHumanHandler)
	if !ok {
		t.Fatalf("expected *WaitForHumanHandler, got %T", handler)
	}
	if hh.Interviewer == nil {
		t.Error("expected Interviewer to be wired")
	}
	if hh.Interviewer != &gate {
		t.Error("expected Interviewer to be the HumanGateModel")
	}
}

func TestWireHumanGateNilHandler(t *testing.T) {
	// Engine with no handlers should not panic
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})

	gate := NewHumanGateModel()
	WireHumanGate(engine, &gate) // should not panic
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
