// ABOUTME: Tests for the StreamModel inline Bubble Tea progress display.
// ABOUTME: Covers constructor, Init, Update message handling, View rendering, verbose mode, and result channel.
package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
	tea "github.com/charmbracelet/bubbletea"
)

// markCompleted marks the first n nodes in order as NodeCompleted for test setup.
func markCompleted(m *StreamModel, n int) {
	for i, id := range m.nodeOrder {
		if i >= n {
			break
		}
		m.statuses[id] = NodeCompleted
	}
}

// testStreamGraph creates a simple linear DAG: start -> build -> test -> done.
func testStreamGraph() *attractor.Graph {
	return &attractor.Graph{
		Name: "stream_test",
		Nodes: map[string]*attractor.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "label": "Start"}},
			"build": {ID: "build", Attrs: map[string]string{"shape": "box", "label": "Build"}},
			"test":  {ID: "test", Attrs: map[string]string{"shape": "box", "label": "Test"}},
			"done":  {ID: "done", Attrs: map[string]string{"shape": "Msquare", "label": "Done"}},
		},
		Edges: []*attractor.Edge{
			{From: "start", To: "build"},
			{From: "build", To: "test"},
			{From: "test", To: "done"},
		},
	}
}

func testStreamModel() StreamModel {
	g := testStreamGraph()
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	return NewStreamModel(g, engine, "examples/simple.dot", context.Background(), false)
}

func testStreamModelVerbose() StreamModel {
	g := testStreamGraph()
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	return NewStreamModel(g, engine, "examples/simple.dot", context.Background(), true)
}

func TestNewStreamModelSetsNodeOrder(t *testing.T) {
	m := testStreamModel()

	if len(m.nodeOrder) != 4 {
		t.Fatalf("expected 4 nodes in order, got %d", len(m.nodeOrder))
	}

	// start should come before build, build before test, test before done (topological)
	indexOf := func(id string) int {
		for i, n := range m.nodeOrder {
			if n == id {
				return i
			}
		}
		return -1
	}

	if indexOf("start") >= indexOf("build") {
		t.Error("start should come before build in topological order")
	}
	if indexOf("build") >= indexOf("test") {
		t.Error("build should come before test in topological order")
	}
	if indexOf("test") >= indexOf("done") {
		t.Error("test should come before done in topological order")
	}
}

func TestNewStreamModelInitializesAllPending(t *testing.T) {
	m := testStreamModel()

	for _, id := range m.nodeOrder {
		status := m.statuses[id]
		if status != NodePending {
			t.Errorf("node %q: expected NodePending, got %v", id, status)
		}
	}
}

func TestNewStreamModelTotalCount(t *testing.T) {
	m := testStreamModel()

	if m.total != 4 {
		t.Errorf("total = %d, want 4", m.total)
	}
	if m.completedCount() != 0 {
		t.Errorf("completed = %d, want 0", m.completedCount())
	}
}

func TestStreamModelInitReturnsBatch(t *testing.T) {
	m := testStreamModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned nil, expected a batch command")
	}
}

func TestStreamModelHandleStageStarted(t *testing.T) {
	m := testStreamModel()

	msg := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}

	updated, _ := m.Update(msg)
	m = updated.(StreamModel)

	if m.statuses["build"] != NodeRunning {
		t.Errorf("expected build to be NodeRunning, got %v", m.statuses["build"])
	}
	if _, ok := m.startedAt["build"]; !ok {
		t.Error("expected startedAt to be set for build")
	}
}

func TestStreamModelHandleStageCompleted(t *testing.T) {
	m := testStreamModel()

	// First start the node
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	// Then complete it
	completed := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageCompleted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ = m.Update(completed)
	m = updated.(StreamModel)

	if m.statuses["build"] != NodeCompleted {
		t.Errorf("expected build to be NodeCompleted, got %v", m.statuses["build"])
	}
	if m.completedCount() != 1 {
		t.Errorf("completed = %d, want 1", m.completedCount())
	}
	if _, ok := m.durations["build"]; !ok {
		t.Error("expected duration to be recorded for build")
	}
}

func TestStreamModelHandleStageFailed(t *testing.T) {
	m := testStreamModel()

	// Start then fail
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	failed := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageFailed,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"reason": "compilation error"},
		},
	}
	updated, _ = m.Update(failed)
	m = updated.(StreamModel)

	if m.statuses["build"] != NodeFailed {
		t.Errorf("expected build to be NodeFailed, got %v", m.statuses["build"])
	}
	if _, ok := m.durations["build"]; !ok {
		t.Error("expected duration to be recorded for failed build")
	}
}

func TestStreamModelHandlePipelineResult(t *testing.T) {
	m := testStreamModel()

	msg := PipelineResultMsg{
		Result: &attractor.RunResult{
			CompletedNodes: []string{"start", "build", "test", "done"},
		},
		Err: nil,
	}

	updated, cmd := m.Update(msg)
	m = updated.(StreamModel)

	if !m.done {
		t.Error("expected done to be true after PipelineResultMsg")
	}
	if m.err != nil {
		t.Errorf("expected nil error, got %v", m.err)
	}

	// cmd should be tea.Quit
	if cmd == nil {
		t.Fatal("expected a quit command after pipeline result")
	}

	// Result should be readable from the channel
	select {
	case result := <-m.ResultCh():
		if result.Err != nil {
			t.Errorf("expected nil error on result channel, got %v", result.Err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out reading from result channel")
	}
}

func TestStreamModelHandlePipelineResultWithError(t *testing.T) {
	m := testStreamModel()

	msg := PipelineResultMsg{
		Err: context.Canceled,
	}

	updated, _ := m.Update(msg)
	m = updated.(StreamModel)

	if !m.done {
		t.Error("expected done to be true")
	}
	if m.err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", m.err)
	}
}

func TestStreamModelHandleTick(t *testing.T) {
	m := testStreamModel()

	initialIdx := m.spinnerIdx
	msg := TickMsg{Time: time.Now()}

	updated, cmd := m.Update(msg)
	m = updated.(StreamModel)

	if m.spinnerIdx != initialIdx+1 {
		t.Errorf("spinnerIdx = %d, want %d", m.spinnerIdx, initialIdx+1)
	}
	if cmd == nil {
		t.Error("expected tick command to continue when not done")
	}
}

func TestStreamModelHandleTickWhenDone(t *testing.T) {
	m := testStreamModel()
	m.done = true

	msg := TickMsg{Time: time.Now()}
	_, cmd := m.Update(msg)

	if cmd != nil {
		t.Error("expected nil command when done (no more ticks)")
	}
}

func TestStreamModelViewShowsHeader(t *testing.T) {
	m := testStreamModel()
	view := m.View()

	if !strings.Contains(view, "mammoth") {
		t.Error("view should contain 'mammoth' in header")
	}
	if !strings.Contains(view, "examples/simple.dot") {
		t.Error("view should contain the source filename")
	}
}

func TestStreamModelViewShowsNodeStatuses(t *testing.T) {
	m := testStreamModel()

	// Complete one node
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "start",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	completed := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageCompleted,
			NodeID:    "start",
			Timestamp: time.Now(),
		},
	}
	updated, _ = m.Update(completed)
	m = updated.(StreamModel)

	view := m.View()

	// Completed nodes should show checkmark
	if !strings.Contains(view, "✓") {
		t.Error("view should contain ✓ for completed nodes")
	}

	// Pending nodes should show their labels
	if !strings.Contains(view, "Build") {
		t.Error("view should show 'Build' label for pending node")
	}
}

func TestStreamModelViewShowsRunningSpinner(t *testing.T) {
	m := testStreamModel()

	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	view := m.View()

	if !strings.Contains(view, "running...") {
		t.Error("view should show 'running...' for active node")
	}
}

func TestStreamModelViewShowsProgressLine(t *testing.T) {
	m := testStreamModel()

	// Complete one node to get a non-zero count
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "start",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	completed := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageCompleted,
			NodeID:    "start",
			Timestamp: time.Now(),
		},
	}
	updated, _ = m.Update(completed)
	m = updated.(StreamModel)

	// Trigger pipeline start so elapsed time works
	m.pipelineStart = time.Now().Add(-5 * time.Second)

	view := m.View()

	if !strings.Contains(view, "1/4") {
		t.Errorf("view should contain '1/4' progress, got:\n%s", view)
	}
	if !strings.Contains(view, "complete") {
		t.Errorf("view should contain 'complete' in progress line, got:\n%s", view)
	}
}

func TestStreamModelViewShowsCompletionLine(t *testing.T) {
	m := testStreamModel()
	m.done = true
	markCompleted(&m, 4)
	m.pipelineStart = time.Now().Add(-10 * time.Second)

	view := m.View()

	if !strings.Contains(view, "✓") {
		t.Error("completion view should contain ✓")
	}
	if !strings.Contains(view, "4/4") {
		t.Errorf("completion view should contain '4/4', got:\n%s", view)
	}
}

func TestStreamModelViewShowsFailureLine(t *testing.T) {
	m := testStreamModel()
	m.done = true
	m.err = context.Canceled
	markCompleted(&m, 2)
	m.pipelineStart = time.Now().Add(-10 * time.Second)

	view := m.View()

	if !strings.Contains(view, "✗") {
		t.Error("failure view should contain ✗")
	}
	if !strings.Contains(view, "FAILED") {
		t.Errorf("failure view should contain 'FAILED', got:\n%s", view)
	}
}

func TestStreamModelHandleHumanGate(t *testing.T) {
	m := testStreamModel()

	msg := HumanGateRequestMsg{
		Question: "Approve deployment?",
		Options:  []string{"yes", "no"},
	}

	updated, _ := m.Update(msg)
	m = updated.(StreamModel)

	if !m.humanGate.IsActive() {
		t.Error("human gate should be active after HumanGateRequestMsg")
	}
}

func TestStreamModelVerboseShowsAgentEvents(t *testing.T) {
	m := testStreamModelVerbose()

	// Start a node
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	// Send an agent tool call event
	toolEvt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventAgentToolCallStart,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"tool_name": "read_file"},
		},
	}
	updated, _ = m.Update(toolEvt)
	m = updated.(StreamModel)

	// Check that agent lines were recorded
	if lines, ok := m.agentLines["build"]; !ok || len(lines) == 0 {
		t.Error("verbose mode should record agent event lines for running node")
	}

	view := m.View()
	if !strings.Contains(view, "read_file") {
		t.Errorf("verbose view should show tool name, got:\n%s", view)
	}
}

func TestStreamModelVerboseShowsLLMTurn(t *testing.T) {
	m := testStreamModelVerbose()

	// Start a node
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	// Send an LLM turn event
	llmEvt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventAgentLLMTurn,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"input_tokens": 1200, "output_tokens": 340},
		},
	}
	updated, _ = m.Update(llmEvt)
	m = updated.(StreamModel)

	view := m.View()
	if !strings.Contains(view, "llm turn") {
		t.Errorf("verbose view should show 'llm turn', got:\n%s", view)
	}
}

func TestStreamModelNonVerboseHidesAgentEvents(t *testing.T) {
	m := testStreamModel() // non-verbose

	// Start a node
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	// Send an agent tool call event
	toolEvt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventAgentToolCallStart,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"tool_name": "read_file"},
		},
	}
	updated, _ = m.Update(toolEvt)
	m = updated.(StreamModel)

	// Agent lines should not be recorded in non-verbose mode
	if lines, ok := m.agentLines["build"]; ok && len(lines) > 0 {
		t.Error("non-verbose mode should not record agent event lines")
	}
}

func TestStreamModelResultChannel(t *testing.T) {
	m := testStreamModel()

	ch := m.ResultCh()
	if ch == nil {
		t.Fatal("ResultCh() returned nil")
	}

	// Channel should be empty before pipeline completes
	select {
	case <-ch:
		t.Fatal("result channel should be empty before pipeline completes")
	default:
		// expected
	}
}

func TestStreamModelCtrlCQuits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g := testStreamGraph()
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	m := NewStreamModel(g, engine, "test.dot", ctx, false)

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command on ctrl+c")
	}
}

func TestStreamModelHumanGateKeyRouting(t *testing.T) {
	m := testStreamModel()

	// Activate human gate
	gateMsg := HumanGateRequestMsg{
		Question: "Continue?",
		Options:  []string{"yes"},
	}
	updated, _ := m.Update(gateMsg)
	m = updated.(StreamModel)

	// Type a character - should go to human gate, not trigger quit
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}
	updated, cmd := m.Update(keyMsg)
	m = updated.(StreamModel)

	// cmd might be a textinput blink command, which is fine — no assertion needed
	_ = cmd
	if m.humanGate.IsActive() != true {
		t.Error("human gate should remain active after typing")
	}
}

func TestStreamModelPipelineStartedSetsTime(t *testing.T) {
	m := testStreamModel()

	msg := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventPipelineStarted,
			Timestamp: time.Now(),
		},
	}

	updated, _ := m.Update(msg)
	m = updated.(StreamModel)

	if m.pipelineStart.IsZero() {
		t.Error("pipelineStart should be set after EventPipelineStarted")
	}
}

func TestNewStreamModelWithNilGraph(t *testing.T) {
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	m := NewStreamModel(nil, engine, "test.dot", context.Background(), false)

	if len(m.nodeOrder) != 0 {
		t.Errorf("expected empty nodeOrder for nil graph, got %d", len(m.nodeOrder))
	}
	if m.total != 0 {
		t.Errorf("expected total=0 for nil graph, got %d", m.total)
	}
}

func TestStreamModelViewNodeWithoutLabel(t *testing.T) {
	// Create a graph where a node has no label attribute
	g := &attractor.Graph{
		Name: "nolabel_test",
		Nodes: map[string]*attractor.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"done":  {ID: "done", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*attractor.Edge{
			{From: "start", To: "done"},
		},
	}
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	m := NewStreamModel(g, engine, "test.dot", context.Background(), false)

	view := m.View()

	// Should fall back to node ID
	if !strings.Contains(view, "start") {
		t.Errorf("view should contain node ID 'start' as fallback, got:\n%s", view)
	}
	if !strings.Contains(view, "done") {
		t.Errorf("view should contain node ID 'done' as fallback, got:\n%s", view)
	}
}

func TestStreamModelHumanGatePointer(t *testing.T) {
	m := testStreamModel()
	gate := m.HumanGate()
	if gate == nil {
		t.Fatal("HumanGate() returned nil")
	}
}

func TestStreamModelWindowSizeMsg(t *testing.T) {
	m := testStreamModel()

	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	updated, _ := m.Update(msg)
	m = updated.(StreamModel)

	if m.width != 120 {
		t.Errorf("width = %d, want 120", m.width)
	}
}

// --- Resume awareness tests ---

func testStreamModelWithResume() StreamModel {
	g := testStreamGraph()
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	info := &ResumeInfo{
		ResumedFrom:   "Build",
		PreviousNodes: []string{"start"},
	}
	return NewStreamModel(g, engine, "examples/simple.dot", context.Background(), false, WithResumeInfo(info))
}

func TestStreamModelResumeHeaderShown(t *testing.T) {
	m := testStreamModelWithResume()
	view := m.View()

	if !strings.Contains(view, "resuming from Build") {
		t.Errorf("expected resume header with 'resuming from Build', got:\n%s", view)
	}
}

func TestStreamModelResumePreMarksPreviousNodes(t *testing.T) {
	m := testStreamModelWithResume()

	// "start" should be pre-marked as completed
	if m.statuses["start"] != NodeCompleted {
		t.Errorf("expected start to be NodeCompleted, got %v", m.statuses["start"])
	}

	// "build" should still be pending
	if m.statuses["build"] != NodePending {
		t.Errorf("expected build to be NodePending, got %v", m.statuses["build"])
	}
}

func TestStreamModelResumeCompletedCount(t *testing.T) {
	m := testStreamModelWithResume()

	if m.completedCount() != 1 {
		t.Errorf("expected completed=1 (from resume), got %d", m.completedCount())
	}
}

func TestStreamModelResumePreviousRunLabel(t *testing.T) {
	m := testStreamModelWithResume()
	view := m.View()

	if !strings.Contains(view, "(previous run)") {
		t.Errorf("expected '(previous run)' for pre-completed node, got:\n%s", view)
	}
}

func TestStreamModelNoResumeHeaderWhenFresh(t *testing.T) {
	m := testStreamModel()
	view := m.View()

	if strings.Contains(view, "resuming from") {
		t.Error("expected no resume header for fresh model")
	}
}

func TestStreamModelResumeInfoNilOption(t *testing.T) {
	g := testStreamGraph()
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	// Passing nil ResumeInfo should be safe
	m := NewStreamModel(g, engine, "test.dot", context.Background(), false, WithResumeInfo(nil))

	if m.resumeInfo != nil {
		t.Error("expected nil resumeInfo when passing nil")
	}

	view := m.View()
	if strings.Contains(view, "resuming from") {
		t.Error("expected no resume header when ResumeInfo is nil")
	}
}

func TestStreamModelAccumulatesTokensFromLLMTurn(t *testing.T) {
	m := testStreamModel() // non-verbose

	// Start a node
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	// Send an LLM turn event
	llmEvt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventAgentLLMTurn,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"input_tokens": 1000, "output_tokens": 500},
		},
	}
	updated, _ = m.Update(llmEvt)
	m = updated.(StreamModel)

	if m.nodeTokens["build"] != 1500 {
		t.Errorf("expected nodeTokens[build]=1500, got %d", m.nodeTokens["build"])
	}
	if m.totalTokens != 1500 {
		t.Errorf("expected totalTokens=1500, got %d", m.totalTokens)
	}

	// Send another LLM turn
	llmEvt2 := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventAgentLLMTurn,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"input_tokens": 800, "output_tokens": 200},
		},
	}
	updated, _ = m.Update(llmEvt2)
	m = updated.(StreamModel)

	if m.nodeTokens["build"] != 2500 {
		t.Errorf("expected nodeTokens[build]=2500, got %d", m.nodeTokens["build"])
	}
	if m.totalTokens != 2500 {
		t.Errorf("expected totalTokens=2500, got %d", m.totalTokens)
	}
}

func TestStreamModelCapturesModelFromStageCompleted(t *testing.T) {
	m := testStreamModel()

	// Start then complete a node with codergen.model data
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	completed := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageCompleted,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data: map[string]any{
				"codergen.model":       "claude-sonnet-4-5-20250929",
				"codergen.tokens_used": 5000,
			},
		},
	}
	updated, _ = m.Update(completed)
	m = updated.(StreamModel)

	if m.nodeModels["build"] != "claude-sonnet-4-5-20250929" {
		t.Errorf("expected nodeModels[build]='claude-sonnet-4-5-20250929', got %q", m.nodeModels["build"])
	}
}

func TestStreamModelRendersTokensOnCompletedNode(t *testing.T) {
	m := testStreamModel()

	// Start, accumulate tokens, complete
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	llmEvt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventAgentLLMTurn,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"input_tokens": 10000, "output_tokens": 2340},
		},
	}
	updated, _ = m.Update(llmEvt)
	m = updated.(StreamModel)

	completed := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageCompleted,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data: map[string]any{
				"codergen.model": "claude-sonnet-4-5-20250929",
			},
		},
	}
	updated, _ = m.Update(completed)
	m = updated.(StreamModel)

	view := m.View()
	if !strings.Contains(view, "12,340 tok") {
		t.Errorf("completed node should show '12,340 tok', got:\n%s", view)
	}
	if !strings.Contains(view, "sonnet") {
		t.Errorf("completed node should show 'sonnet' model name, got:\n%s", view)
	}
}

func TestStreamModelRendersTokensInProgressLine(t *testing.T) {
	m := testStreamModel()
	m.pipelineStart = time.Now().Add(-5 * time.Second)

	// Complete a node with tokens
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "start",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	llmEvt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventAgentLLMTurn,
			NodeID:    "start",
			Timestamp: time.Now(),
			Data:      map[string]any{"input_tokens": 20000, "output_tokens": 5050},
		},
	}
	updated, _ = m.Update(llmEvt)
	m = updated.(StreamModel)

	completed := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageCompleted,
			NodeID:    "start",
			Timestamp: time.Now(),
		},
	}
	updated, _ = m.Update(completed)
	m = updated.(StreamModel)

	view := m.View()
	if !strings.Contains(view, "25,050 tokens") {
		t.Errorf("progress line should show '25,050 tokens', got:\n%s", view)
	}
}

func TestStreamModelRendersTokensOnRunningNode(t *testing.T) {
	m := testStreamModel()

	// Start node and send token data
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	llmEvt := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventAgentLLMTurn,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"input_tokens": 3500, "output_tokens": 1000},
		},
	}
	updated, _ = m.Update(llmEvt)
	m = updated.(StreamModel)

	view := m.View()
	if !strings.Contains(view, "4,500 tok") {
		t.Errorf("running node should show '4,500 tok', got:\n%s", view)
	}
}

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1,000"},
		{12340, "12,340"},
		{100000, "100,000"},
		{1000000, "1,000,000"},
		{-1, "-1"},
		{-5000, "-5000"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.input), func(t *testing.T) {
			got := formatTokenCount(tt.input)
			if got != tt.expected {
				t.Errorf("formatTokenCount(%d) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestShortModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-sonnet-4-5-20250929", "sonnet"},
		{"claude-opus-4-6", "opus"},
		{"claude-haiku-4-5-20251001", "haiku"},
		{"gpt-4o", "gpt-4o"},
		{"gpt-4o-mini", "gpt-4o-mini"},
		{"gemini-2.0-flash", "gemini-2.0-flash"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shortModelName(tt.input)
			if got != tt.expected {
				t.Errorf("shortModelName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStreamModelTracksTotalToolCalls(t *testing.T) {
	m := testStreamModel() // non-verbose

	// Start a node
	started := EngineEventMsg{
		Event: attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "build",
			Timestamp: time.Now(),
		},
	}
	updated, _ := m.Update(started)
	m = updated.(StreamModel)

	// Send tool call events — should count even in non-verbose mode
	for i := 0; i < 5; i++ {
		toolEvt := EngineEventMsg{
			Event: attractor.EngineEvent{
				Type:      attractor.EventAgentToolCallStart,
				NodeID:    "build",
				Timestamp: time.Now(),
				Data:      map[string]any{"tool_name": "read_file"},
			},
		}
		updated, _ = m.Update(toolEvt)
		m = updated.(StreamModel)
	}

	if m.totalToolCalls != 5 {
		t.Errorf("expected totalToolCalls=5, got %d", m.totalToolCalls)
	}
	if m.nodeToolCalls["build"] != 5 {
		t.Errorf("expected nodeToolCalls[build]=5, got %d", m.nodeToolCalls["build"])
	}
}

func TestStreamModelSummaryShownWhenDone(t *testing.T) {
	m := testStreamModel()
	m.done = true
	markCompleted(&m, 4)
	m.pipelineStart = time.Now().Add(-30 * time.Second)
	m.totalTokens = 12000
	m.totalToolCalls = 10

	// Mark all nodes completed
	for _, id := range m.nodeOrder {
		m.statuses[id] = NodeCompleted
		m.durations[id] = 5 * time.Second
	}

	view := m.View()

	if !strings.Contains(view, "Summary") {
		t.Errorf("done view should contain 'Summary' section, got:\n%s", view)
	}
	if !strings.Contains(view, "Nodes") {
		t.Errorf("done view should contain 'Nodes' line, got:\n%s", view)
	}
	if !strings.Contains(view, "Duration") {
		t.Errorf("done view should contain 'Duration' line, got:\n%s", view)
	}
}

func TestStreamModelSummaryShowsModelsUsed(t *testing.T) {
	m := testStreamModel()
	m.done = true
	markCompleted(&m, 4)
	m.pipelineStart = time.Now().Add(-30 * time.Second)

	// Set different models for different nodes
	m.nodeModels["build"] = "claude-sonnet-4-5-20250929"
	m.nodeModels["test"] = "claude-opus-4-6"

	// Mark nodes completed
	for _, id := range m.nodeOrder {
		m.statuses[id] = NodeCompleted
		m.durations[id] = 5 * time.Second
	}

	view := m.View()

	if !strings.Contains(view, "sonnet") {
		t.Errorf("summary should show 'sonnet' model, got:\n%s", view)
	}
	if !strings.Contains(view, "opus") {
		t.Errorf("summary should show 'opus' model, got:\n%s", view)
	}
}

func TestStreamModelSummaryShowsNodeCounts(t *testing.T) {
	m := testStreamModel()
	m.done = true
	markCompleted(&m, 3)
	m.pipelineStart = time.Now().Add(-30 * time.Second)

	// Mix of completed and failed
	m.statuses["start"] = NodeCompleted
	m.statuses["build"] = NodeCompleted
	m.statuses["test"] = NodeFailed
	m.statuses["done"] = NodeCompleted

	for _, id := range m.nodeOrder {
		m.durations[id] = 5 * time.Second
	}

	view := m.View()

	if !strings.Contains(view, "3 passed") {
		t.Errorf("summary should show '3 passed', got:\n%s", view)
	}
	if !strings.Contains(view, "1 failed") {
		t.Errorf("summary should show '1 failed', got:\n%s", view)
	}
}

func TestStreamModelSummaryShowsToolCalls(t *testing.T) {
	m := testStreamModel()
	m.done = true
	markCompleted(&m, 4)
	m.pipelineStart = time.Now().Add(-30 * time.Second)
	m.totalToolCalls = 42

	for _, id := range m.nodeOrder {
		m.statuses[id] = NodeCompleted
		m.durations[id] = 5 * time.Second
	}

	view := m.View()

	if !strings.Contains(view, "42 calls") {
		t.Errorf("summary should show '42 calls', got:\n%s", view)
	}
}

func TestStreamModelSummaryMinimalNoTokensNoTools(t *testing.T) {
	m := testStreamModel()
	m.done = true
	markCompleted(&m, 4)
	m.pipelineStart = time.Now().Add(-30 * time.Second)
	// No tokens, no tool calls, no models

	for _, id := range m.nodeOrder {
		m.statuses[id] = NodeCompleted
		m.durations[id] = 5 * time.Second
	}

	view := m.View()

	// Summary should appear
	if !strings.Contains(view, "Summary") {
		t.Errorf("minimal summary should still appear, got:\n%s", view)
	}
	// Tokens and Tools lines should be suppressed
	if strings.Contains(view, "Tokens") {
		t.Errorf("summary should not show Tokens line when totalTokens=0, got:\n%s", view)
	}
	if strings.Contains(view, "Tools") {
		t.Errorf("summary should not show Tools line when totalToolCalls=0, got:\n%s", view)
	}
	// Duration and Nodes should still appear
	if !strings.Contains(view, "Duration") {
		t.Errorf("summary should always show Duration, got:\n%s", view)
	}
	if !strings.Contains(view, "Nodes") {
		t.Errorf("summary should always show Nodes, got:\n%s", view)
	}
}

func TestStreamModelSummaryResumePreviousNodesExcluded(t *testing.T) {
	m := testStreamModelWithResume()
	// "start" is pre-completed from previous run (duration = -1)
	// Complete remaining nodes in this session
	for _, id := range []string{"build", "test", "done"} {
		m.statuses[id] = NodeCompleted
		m.durations[id] = 5 * time.Second
	}
	markCompleted(&m, 4) // 1 from resume + 3 from this run
	m.done = true
	m.pipelineStart = time.Now().Add(-30 * time.Second)

	view := m.View()

	// Summary should say 3 ran (not 4), since "start" was a previous run
	if !strings.Contains(view, "3 ran") {
		t.Errorf("summary should show '3 ran' (excluding resumed node), got:\n%s", view)
	}
	if !strings.Contains(view, "3 passed") {
		t.Errorf("summary should show '3 passed' (excluding resumed node), got:\n%s", view)
	}
}

func TestStreamModelSummaryNotShownWhileRunning(t *testing.T) {
	m := testStreamModel()
	m.done = false
	m.pipelineStart = time.Now().Add(-30 * time.Second)
	m.totalToolCalls = 10

	view := m.View()

	if strings.Contains(view, "Summary") {
		t.Errorf("summary should NOT appear while running, got:\n%s", view)
	}
}

func TestStreamModelNodeIndexByID(t *testing.T) {
	m := testStreamModel()

	// "start" should be first in topo order
	idx := m.nodeIndexByID("start")
	if idx < 0 {
		t.Fatal("expected start to be in nodeOrder")
	}

	// "done" should be last
	idxDone := m.nodeIndexByID("done")
	if idxDone < idx {
		t.Error("expected done after start in topo order")
	}

	// Non-existent node returns -1
	idxMissing := m.nodeIndexByID("nonexistent")
	if idxMissing != -1 {
		t.Errorf("expected -1 for missing node, got %d", idxMissing)
	}
}

func TestStreamModelHumanGatePassesContext(t *testing.T) {
	// Create a graph with a human gate node
	g := &attractor.Graph{
		Name: "gate_context_test",
		Nodes: map[string]*attractor.Node{
			"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "label": "Start"}},
			"deploy": {ID: "deploy", Attrs: map[string]string{"shape": "hexagon", "label": "Deploy"}},
			"done":   {ID: "done", Attrs: map[string]string{"shape": "Msquare", "label": "Done"}},
		},
		Edges: []*attractor.Edge{
			{From: "start", To: "deploy"},
			{From: "deploy", To: "done"},
		},
	}
	engine := attractor.NewEngine(attractor.EngineConfig{
		DefaultRetry: attractor.RetryPolicyNone(),
	})
	m := NewStreamModel(g, engine, "test.dot", context.Background(), false)

	// Simulate receiving a HumanGateRequestMsg with a NodeID
	msg := HumanGateRequestMsg{
		Question: "Approve deployment?",
		Options:  []string{"yes", "no"},
		NodeID:   "deploy",
	}

	updated, _ := m.Update(msg)
	m = updated.(StreamModel)

	if !m.humanGate.IsActive() {
		t.Fatal("expected human gate to be active")
	}

	// The view should show context (label + position)
	view := m.humanGate.View()
	if !strings.Contains(view, "Deploy") {
		t.Errorf("expected human gate view to show node label 'Deploy', got:\n%s", view)
	}
	if !strings.Contains(view, "step") {
		t.Errorf("expected human gate view to show step position, got:\n%s", view)
	}
}

func TestStreamModelSetResumeCmd(t *testing.T) {
	m := testStreamModel()
	called := false
	m.SetResumeCmd(func() tea.Cmd {
		called = true
		return nil
	})

	if m.resumeCmd == nil {
		t.Fatal("expected resumeCmd to be set")
	}

	// Call it to verify
	m.resumeCmd()
	if !called {
		t.Error("expected resumeCmd to be called")
	}
}
