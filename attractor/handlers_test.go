// ABOUTME: Tests for all 9 node handler types in the attractor pipeline runner.
// ABOUTME: Covers start, exit, codergen, conditional, parallel, fan-in, tool, manager, and human handlers.
package attractor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// --- Helper functions for building test fixtures ---

func newTestGraph() *Graph {
	return &Graph{
		Name:         "test",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
}

func addNode(g *Graph, id string, attrs map[string]string) *Node {
	n := &Node{ID: id, Attrs: attrs}
	g.Nodes[id] = n
	return n
}

func addEdge(g *Graph, from, to string, attrs map[string]string) *Edge {
	e := &Edge{From: from, To: to, Attrs: attrs}
	g.Edges = append(g.Edges, e)
	return e
}

// newContextWithGraph creates a pipeline context with the graph stored for handler access.
func newContextWithGraph(g *Graph) *Context {
	pctx := NewContext()
	pctx.Set("_graph", g)
	return pctx
}

// stubInterviewer is a test double for the Interviewer interface that returns a preset answer.
type stubInterviewer struct {
	answer string
	err    error
}

func (s *stubInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	return s.answer, s.err
}

// --- HandlerRegistry tests ---

func TestNewHandlerRegistry(t *testing.T) {
	reg := NewHandlerRegistry()
	if reg == nil {
		t.Fatal("NewHandlerRegistry returned nil")
	}
}

func TestHandlerRegistryRegisterAndGet(t *testing.T) {
	reg := NewHandlerRegistry()
	handler := &StartHandler{}
	reg.Register(handler)

	got := reg.Get("start")
	if got == nil {
		t.Fatal("expected handler for 'start', got nil")
	}
	if got.Type() != "start" {
		t.Errorf("expected type 'start', got %q", got.Type())
	}
}

func TestHandlerRegistryGetMissing(t *testing.T) {
	reg := NewHandlerRegistry()
	got := reg.Get("nonexistent")
	if got != nil {
		t.Errorf("expected nil for missing handler, got %v", got)
	}
}

func TestDefaultHandlerRegistryHasAllHandlers(t *testing.T) {
	reg := DefaultHandlerRegistry()

	expectedTypes := []string{
		"start",
		"exit",
		"codergen",
		"conditional",
		"parallel",
		"parallel.fan_in",
		"tool",
		"stack.manager_loop",
		"wait.human",
	}

	for _, typeName := range expectedTypes {
		h := reg.Get(typeName)
		if h == nil {
			t.Errorf("DefaultHandlerRegistry missing handler for type %q", typeName)
			continue
		}
		if h.Type() != typeName {
			t.Errorf("handler for %q returned Type() = %q", typeName, h.Type())
		}
	}
}

func TestHandlerRegistryRegisterOverwrites(t *testing.T) {
	reg := NewHandlerRegistry()
	h1 := &StartHandler{}
	h2 := &StartHandler{}
	reg.Register(h1)
	reg.Register(h2)
	got := reg.Get("start")
	if got != h2 {
		t.Error("expected second registered handler to overwrite first")
	}
}

// --- Start handler tests ---

func TestStartHandlerType(t *testing.T) {
	h := &StartHandler{}
	if h.Type() != "start" {
		t.Errorf("expected type 'start', got %q", h.Type())
	}
}

func TestStartHandlerExecute(t *testing.T) {
	h := &StartHandler{}
	g := newTestGraph()
	node := addNode(g, "start", map[string]string{"shape": "Mdiamond"})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}

	// Should set _started_at in context updates
	startedAt, ok := outcome.ContextUpdates["_started_at"]
	if !ok {
		t.Error("expected _started_at in context updates")
	}
	if _, ok := startedAt.(string); !ok {
		t.Errorf("expected _started_at to be a string, got %T", startedAt)
	}
}

// --- Exit handler tests ---

func TestExitHandlerType(t *testing.T) {
	h := &ExitHandler{}
	if h.Type() != "exit" {
		t.Errorf("expected type 'exit', got %q", h.Type())
	}
}

func TestExitHandlerExecute(t *testing.T) {
	h := &ExitHandler{}
	g := newTestGraph()
	node := addNode(g, "exit", map[string]string{"shape": "Msquare"})
	pctx := NewContext()
	pctx.Set("some_key", "some_value")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	// Should note the exit
	if outcome.Notes == "" {
		t.Error("expected non-empty notes on exit")
	}
}

func TestExitHandlerCapturesFinishedAt(t *testing.T) {
	h := &ExitHandler{}
	g := newTestGraph()
	node := addNode(g, "exit", map[string]string{"shape": "Msquare"})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	finishedAt, ok := outcome.ContextUpdates["_finished_at"]
	if !ok {
		t.Error("expected _finished_at in context updates")
	}
	if _, ok := finishedAt.(string); !ok {
		t.Errorf("expected _finished_at to be a string, got %T", finishedAt)
	}
}

// --- Codergen handler tests ---

func TestCodergenHandlerType(t *testing.T) {
	h := &CodergenHandler{}
	if h.Type() != "codergen" {
		t.Errorf("expected type 'codergen', got %q", h.Type())
	}
}

func TestCodergenHandlerExecuteStub(t *testing.T) {
	h := &CodergenHandler{Backend: &fakeBackend{}}
	g := newTestGraph()
	node := addNode(g, "codegen1", map[string]string{
		"shape":        "box",
		"prompt":       "Write a function that adds two numbers",
		"label":        "Add Function",
		"llm_model":    "claude-opus-4-20250514",
		"llm_provider": "anthropic",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	// Outcome should record the prompt and config
	if outcome.ContextUpdates["last_stage"] != "codegen1" {
		t.Errorf("expected last_stage = codegen1, got %v", outcome.ContextUpdates["last_stage"])
	}
}

func TestCodergenHandlerUsesLabelAsFallbackPrompt(t *testing.T) {
	backend := &fakeBackend{}
	h := &CodergenHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "codegen2", map[string]string{
		"shape": "box",
		"label": "My Label Prompt",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	// Verify the label was used as the prompt (no explicit prompt attr)
	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", len(backend.calls))
	}
	if backend.calls[0].Prompt != "My Label Prompt" {
		t.Errorf("expected prompt to fall back to label, got %q", backend.calls[0].Prompt)
	}
}

func TestCodergenHandlerGoalGateAttribute(t *testing.T) {
	h := &CodergenHandler{Backend: &fakeBackend{}}
	g := newTestGraph()
	node := addNode(g, "codegen3", map[string]string{
		"shape":     "box",
		"prompt":    "Test something",
		"goal_gate": "true",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
}

func TestCodergenHandlerMaxRetriesAttribute(t *testing.T) {
	h := &CodergenHandler{Backend: &fakeBackend{}}
	g := newTestGraph()
	node := addNode(g, "codegen4", map[string]string{
		"shape":       "box",
		"prompt":      "Do work",
		"max_retries": "3",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// max_retries is recorded for engine use
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
}

// --- Conditional handler tests ---

func TestConditionalHandlerType(t *testing.T) {
	h := &ConditionalHandler{}
	if h.Type() != "conditional" {
		t.Errorf("expected type 'conditional', got %q", h.Type())
	}
}

func TestConditionalHandlerSelectsMatchingEdge(t *testing.T) {
	h := &ConditionalHandler{}
	g := newTestGraph()
	node := addNode(g, "gate", map[string]string{"shape": "diamond"})
	addNode(g, "yes_path", map[string]string{})
	addNode(g, "no_path", map[string]string{})
	addEdge(g, "gate", "yes_path", map[string]string{"label": "Yes", "condition": "context.ready = true"})
	addEdge(g, "gate", "no_path", map[string]string{"label": "No", "condition": "context.ready = false"})

	pctx := NewContext()
	pctx.Set("ready", "true")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	if outcome.Notes == "" {
		t.Error("expected notes to describe the conditional evaluation")
	}
}

func TestConditionalHandlerWithOutgoingEdges(t *testing.T) {
	h := &ConditionalHandler{}
	g := newTestGraph()
	node := addNode(g, "branch", map[string]string{"shape": "diamond"})
	addNode(g, "path_a", map[string]string{})
	addNode(g, "path_b", map[string]string{})
	addEdge(g, "branch", "path_a", map[string]string{"label": "A", "condition": "outcome = success"})
	addEdge(g, "branch", "path_b", map[string]string{"label": "B", "condition": "outcome = fail"})

	pctx := NewContext()
	pctx.Set("outcome", "success")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
}

// --- Parallel handler tests ---

func TestParallelHandlerType(t *testing.T) {
	h := &ParallelHandler{}
	if h.Type() != "parallel" {
		t.Errorf("expected type 'parallel', got %q", h.Type())
	}
}

func TestParallelHandlerListsBranches(t *testing.T) {
	h := &ParallelHandler{}
	g := newTestGraph()
	node := addNode(g, "fanout", map[string]string{"shape": "component"})
	addNode(g, "branch1", map[string]string{})
	addNode(g, "branch2", map[string]string{})
	addNode(g, "branch3", map[string]string{})
	addEdge(g, "fanout", "branch1", map[string]string{"label": "b1"})
	addEdge(g, "fanout", "branch2", map[string]string{"label": "b2"})
	addEdge(g, "fanout", "branch3", map[string]string{"label": "b3"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	// Should record spawned branches
	branches, ok := outcome.ContextUpdates["parallel.branches"]
	if !ok {
		t.Error("expected parallel.branches in context updates")
	}
	branchList, ok := branches.([]string)
	if !ok {
		t.Fatalf("expected []string for parallel.branches, got %T", branches)
	}
	if len(branchList) != 3 {
		t.Errorf("expected 3 branches, got %d", len(branchList))
	}
}

func TestParallelHandlerNoBranches(t *testing.T) {
	h := &ParallelHandler{}
	g := newTestGraph()
	node := addNode(g, "fanout", map[string]string{"shape": "component"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail for no branches, got %v", outcome.Status)
	}
}

// --- Fan-in handler tests ---

func TestFanInHandlerType(t *testing.T) {
	h := &FanInHandler{}
	if h.Type() != "parallel.fan_in" {
		t.Errorf("expected type 'parallel.fan_in', got %q", h.Type())
	}
}

func TestFanInHandlerWithResults(t *testing.T) {
	h := &FanInHandler{}
	g := newTestGraph()
	node := addNode(g, "fanin", map[string]string{"shape": "tripleoctagon"})

	pctx := NewContext()
	pctx.Set("parallel.results", "branch1:success,branch2:success")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
}

func TestFanInHandlerNoResults(t *testing.T) {
	h := &FanInHandler{}
	g := newTestGraph()
	node := addNode(g, "fanin", map[string]string{"shape": "tripleoctagon"})

	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail when no parallel results, got %v", outcome.Status)
	}
	if outcome.FailureReason == "" {
		t.Error("expected failure reason for missing results")
	}
}

// --- Tool handler tests ---

func TestToolHandlerType(t *testing.T) {
	h := &ToolHandler{}
	if h.Type() != "tool" {
		t.Errorf("expected type 'tool', got %q", h.Type())
	}
}

func TestToolHandlerRecordsCommand(t *testing.T) {
	h := &ToolHandler{}
	g := newTestGraph()
	node := addNode(g, "run_tool", map[string]string{
		"shape":   "parallelogram",
		"command": "echo hello",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	stdout, ok := outcome.ContextUpdates["tool.stdout"].(string)
	if !ok {
		t.Fatalf("expected tool.stdout to be a string, got %T", outcome.ContextUpdates["tool.stdout"])
	}
	if !strings.Contains(stdout, "hello") {
		t.Errorf("expected 'hello' in tool.stdout, got %q", stdout)
	}
}

func TestToolHandlerNoCommand(t *testing.T) {
	h := &ToolHandler{}
	g := newTestGraph()
	node := addNode(g, "run_tool", map[string]string{
		"shape": "parallelogram",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail for missing command, got %v", outcome.Status)
	}
}

func TestToolHandlerUsesPromptFallbackInRegistry(t *testing.T) {
	h := &ToolHandler{}
	g := newTestGraph()
	node := addNode(g, "run_tool", map[string]string{
		"shape":  "parallelogram",
		"prompt": "echo from_prompt",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	stdout, ok := outcome.ContextUpdates["tool.stdout"].(string)
	if !ok {
		t.Fatalf("expected tool.stdout to be a string, got %T", outcome.ContextUpdates["tool.stdout"])
	}
	if !strings.Contains(stdout, "from_prompt") {
		t.Errorf("expected 'from_prompt' in tool.stdout, got %q", stdout)
	}
}

// --- Manager loop handler tests ---

func TestManagerLoopHandlerType(t *testing.T) {
	h := &ManagerLoopHandler{}
	if h.Type() != "stack.manager_loop" {
		t.Errorf("expected type 'stack.manager_loop', got %q", h.Type())
	}
}

func TestManagerLoopHandlerRecordsConfig(t *testing.T) {
	h := &ManagerLoopHandler{}
	g := newTestGraph()
	g.Attrs["stack.child_dotfile"] = "child.dot"
	node := addNode(g, "manager", map[string]string{
		"shape":                  "house",
		"manager.poll_interval":  "30s",
		"manager.max_cycles":     "100",
		"manager.stop_condition": "context.done = true",
		"manager.actions":        "observe,steer,wait",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	if outcome.ContextUpdates["manager.child_dotfile"] != "child.dot" {
		t.Errorf("expected manager.child_dotfile = 'child.dot', got %v", outcome.ContextUpdates["manager.child_dotfile"])
	}
	if outcome.ContextUpdates["manager.max_cycles"] != "100" {
		t.Errorf("expected manager.max_cycles = '100', got %v", outcome.ContextUpdates["manager.max_cycles"])
	}
}

func TestManagerLoopHandlerDefaultConfig(t *testing.T) {
	h := &ManagerLoopHandler{}
	g := newTestGraph()
	node := addNode(g, "manager", map[string]string{
		"shape": "house",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
}

// --- Wait for human handler tests ---

func TestWaitForHumanHandlerType(t *testing.T) {
	h := &WaitForHumanHandler{}
	if h.Type() != "wait.human" {
		t.Errorf("expected type 'wait.human', got %q", h.Type())
	}
}

func TestWaitForHumanHandlerWithInterviewer(t *testing.T) {
	interviewer := &stubInterviewer{answer: "yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}
	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape": "hexagon",
		"label": "Do you approve?",
	})
	addNode(g, "approve", map[string]string{})
	addNode(g, "reject", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})
	addEdge(g, "human_gate", "reject", map[string]string{"label": "[N] No"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	if outcome.ContextUpdates["human.gate.selected"] == "" {
		t.Error("expected human.gate.selected in context updates")
	}
}

func TestWaitForHumanHandlerNoInterviewerUsesNodeAttrs(t *testing.T) {
	h := &WaitForHumanHandler{}
	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":    "hexagon",
		"label":    "Pick one",
		"question": "What do you want?",
		"options":  "A,B,C",
	})
	addNode(g, "path_a", map[string]string{})
	addEdge(g, "human_gate", "path_a", map[string]string{"label": "A"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Without an interviewer, should return a pending/fail status
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail without interviewer, got %v", outcome.Status)
	}
}

func TestWaitForHumanHandlerInterviewerError(t *testing.T) {
	interviewer := &stubInterviewer{answer: "", err: fmt.Errorf("human disconnected")}
	h := &WaitForHumanHandler{Interviewer: interviewer}
	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape": "hexagon",
		"label": "Approve?",
	})
	addNode(g, "yes", map[string]string{})
	addEdge(g, "human_gate", "yes", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail on interviewer error, got %v", outcome.Status)
	}
	if outcome.FailureReason == "" {
		t.Error("expected failure reason")
	}
}

func TestWaitForHumanHandlerNoEdges(t *testing.T) {
	interviewer := &stubInterviewer{answer: "yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}
	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape": "hexagon",
		"label": "Approve?",
	})
	// No outgoing edges

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail for no outgoing edges, got %v", outcome.Status)
	}
}

// --- Context cancellation tests ---

func TestStartHandlerRespectsContextCancellation(t *testing.T) {
	h := &StartHandler{}
	g := newTestGraph()
	node := addNode(g, "start", map[string]string{"shape": "Mdiamond"})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := h.Execute(ctx, node, pctx, store)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestCodergenHandlerRespectsContextCancellation(t *testing.T) {
	h := &CodergenHandler{}
	g := newTestGraph()
	node := addNode(g, "codegen", map[string]string{
		"shape":  "box",
		"prompt": "Do work",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := h.Execute(ctx, node, pctx, store)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

// --- Resolve handler from shape tests ---

func TestResolveHandlerFromShape(t *testing.T) {
	reg := DefaultHandlerRegistry()

	tests := []struct {
		shape    string
		wantType string
	}{
		{"Mdiamond", "start"},
		{"Msquare", "exit"},
		{"box", "codergen"},
		{"diamond", "conditional"},
		{"component", "parallel"},
		{"tripleoctagon", "parallel.fan_in"},
		{"parallelogram", "tool"},
		{"house", "stack.manager_loop"},
		{"hexagon", "wait.human"},
	}

	for _, tt := range tests {
		handlerType := ShapeToHandlerType(tt.shape)
		h := reg.Get(handlerType)
		if h == nil {
			t.Errorf("no handler for shape %q (type %q)", tt.shape, handlerType)
			continue
		}
		if h.Type() != tt.wantType {
			t.Errorf("shape %q: expected handler type %q, got %q", tt.shape, tt.wantType, h.Type())
		}
	}
}

func TestShapeToHandlerTypeUnknownShape(t *testing.T) {
	result := ShapeToHandlerType("unknown_shape")
	if result != "codergen" {
		t.Errorf("expected codergen for unknown shape, got %q", result)
	}
}

// --- Codergen handler with graph context ---

func TestCodergenHandlerRecordsLLMConfig(t *testing.T) {
	h := &CodergenHandler{Backend: &fakeBackend{}}
	g := newTestGraph()
	node := addNode(g, "codegen_cfg", map[string]string{
		"shape":        "box",
		"prompt":       "Generate code",
		"llm_model":    "gpt-4",
		"llm_provider": "openai",
	})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.ContextUpdates["codergen.model"] != "gpt-4" {
		t.Errorf("expected codergen.model = gpt-4, got %v", outcome.ContextUpdates["codergen.model"])
	}
	if outcome.ContextUpdates["codergen.provider"] != "openai" {
		t.Errorf("expected codergen.provider = openai, got %v", outcome.ContextUpdates["codergen.provider"])
	}
}

// --- Parallel handler records attributes ---

func TestParallelHandlerRecordsJoinPolicy(t *testing.T) {
	h := &ParallelHandler{}
	g := newTestGraph()
	node := addNode(g, "fanout", map[string]string{
		"shape":        "component",
		"join_policy":  "first_success",
		"error_policy": "fail_fast",
		"max_parallel": "8",
	})
	addNode(g, "b1", map[string]string{})
	addEdge(g, "fanout", "b1", map[string]string{})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected success, got %v", outcome.Status)
	}
	if outcome.ContextUpdates["parallel.join_policy"] != "first_success" {
		t.Errorf("expected join_policy = first_success, got %v", outcome.ContextUpdates["parallel.join_policy"])
	}
}

// --- Test handler receives the graph ---

func TestConditionalHandlerReceivesGraph(t *testing.T) {
	h := &ConditionalHandler{}
	g := newTestGraph()
	g.Attrs["goal"] = "test goal"
	node := addNode(g, "cond", map[string]string{"shape": "diamond"})
	addNode(g, "target", map[string]string{})
	addEdge(g, "cond", "target", map[string]string{"condition": "context.x = y"})

	pctx := NewContext()
	pctx.Set("x", "y")
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected success, got %v", outcome.Status)
	}
}

// --- Start handler timing ---

func TestStartHandlerStartedAtIsValidTimestamp(t *testing.T) {
	h := &StartHandler{}
	g := newTestGraph()
	node := addNode(g, "start", map[string]string{"shape": "Mdiamond"})
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ts, ok := outcome.ContextUpdates["_started_at"].(string)
	if !ok {
		t.Fatal("_started_at is not a string")
	}
	_, parseErr := time.Parse(time.RFC3339Nano, ts)
	if parseErr != nil {
		t.Errorf("_started_at is not a valid RFC3339Nano timestamp: %v", parseErr)
	}
}
