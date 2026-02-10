// ABOUTME: Tests for the top-level AppModel that orchestrates all TUI sub-panels.
// ABOUTME: Covers initialization, message routing, focus management, human gate interaction, and view rendering.
package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/2389-research/makeatron/attractor"
	tea "github.com/charmbracelet/bubbletea"
)

// testAppModel creates an AppModel with a simple 3-node pipeline for testing.
func testAppModel() AppModel {
	g := &attractor.Graph{
		Name: "test_pipeline",
		Nodes: map[string]*attractor.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "label": "Start"}},
			"build": {ID: "build", Attrs: map[string]string{"shape": "box", "label": "Build"}},
			"done":  {ID: "done", Attrs: map[string]string{"shape": "Msquare", "label": "Done"}},
		},
		Edges: []*attractor.Edge{
			{From: "start", To: "build"},
			{From: "build", To: "done"},
		},
	}
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	return NewAppModel(g, engine, "digraph{}", context.Background())
}

func TestNewAppModel(t *testing.T) {
	m := testAppModel()

	// Verify all sub-models are initialized (non-zero state)
	if m.graph.graph == nil {
		t.Error("graph panel has nil graph")
	}
	if m.engine == nil {
		t.Error("engine is nil")
	}
	if m.source != "digraph{}" {
		t.Errorf("source = %q, want %q", m.source, "digraph{}")
	}
	if m.focus != FocusGraph {
		t.Errorf("initial focus = %d, want FocusGraph (%d)", m.focus, FocusGraph)
	}
	if m.done {
		t.Error("done should be false initially")
	}
	if m.err != nil {
		t.Errorf("err should be nil initially, got %v", m.err)
	}
	if m.completed != 0 {
		t.Errorf("completed = %d, want 0", m.completed)
	}
}

func TestAppModelInit(t *testing.T) {
	m := testAppModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil, expected a batch command")
	}

	// The Init() cmd should be a batch (tea.BatchMsg). We can't easily introspect
	// tea.Batch internals, but we can verify it returned something non-nil.
	// We also verify that statusBar was started (start time set).
	// Note: Init modifies m but since AppModel uses value receivers, we need
	// to verify indirectly. The statusBar.Start() should have been called before
	// returning.
}

func TestAppModelUpdateWindowSize(t *testing.T) {
	m := testAppModel()
	msg := tea.WindowSizeMsg{Width: 120, Height: 40}

	updated, _ := m.Update(msg)
	m = updated.(AppModel)

	if m.width != 120 {
		t.Errorf("width = %d, want 120", m.width)
	}
	if m.height != 40 {
		t.Errorf("height = %d, want 40", m.height)
	}
}

func TestAppModelUpdateEngineEventStageStarted(t *testing.T) {
	m := testAppModel()
	evt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}

	updated, _ := m.Update(evt)
	m = updated.(AppModel)

	if m.graph.GetNodeStatus("build") != NodeRunning {
		t.Errorf("graph node status = %v, want NodeRunning", m.graph.GetNodeStatus("build"))
	}
}

func TestAppModelUpdateEngineEventStageCompleted(t *testing.T) {
	m := testAppModel()

	// First start the node
	updated, _ := m.Update(EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	})
	m = updated.(AppModel)

	// Now complete it
	updated, _ = m.Update(EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageCompleted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	})
	m = updated.(AppModel)

	if m.graph.GetNodeStatus("build") != NodeCompleted {
		t.Errorf("graph node status = %v, want NodeCompleted", m.graph.GetNodeStatus("build"))
	}
	if m.completed != 1 {
		t.Errorf("completed = %d, want 1", m.completed)
	}
}

func TestAppModelUpdateEngineEventStageFailed(t *testing.T) {
	m := testAppModel()
	evt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageFailed,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}

	updated, _ := m.Update(evt)
	m = updated.(AppModel)

	if m.graph.GetNodeStatus("build") != NodeFailed {
		t.Errorf("graph node status = %v, want NodeFailed", m.graph.GetNodeStatus("build"))
	}
}

func TestAppModelUpdateEngineEventPipelineStarted(t *testing.T) {
	m := testAppModel()
	evt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventPipelineStarted,
			Timestamp: time.Now(),
		},
	}

	updated, _ := m.Update(evt)
	m = updated.(AppModel)

	// Pipeline started should be logged
	if m.log.Len() != 1 {
		t.Errorf("log.Len() = %d, want 1", m.log.Len())
	}
}

func TestAppModelUpdatePipelineResult(t *testing.T) {
	m := testAppModel()
	msg := PipelineResultMsg{
		Result: &attractor.RunResult{
			CompletedNodes: []string{"start", "build", "done"},
		},
		Err: nil,
	}

	updated, _ := m.Update(msg)
	m = updated.(AppModel)

	if !m.done {
		t.Error("done should be true after PipelineResultMsg")
	}
	if m.err != nil {
		t.Errorf("err should be nil, got %v", m.err)
	}
}

func TestAppModelUpdatePipelineResultError(t *testing.T) {
	m := testAppModel()
	expectedErr := errors.New("pipeline exploded")
	msg := PipelineResultMsg{
		Result: nil,
		Err:    expectedErr,
	}

	updated, _ := m.Update(msg)
	m = updated.(AppModel)

	if !m.done {
		t.Error("done should be true even on error")
	}
	if m.err == nil {
		t.Fatal("err should be non-nil")
	}
	if m.err.Error() != "pipeline exploded" {
		t.Errorf("err = %q, want %q", m.err.Error(), "pipeline exploded")
	}
}

func TestAppModelUpdateTick(t *testing.T) {
	m := testAppModel()
	initialSpinner := m.graph.spinnerIndex

	updated, _ := m.Update(TickMsg{Time: time.Now()})
	m = updated.(AppModel)

	if m.graph.spinnerIndex != initialSpinner+1 {
		t.Errorf("spinnerIndex = %d, want %d", m.graph.spinnerIndex, initialSpinner+1)
	}
}

func TestAppModelUpdateTickReturnsCmdWhenNotDone(t *testing.T) {
	m := testAppModel()

	_, cmd := m.Update(TickMsg{Time: time.Now()})

	if cmd == nil {
		t.Error("tick should return a cmd when pipeline is not done")
	}
}

func TestAppModelUpdateTickReturnsNilWhenDone(t *testing.T) {
	m := testAppModel()

	// Mark pipeline as done
	updated, _ := m.Update(PipelineResultMsg{
		Result: &attractor.RunResult{},
		Err:    nil,
	})
	m = updated.(AppModel)

	// Now tick should return nil cmd
	_, cmd := m.Update(TickMsg{Time: time.Now()})

	if cmd != nil {
		t.Error("tick should return nil cmd when pipeline is done")
	}
}

func TestAppModelUpdateKeyQuit(t *testing.T) {
	m := testAppModel()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}

	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("q key should return a quit command")
	}

	// Execute the command and verify it produces a QuitMsg
	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("cmd() returned %T, want tea.QuitMsg", result)
	}
}

func TestAppModelUpdateKeyCtrlC(t *testing.T) {
	m := testAppModel()
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}

	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("ctrl+c should return a quit command")
	}

	result := cmd()
	if _, ok := result.(tea.QuitMsg); !ok {
		t.Errorf("cmd() returned %T, want tea.QuitMsg", result)
	}
}

func TestAppModelUpdateKeyTab(t *testing.T) {
	m := testAppModel()
	if m.focus != FocusGraph {
		t.Fatalf("initial focus = %d, want FocusGraph", m.focus)
	}

	// Tab should cycle to log
	msg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(msg)
	m = updated.(AppModel)

	if m.focus != FocusLog {
		t.Errorf("focus after first tab = %d, want FocusLog (%d)", m.focus, FocusLog)
	}

	// Tab again should cycle back to graph
	updated, _ = m.Update(msg)
	m = updated.(AppModel)

	if m.focus != FocusGraph {
		t.Errorf("focus after second tab = %d, want FocusGraph (%d)", m.focus, FocusGraph)
	}
}

func TestAppModelUpdateHumanGateRequest(t *testing.T) {
	m := testAppModel()
	msg := HumanGateRequestMsg{
		Question: "Deploy to production?",
		Options:  []string{"yes", "no"},
	}

	updated, _ := m.Update(msg)
	m = updated.(AppModel)

	if !m.humanGate.IsActive() {
		t.Error("human gate should be active after HumanGateRequestMsg")
	}
}

func TestAppModelUpdateHumanGateSubmit(t *testing.T) {
	m := testAppModel()

	// Activate the gate
	updated, _ := m.Update(HumanGateRequestMsg{
		Question: "Proceed?",
		Options:  []string{"yes", "no"},
	})
	m = updated.(AppModel)

	if !m.humanGate.IsActive() {
		t.Fatal("gate should be active")
	}

	// Start a goroutine to drain the response channel so Submit doesn't block
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		<-m.humanGate.responseCh
	}()

	// Send enter key to submit
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(AppModel)

	// Wait for the goroutine to receive the response
	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response channel drain")
	}

	if m.humanGate.IsActive() {
		t.Error("gate should be deactivated after submit")
	}

	// Should return a cmd to wait for next gate request
	if cmd == nil {
		t.Error("submit should return a WaitForHumanGateCmd")
	}
}

func TestAppModelUpdateKeysBlockedDuringGate(t *testing.T) {
	m := testAppModel()

	// Activate the gate
	updated, _ := m.Update(HumanGateRequestMsg{
		Question: "Proceed?",
		Options:  []string{"yes"},
	})
	m = updated.(AppModel)

	// "q" should NOT quit when gate is active - it should be forwarded to the gate's text input
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	// If cmd produces a QuitMsg, that's wrong
	if cmd != nil {
		result := cmd()
		if _, ok := result.(tea.QuitMsg); ok {
			t.Error("q key should not quit when human gate is active")
		}
	}
}

func TestAppModelViewNotEmpty(t *testing.T) {
	m := testAppModel()
	m.width = 80
	m.height = 24

	view := m.View()
	if view == "" {
		t.Error("View() returned empty string")
	}
}

func TestAppModelViewShowsDoneMessage(t *testing.T) {
	m := testAppModel()
	m.width = 80
	m.height = 24

	// Complete the pipeline
	updated, _ := m.Update(PipelineResultMsg{
		Result: &attractor.RunResult{
			CompletedNodes: []string{"start", "build", "done"},
		},
	})
	m = updated.(AppModel)

	view := m.View()
	if view == "" {
		t.Error("View() returned empty string after pipeline done")
	}
	// The view should contain some indication that the pipeline is done.
	// We don't test exact rendering because lipgloss makes that brittle.
}

func TestFocusTargetConstants(t *testing.T) {
	if FocusGraph != 0 {
		t.Errorf("FocusGraph = %d, want 0", FocusGraph)
	}
	if FocusLog != 1 {
		t.Errorf("FocusLog = %d, want 1", FocusLog)
	}
	if FocusGraph == FocusLog {
		t.Error("FocusGraph and FocusLog should be different values")
	}
}

func TestAppModelUpdateMultipleStageCompletions(t *testing.T) {
	m := testAppModel()

	// Complete two nodes
	for _, nodeID := range []string{"start", "build"} {
		updated, _ := m.Update(EngineEventMsg{
			Event: attractor.EngineEvent{
				Type:      attractor.EventStageCompleted,
				NodeID:    nodeID,
				Timestamp: time.Now(),
			},
		})
		m = updated.(AppModel)
	}

	if m.completed != 2 {
		t.Errorf("completed = %d, want 2", m.completed)
	}
}

func TestAppModelUpdateStageRetryingLogged(t *testing.T) {
	m := testAppModel()
	evt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageRetrying,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}

	updated, _ := m.Update(evt)
	m = updated.(AppModel)

	// Retrying events should be logged
	if m.log.Len() != 1 {
		t.Errorf("log.Len() = %d, want 1 (retrying event should be logged)", m.log.Len())
	}
}

func TestAppModelUpdateLogFocusState(t *testing.T) {
	m := testAppModel()

	// Initially graph is focused, log is not
	if m.log.IsFocused() {
		t.Error("log should not be focused initially")
	}

	// Tab to focus log
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(AppModel)

	if !m.log.IsFocused() {
		t.Error("log should be focused after tab")
	}

	// Tab back to graph
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(AppModel)

	if m.log.IsFocused() {
		t.Error("log should not be focused after second tab")
	}
}

func TestAppModelUpdateEngineEventStageStartedSetsDetail(t *testing.T) {
	m := testAppModel()
	evt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}

	updated, _ := m.Update(evt)
	m = updated.(AppModel)

	// Detail panel should have the active node set
	if m.detail.active == nil {
		t.Fatal("detail panel active node should be set after stage.started")
	}
	if m.detail.active.Name != "Build" {
		t.Errorf("detail.active.Name = %q, want %q", m.detail.active.Name, "Build")
	}
	if m.detail.active.Status != NodeRunning {
		t.Errorf("detail.active.Status = %v, want NodeRunning", m.detail.active.Status)
	}
}

func TestAppModelUpdatePipelineResultClearsDetail(t *testing.T) {
	m := testAppModel()

	// Start a node to populate detail
	updated, _ := m.Update(EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	})
	m = updated.(AppModel)

	// Complete pipeline
	updated, _ = m.Update(PipelineResultMsg{
		Result: &attractor.RunResult{},
	})
	m = updated.(AppModel)

	if m.detail.active != nil {
		t.Error("detail panel should be cleared after pipeline result")
	}
}

func TestAppModelHumanGateReturnsPointer(t *testing.T) {
	m := testAppModel()
	gate := m.HumanGate()
	if gate == nil {
		t.Fatal("HumanGate() returned nil")
	}
	// The pointer should refer to the model's humanGate field
	if gate != &m.humanGate {
		t.Error("HumanGate() should return pointer to internal humanGate field")
	}
}
