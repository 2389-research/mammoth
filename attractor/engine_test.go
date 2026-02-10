// ABOUTME: Tests for the pipeline execution engine covering the full 5-phase lifecycle.
// ABOUTME: Covers linear pipelines, branching, goal gates, retries, checkpoints, context cancellation, and edge cases.
package attractor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Test handler implementation ---

// testHandler is a configurable NodeHandler for testing that returns preset outcomes.
type testHandler struct {
	typeName   string
	executeFn  func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error)
	callCount  int
	calledWith []*Node
}

func (h *testHandler) Type() string { return h.typeName }

func (h *testHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	h.callCount++
	h.calledWith = append(h.calledWith, node)
	if h.executeFn != nil {
		return h.executeFn(ctx, node, pctx, store)
	}
	return &Outcome{Status: StatusSuccess}, nil
}

// newSuccessHandler returns a testHandler that always succeeds.
func newSuccessHandler(typeName string) *testHandler {
	return &testHandler{
		typeName: typeName,
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
}

// newFailHandler returns a testHandler that always fails.
func newFailHandler(typeName string) *testHandler {
	return &testHandler{
		typeName: typeName,
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return &Outcome{Status: StatusFail, FailureReason: "test failure"}, nil
		},
	}
}

// newErrorHandler returns a testHandler that always returns an error.
func newErrorHandler(typeName string) *testHandler {
	return &testHandler{
		typeName: typeName,
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return nil, fmt.Errorf("test execution error")
		},
	}
}

// newContextUpdateHandler returns a handler that sets context updates.
func newContextUpdateHandler(typeName string, updates map[string]any) *testHandler {
	return &testHandler{
		typeName: typeName,
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return &Outcome{
				Status:         StatusSuccess,
				ContextUpdates: updates,
			}, nil
		},
	}
}

// buildTestRegistry creates a registry with handlers for testing.
func buildTestRegistry(handlers ...*testHandler) *HandlerRegistry {
	reg := NewHandlerRegistry()
	for _, h := range handlers {
		reg.Register(h)
	}
	return reg
}

// buildLinearGraph creates: start -> a -> b -> exit
func buildLinearGraph() *Graph {
	g := &Graph{
		Name:         "linear",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["a"] = &Node{ID: "a", Attrs: map[string]string{"shape": "box", "label": "Step A"}}
	g.Nodes["b"] = &Node{ID: "b", Attrs: map[string]string{"shape": "box", "label": "Step B"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "a", Attrs: map[string]string{}},
		&Edge{From: "a", To: "b", Attrs: map[string]string{}},
		&Edge{From: "b", To: "exit", Attrs: map[string]string{}},
	)
	return g
}

// --- Engine tests ---

func TestEngineRunGraphLinearPipeline(t *testing.T) {
	g := buildLinearGraph()

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// All four nodes should be completed
	if len(result.CompletedNodes) != 4 {
		t.Errorf("expected 4 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}

	// Start handler called once
	if startH.callCount != 1 {
		t.Errorf("expected start handler called 1 time, got %d", startH.callCount)
	}

	// Codergen handler called for nodes "a" and "b"
	if codergenH.callCount != 2 {
		t.Errorf("expected codergen handler called 2 times, got %d", codergenH.callCount)
	}

	// Exit handler called once
	if exitH.callCount != 1 {
		t.Errorf("expected exit handler called 1 time, got %d", exitH.callCount)
	}
}

func TestEngineRunGraphConditionalBranching(t *testing.T) {
	g := &Graph{
		Name:         "conditional",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["check"] = &Node{ID: "check", Attrs: map[string]string{"shape": "box", "label": "Check"}}
	g.Nodes["yes_path"] = &Node{ID: "yes_path", Attrs: map[string]string{"shape": "box", "label": "Yes Path"}}
	g.Nodes["no_path"] = &Node{ID: "no_path", Attrs: map[string]string{"shape": "box", "label": "No Path"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "check", Attrs: map[string]string{}},
		&Edge{From: "check", To: "yes_path", Attrs: map[string]string{"condition": "outcome = success"}},
		&Edge{From: "check", To: "no_path", Attrs: map[string]string{"condition": "outcome = fail"}},
		&Edge{From: "yes_path", To: "exit", Attrs: map[string]string{}},
		&Edge{From: "no_path", To: "exit", Attrs: map[string]string{}},
	)

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should follow: start -> check -> yes_path (condition "outcome = success") -> exit
	foundYes := false
	foundNo := false
	for _, n := range result.CompletedNodes {
		if n == "yes_path" {
			foundYes = true
		}
		if n == "no_path" {
			foundNo = true
		}
	}
	if !foundYes {
		t.Error("expected yes_path in completed nodes")
	}
	if foundNo {
		t.Error("did not expect no_path in completed nodes (condition should not match)")
	}
}

func TestEngineRunGraphGoalGateEnforcementWithRetryTarget(t *testing.T) {
	g := &Graph{
		Name:         "goal_gate",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["gate"] = &Node{ID: "gate", Attrs: map[string]string{
		"shape":        "box",
		"label":        "Gate",
		"goal_gate":    "true",
		"retry_target": "gate",
	}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "gate", Attrs: map[string]string{}},
		&Edge{From: "gate", To: "exit", Attrs: map[string]string{}},
	)

	callCount := 0
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			callCount++
			// Fail first two times, succeed third
			if callCount <= 2 {
				return &Outcome{Status: StatusFail, FailureReason: "not yet"}, nil
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Gate should have been retried via goal gate mechanism
	if callCount < 3 {
		t.Errorf("expected gate to be called at least 3 times, got %d", callCount)
	}
	if result.FinalOutcome == nil {
		t.Fatal("expected non-nil final outcome")
	}
}

func TestEngineRunGraphGoalGateFailureNoRetryTarget(t *testing.T) {
	g := &Graph{
		Name:         "goal_gate_fail",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["gate"] = &Node{ID: "gate", Attrs: map[string]string{
		"shape":     "box",
		"label":     "Gate",
		"goal_gate": "true",
	}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "gate", Attrs: map[string]string{}},
		&Edge{From: "gate", To: "exit", Attrs: map[string]string{}},
	)

	startH := newSuccessHandler("start")
	codergenH := newFailHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err == nil {
		t.Fatal("expected error for goal gate failure with no retry target")
	}
	if !strings.Contains(err.Error(), "goal gate") {
		t.Errorf("expected error about goal gate, got: %v", err)
	}
}

func TestEngineRunGraphRetryLogic(t *testing.T) {
	g := &Graph{
		Name:         "retry",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["flaky"] = &Node{ID: "flaky", Attrs: map[string]string{
		"shape":       "box",
		"label":       "Flaky",
		"max_retries": "3",
	}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "flaky", Attrs: map[string]string{}},
		&Edge{From: "flaky", To: "exit", Attrs: map[string]string{}},
	)

	callCount := 0
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			callCount++
			if callCount < 3 {
				return &Outcome{Status: StatusRetry}, nil
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Flaky node should have been called 3 times (2 retries + final success)
	if callCount != 3 {
		t.Errorf("expected 3 calls to flaky handler, got %d", callCount)
	}
	if result.NodeOutcomes["flaky"].Status != StatusSuccess {
		t.Errorf("expected flaky to succeed, got %v", result.NodeOutcomes["flaky"].Status)
	}
}

func TestEngineRunGraphRetryExhaustion(t *testing.T) {
	g := &Graph{
		Name:         "retry_exhaust",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["always_retry"] = &Node{ID: "always_retry", Attrs: map[string]string{
		"shape":       "box",
		"label":       "Always Retry",
		"max_retries": "2",
	}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "always_retry", Attrs: map[string]string{}},
		&Edge{From: "always_retry", To: "exit", Attrs: map[string]string{}},
	)

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return &Outcome{Status: StatusRetry}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After exhausting retries, the node should report fail
	outcome := result.NodeOutcomes["always_retry"]
	if outcome == nil {
		t.Fatal("expected outcome for always_retry")
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected fail after retry exhaustion, got %v", outcome.Status)
	}
}

func TestEngineRunGraphContextUpdatesPropagated(t *testing.T) {
	g := buildLinearGraph()

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "a" {
				return &Outcome{
					Status:         StatusSuccess,
					ContextUpdates: map[string]any{"from_a": "hello"},
				}, nil
			}
			// Node b should see from_a in context
			val := pctx.GetString("from_a", "")
			return &Outcome{
				Status:         StatusSuccess,
				ContextUpdates: map[string]any{"b_saw": val},
			}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Context should contain the propagated value
	val := result.Context.GetString("b_saw", "")
	if val != "hello" {
		t.Errorf("expected context 'b_saw'='hello', got %q", val)
	}
}

func TestEngineRunGraphCheckpointSaving(t *testing.T) {
	g := buildLinearGraph()
	cpDir := t.TempDir()

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:      reg,
		CheckpointDir: cpDir,
		DefaultRetry:  RetryPolicyNone(),
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that at least one checkpoint file was written
	entries, err := os.ReadDir(cpDir)
	if err != nil {
		t.Fatalf("error reading checkpoint dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one checkpoint file in checkpoint dir")
	}

	// Verify we can load a checkpoint
	for _, entry := range entries {
		cp, err := LoadCheckpoint(filepath.Join(cpDir, entry.Name()))
		if err != nil {
			t.Errorf("failed to load checkpoint %q: %v", entry.Name(), err)
			continue
		}
		if cp.CurrentNode == "" {
			t.Error("checkpoint has empty current node")
		}
	}
}

func TestEngineRunGraphNoStartNode(t *testing.T) {
	g := &Graph{
		Name:         "no_start",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["a"] = &Node{ID: "a", Attrs: map[string]string{"shape": "box"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "a", To: "exit", Attrs: map[string]string{}},
	)

	engine := NewEngine(EngineConfig{
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err == nil {
		t.Fatal("expected error for graph with no start node")
	}
}

func TestEngineRunGraphValidationFailure(t *testing.T) {
	g := &Graph{
		Name:         "invalid",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	// Graph with edge referencing nonexistent node
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "nonexistent", Attrs: map[string]string{}},
		&Edge{From: "start", To: "exit", Attrs: map[string]string{}},
	)

	engine := NewEngine(EngineConfig{
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err == nil {
		t.Fatal("expected error for invalid graph")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestEngineRunGraphContextCancellation(t *testing.T) {
	g := buildLinearGraph()

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
				// Simulate some work
				time.Sleep(10 * time.Millisecond)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
					return &Outcome{Status: StatusSuccess}, nil
				}
			}
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel right away
	cancel()

	_, err := engine.RunGraph(ctx, g)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestEngineRunGraphFailureRouting(t *testing.T) {
	g := &Graph{
		Name:         "fail_routing",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["will_fail"] = &Node{ID: "will_fail", Attrs: map[string]string{"shape": "box", "label": "Will Fail"}}
	g.Nodes["error_handler"] = &Node{ID: "error_handler", Attrs: map[string]string{"shape": "box", "label": "Error Handler"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "will_fail", Attrs: map[string]string{}},
		&Edge{From: "will_fail", To: "error_handler", Attrs: map[string]string{"condition": "outcome = fail"}},
		&Edge{From: "will_fail", To: "exit", Attrs: map[string]string{"condition": "outcome = success"}},
		&Edge{From: "error_handler", To: "exit", Attrs: map[string]string{}},
	)

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "will_fail" {
				return &Outcome{Status: StatusFail, FailureReason: "intentional"}, nil
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have followed: start -> will_fail -> error_handler -> exit
	foundErrorHandler := false
	for _, n := range result.CompletedNodes {
		if n == "error_handler" {
			foundErrorHandler = true
		}
	}
	if !foundErrorHandler {
		t.Errorf("expected error_handler in completed nodes, got: %v", result.CompletedNodes)
	}
}

func TestEngineRunGraphEmptyConditionTreatedAsUnconditional(t *testing.T) {
	g := &Graph{
		Name:         "empty_cond",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["a"] = &Node{ID: "a", Attrs: map[string]string{"shape": "box", "label": "A"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "a", Attrs: map[string]string{"condition": ""}},
		&Edge{From: "a", To: "exit", Attrs: map[string]string{"condition": ""}},
	)

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.CompletedNodes) != 3 {
		t.Errorf("expected 3 completed nodes (start, a, exit), got %d: %v",
			len(result.CompletedNodes), result.CompletedNodes)
	}
}

func TestEngineRunFromDOTSource(t *testing.T) {
	source := `digraph test {
		start [shape=Mdiamond]
		middle [shape=box, label="Middle"]
		done [shape=Msquare]
		start -> middle
		middle -> done
	}`

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.CompletedNodes) != 3 {
		t.Errorf("expected 3 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}
}

func TestEngineRunWithEvents(t *testing.T) {
	g := buildLinearGraph()

	var events []EngineEvent
	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
		EventHandler: func(evt EngineEvent) {
			events = append(events, evt)
		},
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have pipeline started, multiple stage events, and pipeline completed
	if len(events) == 0 {
		t.Fatal("expected at least some events")
	}

	// First event should be pipeline started
	if events[0].Type != EventPipelineStarted {
		t.Errorf("expected first event to be pipeline.started, got %v", events[0].Type)
	}

	// Last event should be pipeline completed
	if events[len(events)-1].Type != EventPipelineCompleted {
		t.Errorf("expected last event to be pipeline.completed, got %v", events[len(events)-1].Type)
	}
}

func TestEngineRunGraphGraphAttrsInContext(t *testing.T) {
	g := buildLinearGraph()
	g.Attrs["goal"] = "build something"
	g.Attrs["version"] = "1.0"

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			goal := pctx.GetString("goal", "")
			if goal != "build something" {
				return &Outcome{Status: StatusFail, FailureReason: fmt.Sprintf("expected goal='build something', got %q", goal)}, nil
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify graph attrs are in context
	if result.Context.GetString("goal", "") != "build something" {
		t.Error("expected graph attr 'goal' to be mirrored into context")
	}
}

func TestEngineRunGraphStageFailNoOutgoingEdge(t *testing.T) {
	g := &Graph{
		Name:         "dead_end",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["dead_end"] = &Node{ID: "dead_end", Attrs: map[string]string{"shape": "box", "label": "Dead End"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "dead_end", Attrs: map[string]string{}},
		&Edge{From: "start", To: "exit", Attrs: map[string]string{}},
	)
	// dead_end has no outgoing edge, and will fail

	startH := newSuccessHandler("start")
	codergenH := newFailHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err == nil {
		t.Fatal("expected error when stage fails with no outgoing fail edge")
	}
	if !strings.Contains(err.Error(), "no outgoing") {
		t.Errorf("expected 'no outgoing' in error, got: %v", err)
	}
}

func TestEngineRunGraphRetryWithErrorFromHandler(t *testing.T) {
	g := &Graph{
		Name:         "error_retry",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["errnode"] = &Node{ID: "errnode", Attrs: map[string]string{
		"shape":       "box",
		"label":       "Error Node",
		"max_retries": "2",
	}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "errnode", Attrs: map[string]string{}},
		&Edge{From: "errnode", To: "exit", Attrs: map[string]string{}},
	)

	callCount := 0
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			callCount++
			if callCount < 3 {
				return nil, fmt.Errorf("transient error")
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (2 errors + 1 success), got %d", callCount)
	}
	if result.NodeOutcomes["errnode"].Status != StatusSuccess {
		t.Errorf("expected success after retries, got %v", result.NodeOutcomes["errnode"].Status)
	}
}

func TestUnwrapHandler(t *testing.T) {
	// Layer 0: bare handler (no wrappers)
	bare := &CodergenHandler{}
	unwrapped := unwrapHandler(bare)
	if unwrapped != bare {
		t.Errorf("unwrapping a bare handler should return the same handler")
	}

	// Layer 1: one wrapper
	wrapped1 := &interviewerInjectingHandler{inner: bare}
	unwrapped = unwrapHandler(wrapped1)
	if unwrapped != bare {
		t.Errorf("unwrapping 1 layer should return the bare handler, got %T", unwrapped)
	}

	// Layer 2: two wrappers
	wrapped2 := &interviewerInjectingHandler{inner: wrapped1}
	unwrapped = unwrapHandler(wrapped2)
	if unwrapped != bare {
		t.Errorf("unwrapping 2 layers should return the bare handler, got %T", unwrapped)
	}
}

func TestBackendWiringThroughWrappedHandler(t *testing.T) {
	// Create a CodergenHandler without a backend
	codergenH := &CodergenHandler{}

	// Create a registry and register it
	reg := NewHandlerRegistry()
	reg.Register(codergenH)
	reg.Register(newSuccessHandler("start"))
	reg.Register(newSuccessHandler("exit"))

	// Wrap the registry with an interviewer (this is what server mode does)
	interviewer := &httpInterviewer{}
	wrappedReg := wrapRegistryWithInterviewer(reg, interviewer)

	// Create a backend that records whether RunAgent was called
	backendCalled := false
	backend := &stubCodergenBackend{
		runFn: func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
			backendCalled = true
			return &AgentRunResult{
				Output:     "generated code",
				ToolCalls:  3,
				TokensUsed: 500,
				Success:    true,
			}, nil
		},
	}

	// Create an engine with the wrapped registry and the backend
	engine := NewEngine(EngineConfig{
		Handlers:     wrappedReg,
		Backend:      backend,
		DefaultRetry: RetryPolicyNone(),
	})

	// Build a simple pipeline: start -> code_task -> exit
	g := &Graph{
		Name:         "backend_wiring",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["code_task"] = &Node{ID: "code_task", Attrs: map[string]string{
		"shape": "box",
		"label": "Generate Code",
	}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "code_task", Attrs: map[string]string{}},
		&Edge{From: "code_task", To: "exit", Attrs: map[string]string{}},
	)

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The backend should have been called (not stub mode)
	if !backendCalled {
		t.Error("expected backend RunAgent to be called, but it was not (handler stuck in stub mode)")
	}

	// Verify node completed successfully
	if outcome, ok := result.NodeOutcomes["code_task"]; ok {
		if outcome.Status != StatusSuccess {
			t.Errorf("expected code_task to succeed, got %v", outcome.Status)
		}
	} else {
		t.Error("expected code_task in node outcomes")
	}
}

func TestRunGraphCreatesSharedWorkDir(t *testing.T) {
	g := buildLinearGraph()

	// Track what _workdir was set to in context during execution
	var observedWorkDir string
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if val := pctx.Get("_workdir"); val != nil {
				observedWorkDir = val.(string)
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	// Engine with NO ArtifactDir set — should create a shared temp dir
	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// _workdir should be set in the result context
	workDir := result.Context.GetString("_workdir", "")
	if workDir == "" {
		t.Fatal("expected _workdir to be set in result context, but it was empty")
	}

	// The directory should exist on disk
	info, err := os.Stat(workDir)
	if err != nil {
		t.Fatalf("expected _workdir %q to exist on disk, got error: %v", workDir, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected _workdir %q to be a directory", workDir)
	}

	// The handler should have seen the same value
	if observedWorkDir != workDir {
		t.Errorf("handler saw _workdir=%q, but result context has %q", observedWorkDir, workDir)
	}

	// Clean up the temp dir
	os.RemoveAll(workDir)
}

func TestRunGraphUsesExplicitArtifactDir(t *testing.T) {
	g := buildLinearGraph()

	explicitDir := t.TempDir()

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		ArtifactDir:  explicitDir,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// _workdir should be set to the explicit dir
	workDir := result.Context.GetString("_workdir", "")
	if workDir != explicitDir {
		t.Errorf("expected _workdir=%q, got %q", explicitDir, workDir)
	}
}

// stubCodergenBackend is a test double for CodergenBackend used in engine tests.
type stubCodergenBackend struct {
	runFn func(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error)
}

func (b *stubCodergenBackend) RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
	if b.runFn != nil {
		return b.runFn(ctx, config)
	}
	return &AgentRunResult{Success: true}, nil
}

// --- Handler panic recovery tests ---

func TestEngineHandlerPanicRecoveryString(t *testing.T) {
	// A handler that panics with a string should not crash the engine.
	// The panic is caught by safeExecute, converted to an error, which
	// executeWithRetry then wraps into a StatusFail outcome. The engine
	// sees the fail and routes accordingly (error if no fail edge exists).
	g := buildLinearGraph()

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			panic("something went terribly wrong")
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	// The panic becomes a StatusFail outcome with the panic message in FailureReason.
	// Since node "a" has no fail edge, the engine errors out.
	if err == nil {
		// If there's no routing error, verify the outcome captures the panic
		if result != nil && result.NodeOutcomes["a"] != nil {
			outcome := result.NodeOutcomes["a"]
			if outcome.Status != StatusFail {
				t.Errorf("expected StatusFail, got %v", outcome.Status)
			}
			if !strings.Contains(outcome.FailureReason, "panic") {
				t.Errorf("expected failure reason to mention 'panic', got: %v", outcome.FailureReason)
			}
			return
		}
		t.Fatal("expected error or fail outcome from panicking handler, got nil")
	}
	// The error should mention the node failure
	if !strings.Contains(err.Error(), "fail") {
		t.Errorf("expected error to mention 'fail', got: %v", err)
	}
}

func TestEngineHandlerPanicRecoveryError(t *testing.T) {
	// A handler that panics with an error value should not crash the engine.
	// The panic is caught by safeExecute, converted to an error, which
	// executeWithRetry wraps into a StatusFail outcome. With unconditional
	// edges, the pipeline routes past the failed node without crashing.
	g := buildLinearGraph()

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			panic(fmt.Errorf("error-typed panic"))
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	// The pipeline should not crash. With unconditional edges, the fail
	// outcome is routed onward. Verify the engine handled it gracefully.
	if err != nil {
		// An error is also acceptable (e.g. "no outgoing fail edge")
		if !strings.Contains(err.Error(), "fail") {
			t.Errorf("expected error to mention 'fail', got: %v", err)
		}
		return
	}
	// If no error, verify the panicking nodes recorded fail outcomes
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	outcomeA := result.NodeOutcomes["a"]
	if outcomeA == nil {
		t.Fatal("expected outcome for node 'a'")
	}
	if outcomeA.Status != StatusFail {
		t.Errorf("expected StatusFail for panicking node 'a', got %v", outcomeA.Status)
	}
	if !strings.Contains(outcomeA.FailureReason, "panic") {
		t.Errorf("expected failure reason to contain 'panic', got: %v", outcomeA.FailureReason)
	}
}

func TestEngineHandlerPanicRecoveryNil(t *testing.T) {
	// A handler that panics with nil should still be caught and not crash.
	// In Go 1.21+, panic(nil) produces a *runtime.PanicNilError so
	// recover() returns a non-nil value, which safeExecute converts to an error.
	g := buildLinearGraph()

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			panic(nil)
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	// The pipeline should not crash. Verify the engine handled it gracefully.
	if err != nil {
		// An error is acceptable (e.g. "no outgoing fail edge")
		if !strings.Contains(err.Error(), "fail") {
			t.Errorf("expected error to mention 'fail', got: %v", err)
		}
		return
	}
	// If no error, verify the panicking nodes recorded fail outcomes
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	outcomeA := result.NodeOutcomes["a"]
	if outcomeA == nil {
		t.Fatal("expected outcome for node 'a'")
	}
	if outcomeA.Status != StatusFail {
		t.Errorf("expected StatusFail for panicking node 'a', got %v", outcomeA.Status)
	}
	if !strings.Contains(outcomeA.FailureReason, "panic") {
		t.Errorf("expected failure reason to contain 'panic', got: %v", outcomeA.FailureReason)
	}
}

func TestEngineHandlerPanicRecoveryTerminalNode(t *testing.T) {
	// A terminal node handler that panics should also be caught.
	g := &Graph{
		Name:         "terminal_panic",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}
	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "exit", Attrs: map[string]string{}},
	)

	startH := newSuccessHandler("start")
	exitH := &testHandler{
		typeName: "exit",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			panic("terminal node explosion")
		},
	}
	reg := buildTestRegistry(startH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err == nil {
		t.Fatal("expected error from panicking terminal handler, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected error to mention 'panic', got: %v", err)
	}
	if !strings.Contains(err.Error(), "terminal node explosion") {
		t.Errorf("expected error to contain panic message, got: %v", err)
	}
}

func TestSafeExecuteDirectPanicString(t *testing.T) {
	// Directly test safeExecute to verify panic message content is preserved.
	node := &Node{ID: "panicker", Attrs: map[string]string{}}
	handler := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			panic("something went terribly wrong")
		},
	}
	pctx := NewContext()

	outcome, err := safeExecute(context.Background(), handler, node, pctx, nil)
	if outcome != nil {
		t.Errorf("expected nil outcome from panic, got %v", outcome)
	}
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected error to mention 'panic', got: %v", err)
	}
	if !strings.Contains(err.Error(), "something went terribly wrong") {
		t.Errorf("expected error to contain panic message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "panicker") {
		t.Errorf("expected error to contain node ID, got: %v", err)
	}
}

func TestSafeExecuteDirectPanicError(t *testing.T) {
	// Directly test safeExecute with an error-typed panic value.
	node := &Node{ID: "err_panicker", Attrs: map[string]string{}}
	handler := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			panic(fmt.Errorf("error-typed panic"))
		},
	}
	pctx := NewContext()

	outcome, err := safeExecute(context.Background(), handler, node, pctx, nil)
	if outcome != nil {
		t.Errorf("expected nil outcome from panic, got %v", outcome)
	}
	if err == nil {
		t.Fatal("expected error from error-typed panic, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected error to mention 'panic', got: %v", err)
	}
	if !strings.Contains(err.Error(), "error-typed panic") {
		t.Errorf("expected error to contain panic value, got: %v", err)
	}
}

func TestSafeExecuteDirectPanicNil(t *testing.T) {
	// Directly test safeExecute with a nil panic value.
	node := &Node{ID: "nil_panicker", Attrs: map[string]string{}}
	handler := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			panic(nil)
		},
	}
	pctx := NewContext()

	outcome, err := safeExecute(context.Background(), handler, node, pctx, nil)
	if outcome != nil {
		t.Errorf("expected nil outcome from nil panic, got %v", outcome)
	}
	if err == nil {
		t.Fatal("expected error from nil panic, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected error to mention 'panic', got: %v", err)
	}
}

func TestSafeExecuteDirectNoPanic(t *testing.T) {
	// Directly test safeExecute with a normal (non-panicking) handler.
	node := &Node{ID: "normal", Attrs: map[string]string{}}
	handler := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	pctx := NewContext()

	outcome, err := safeExecute(context.Background(), handler, node, pctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome == nil {
		t.Fatal("expected non-nil outcome")
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", outcome.Status)
	}
}

func TestEngineHandlerNoPanicStillWorks(t *testing.T) {
	// Regression: normal (non-panicking) handlers still work correctly
	// after adding panic recovery.
	g := buildLinearGraph()

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.CompletedNodes) != 4 {
		t.Errorf("expected 4 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}
}
