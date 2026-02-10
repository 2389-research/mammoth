// ABOUTME: Tests for loop_restart edge attribute handling and engine restart behavior.
// ABOUTME: Covers ErrLoopRestart sentinel, restart wrapper, fresh context, max restart limits, and checkpointing.
package attractor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestErrLoopRestartSentinel(t *testing.T) {
	err := &ErrLoopRestart{TargetNode: "node_b"}
	if err.TargetNode != "node_b" {
		t.Errorf("expected TargetNode 'node_b', got %q", err.TargetNode)
	}
	if !strings.Contains(err.Error(), "node_b") {
		t.Errorf("expected error message to contain 'node_b', got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "loop_restart") {
		t.Errorf("expected error message to contain 'loop_restart', got %q", err.Error())
	}
}

func TestErrLoopRestartIsDetectedByErrorsAs(t *testing.T) {
	err := &ErrLoopRestart{TargetNode: "target"}
	var target *ErrLoopRestart
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to detect ErrLoopRestart")
	}
	if target.TargetNode != "target" {
		t.Errorf("expected TargetNode 'target', got %q", target.TargetNode)
	}
}

// buildRestartGraph creates: start -> a -> b -> exit
// with edge a->b having loop_restart=true
func buildRestartGraph() *Graph {
	g := &Graph{
		Name:         "restart_test",
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
		&Edge{From: "a", To: "b", Attrs: map[string]string{"loop_restart": "true"}},
		&Edge{From: "b", To: "exit", Attrs: map[string]string{}},
	)
	return g
}

func TestLoopRestartEdgeTriggers(t *testing.T) {
	g := buildRestartGraph()

	var nodesExecuted []string

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			nodesExecuted = append(nodesExecuted, node.ID)
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	// Graph: start -> a -> b (loop_restart) -> exit
	// Flow: execute start, a, then edge a->b has loop_restart=true.
	// Engine restarts from b with fresh context. Execute b, then b->exit.
	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Node 'b' should be in completed nodes (it runs after restart)
	foundB := false
	for _, n := range result.CompletedNodes {
		if n == "b" {
			foundB = true
		}
	}
	if !foundB {
		t.Errorf("expected node 'b' in completed nodes after restart, got: %v", result.CompletedNodes)
	}
}

func TestLoopRestartFreshContext(t *testing.T) {
	g := buildRestartGraph()

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "a" {
				// Set a context value that should be cleared on restart
				return &Outcome{
					Status:         StatusSuccess,
					ContextUpdates: map[string]any{"from_a": "should_be_cleared"},
				}, nil
			}
			if node.ID == "b" {
				// After restart, context should be fresh - from_a should not exist
				val := pctx.GetString("from_a", "not_found")
				return &Outcome{
					Status:         StatusSuccess,
					ContextUpdates: map[string]any{"b_saw_from_a": val},
				}, nil
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

	// After restart, context should be fresh, so b should not see from_a
	val := result.Context.GetString("b_saw_from_a", "")
	if val != "not_found" {
		t.Errorf("expected fresh context after restart (b_saw_from_a='not_found'), got %q", val)
	}
}

func TestLoopRestartMaxRestartsLimit(t *testing.T) {
	// Build a graph where restart always loops back:
	// start -> a -> b -> exit, where a -> b has loop_restart=true
	// After restart from b: b -> a (unconditional, lexically before exit),
	// then a -> b (loop_restart again), restart from b, etc.
	// b has two outgoing edges: b -> a and b -> exit.
	// By lexical ordering, b -> a is chosen first, creating the loop.
	g := &Graph{
		Name:         "infinite_restart",
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
		&Edge{From: "a", To: "b", Attrs: map[string]string{"loop_restart": "true"}},
		&Edge{From: "b", To: "a", Attrs: map[string]string{}},
		&Edge{From: "b", To: "exit", Attrs: map[string]string{}},
	)

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:      reg,
		DefaultRetry:  RetryPolicyNone(),
		RestartConfig: &RestartConfig{MaxRestarts: 3},
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err == nil {
		t.Fatal("expected error when max restarts exceeded")
	}
	if !strings.Contains(err.Error(), "restart") {
		t.Errorf("expected error about restart limit, got: %v", err)
	}
}

func TestLoopRestartDefaultMaxRestarts(t *testing.T) {
	// Verify that the default max restarts is 5
	cfg := DefaultRestartConfig()
	if cfg.MaxRestarts != 5 {
		t.Errorf("expected default MaxRestarts=5, got %d", cfg.MaxRestarts)
	}
}

func TestLoopRestartFalseDoesNotTrigger(t *testing.T) {
	// Edge with loop_restart=false should not trigger restart
	g := &Graph{
		Name:         "no_restart",
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
		&Edge{From: "a", To: "b", Attrs: map[string]string{"loop_restart": "false"}},
		&Edge{From: "b", To: "exit", Attrs: map[string]string{}},
	)

	var nodesExecuted []string
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			nodesExecuted = append(nodesExecuted, node.ID)
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

	// Normal traversal: start -> a -> b -> exit
	if len(result.CompletedNodes) != 4 {
		t.Errorf("expected 4 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}
}

func TestLoopRestartAbsentDoesNotTrigger(t *testing.T) {
	// Edge without loop_restart attr at all should not trigger restart
	g := buildLinearGraph() // standard: start -> a -> b -> exit, no loop_restart

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

	if len(result.CompletedNodes) != 4 {
		t.Errorf("expected 4 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}
}

func TestLoopRestartCheckpointSavedBeforeRestart(t *testing.T) {
	g := buildRestartGraph()
	cpDir := t.TempDir()

	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Handlers:      reg,
		DefaultRetry:  RetryPolicyNone(),
		CheckpointDir: cpDir,
	})

	_, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Checkpoint files should exist (at least the pre-restart checkpoint)
	entries, err := os.ReadDir(cpDir)
	if err != nil {
		t.Fatalf("error reading checkpoint dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one checkpoint file before restart")
	}
}

func TestLoopRestartContextCancellationDuringRestart(t *testing.T) {
	g := buildRestartGraph()

	restartCount := 0
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			if node.ID == "b" {
				restartCount++
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := engine.RunGraph(ctx, g)
	if err == nil {
		t.Fatal("expected error for cancelled context during restart")
	}
}

func TestEdgeHasLoopRestart(t *testing.T) {
	tests := []struct {
		name     string
		attrs    map[string]string
		expected bool
	}{
		{"true value", map[string]string{"loop_restart": "true"}, true},
		{"false value", map[string]string{"loop_restart": "false"}, false},
		{"absent", map[string]string{}, false},
		{"nil attrs", nil, false},
		{"empty string", map[string]string{"loop_restart": ""}, false},
		{"uppercase TRUE", map[string]string{"loop_restart": "TRUE"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edge := &Edge{From: "a", To: "b", Attrs: tt.attrs}
			got := EdgeHasLoopRestart(edge)
			if got != tt.expected {
				t.Errorf("EdgeHasLoopRestart() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRestartConfigDefaults(t *testing.T) {
	cfg := DefaultRestartConfig()
	if cfg.MaxRestarts != 5 {
		t.Errorf("expected MaxRestarts=5, got %d", cfg.MaxRestarts)
	}
}

func TestLoopRestartGraphAttrsPreservedAcrossRestart(t *testing.T) {
	g := buildRestartGraph()
	g.Attrs["goal"] = "build widgets"

	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "b" {
				// After restart with fresh context, graph attrs should still be mirrored
				goal := pctx.GetString("goal", "")
				return &Outcome{
					Status:         StatusSuccess,
					ContextUpdates: map[string]any{"b_saw_goal": goal},
				}, nil
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

	// Graph attrs should be mirrored into the fresh context
	val := result.Context.GetString("b_saw_goal", "")
	if val != "build widgets" {
		t.Errorf("expected graph attrs preserved across restart (b_saw_goal='build widgets'), got %q", val)
	}
}
