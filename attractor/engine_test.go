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
