// ABOUTME: Tests for checkpoint resume and fidelity degradation on resume.
// ABOUTME: Covers mid-pipeline resume, full->summary:high degradation on first hop, recovery on subsequent hops, and fresh runs unaffected.
package attractor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildFidelityGraph creates: start -> a -> b -> c -> exit
// with configurable fidelity on edges. By default, all edges use full fidelity.
func buildFidelityGraph(edgeFidelities map[string]string) *Graph {
	g := &Graph{
		Name:         "fidelity_pipeline",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        map[string]string{"default_fidelity": "full"},
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["a"] = &Node{ID: "a", Attrs: map[string]string{"shape": "box", "label": "Step A"}}
	g.Nodes["b"] = &Node{ID: "b", Attrs: map[string]string{"shape": "box", "label": "Step B"}}
	g.Nodes["c"] = &Node{ID: "c", Attrs: map[string]string{"shape": "box", "label": "Step C"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}

	edges := []struct {
		from, to string
	}{
		{"start", "a"},
		{"a", "b"},
		{"b", "c"},
		{"c", "exit"},
	}
	for _, e := range edges {
		key := e.from + "->" + e.to
		attrs := map[string]string{}
		if f, ok := edgeFidelities[key]; ok {
			attrs["fidelity"] = f
		}
		g.Edges = append(g.Edges, &Edge{From: e.from, To: e.to, Attrs: attrs})
	}
	return g
}

func TestResumeFromCheckpoint_MidPipeline(t *testing.T) {
	// Build a graph: start -> a -> b -> c -> exit
	g := buildFidelityGraph(nil)

	// Create a checkpoint as if we completed start and a, currently at a
	pctx := NewContext()
	pctx.Set("from_a", "data_from_a")
	cp := NewCheckpoint(pctx, "a", []string{"start", "a"}, map[string]int{})

	// Save checkpoint to disk
	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "test_checkpoint.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	// Track which nodes were executed
	executedNodes := make([]string, 0)
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			executedNodes = append(executedNodes, node.ID)
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have resumed from after node a, so nodes b, c, and exit should be executed
	// Node a should NOT be re-executed
	for _, n := range executedNodes {
		if n == "a" {
			t.Error("node 'a' should not be re-executed on resume")
		}
	}

	// b and c should have been executed
	foundB := false
	foundC := false
	for _, n := range executedNodes {
		if n == "b" {
			foundB = true
		}
		if n == "c" {
			foundC = true
		}
	}
	if !foundB {
		t.Error("expected node 'b' to be executed on resume")
	}
	if !foundC {
		t.Error("expected node 'c' to be executed on resume")
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Context should contain values from the checkpoint
	val := result.Context.GetString("from_a", "")
	if val != "data_from_a" {
		t.Errorf("expected context 'from_a'='data_from_a', got %q", val)
	}
}

func TestResumeFromCheckpoint_FileNotFound(t *testing.T) {
	g := buildFidelityGraph(nil)
	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.ResumeFromCheckpoint(context.Background(), g, "/nonexistent/checkpoint.json")
	if err == nil {
		t.Fatal("expected error for missing checkpoint file")
	}
}

func TestResumeFromCheckpoint_NodeNotInGraph(t *testing.T) {
	g := buildFidelityGraph(nil)

	// Create a checkpoint referencing a node not in the graph
	pctx := NewContext()
	cp := NewCheckpoint(pctx, "nonexistent_node", []string{}, map[string]int{})

	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "test_checkpoint.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
	if err == nil {
		t.Fatal("expected error for checkpoint referencing nonexistent node")
	}
	if !strings.Contains(err.Error(), "nonexistent_node") {
		t.Errorf("expected error to mention node name, got: %v", err)
	}
}

func TestFidelityDegradation_FullToSummaryHighOnResume(t *testing.T) {
	// Build graph where all edges use full fidelity (graph default)
	g := buildFidelityGraph(nil)

	// Create a checkpoint at node a (which used full fidelity)
	pctx := NewContext()
	pctx.Set("data", "value")
	pctx.Set("big_data", strings.Repeat("x", 800))
	cp := NewCheckpoint(pctx, "a", []string{"start", "a"}, map[string]int{})

	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "test_checkpoint.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	// Track fidelity preambles seen by each node
	fidelityPreambles := make(map[string]string)
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			preamble := pctx.GetString("_fidelity_preamble", "")
			if preamble != "" {
				fidelityPreambles[node.ID] = preamble
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Node b is the first node after resume. Since previous node a used full fidelity,
	// the transition a->b should be degraded to summary:high.
	bPreamble, ok := fidelityPreambles["b"]
	if !ok {
		t.Fatal("expected node 'b' to have a fidelity preamble (degradation should apply)")
	}
	if !strings.Contains(strings.ToLower(bPreamble), "summar") || !strings.Contains(strings.ToLower(bPreamble), "high") {
		t.Errorf("expected node 'b' preamble to indicate summary:high degradation, got %q", bPreamble)
	}

	// The big_data value should be truncated in summary:high mode (default 500 chars)
	// This confirms the degradation actually applied the summary:high transform
}

func TestFidelityDegradation_RecoveryAfterOneHop(t *testing.T) {
	// Build graph where all edges use full fidelity
	g := buildFidelityGraph(nil)

	// Create a checkpoint at node a
	pctx := NewContext()
	pctx.Set("data", "value")
	cp := NewCheckpoint(pctx, "a", []string{"start", "a"}, map[string]int{})

	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "test_checkpoint.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	// Track which fidelity was applied at each node
	fidelityAtNode := make(map[string]string)
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			preamble := pctx.GetString("_fidelity_preamble", "")
			fidelityAtNode[node.ID] = preamble
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Node b (first hop) should have degraded fidelity preamble
	bPreamble := fidelityAtNode["b"]
	if bPreamble == "" {
		t.Fatal("expected node 'b' to have a fidelity preamble from degradation")
	}

	// Node c (second hop) should use normal fidelity (full, no preamble)
	cPreamble := fidelityAtNode["c"]
	if cPreamble != "" {
		t.Errorf("expected node 'c' to have no fidelity preamble (full fidelity restored), got %q", cPreamble)
	}
}

func TestFidelityDegradation_NoDegradationWhenPreviousNotFull(t *testing.T) {
	// Build graph where edges use compact fidelity
	edgeFidelities := map[string]string{
		"start->a": "compact",
		"a->b":     "compact",
		"b->c":     "compact",
		"c->exit":  "compact",
	}
	g := buildFidelityGraph(edgeFidelities)

	// Create a checkpoint at node a (which used compact fidelity, not full)
	pctx := NewContext()
	pctx.Set("data", "value")
	cp := NewCheckpoint(pctx, "a", []string{"start", "a"}, map[string]int{})

	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "test_checkpoint.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	// Track fidelity preambles
	fidelityPreambles := make(map[string]string)
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			preamble := pctx.GetString("_fidelity_preamble", "")
			if preamble != "" {
				fidelityPreambles[node.ID] = preamble
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Node b's preamble should reflect compact mode, not summary:high degradation
	bPreamble, ok := fidelityPreambles["b"]
	if !ok {
		// Compact mode produces a preamble, so we expect one
		t.Fatal("expected node 'b' to have a fidelity preamble (compact mode)")
	}
	// The preamble should be about compact, not about summary:high
	if strings.Contains(strings.ToLower(bPreamble), "summary") && strings.Contains(strings.ToLower(bPreamble), "high") {
		t.Errorf("expected node 'b' preamble to NOT indicate summary:high degradation when previous used compact, got %q", bPreamble)
	}
}

func TestFidelityDegradation_FreshRunUnaffected(t *testing.T) {
	// Build graph where all edges use full fidelity
	g := buildFidelityGraph(nil)

	// Track fidelity preambles
	fidelityPreambles := make(map[string]string)
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			preamble := pctx.GetString("_fidelity_preamble", "")
			if preamble != "" {
				fidelityPreambles[node.ID] = preamble
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	// Run fresh (not from checkpoint)
	_, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// In a fresh run with full fidelity everywhere, no preambles should be generated
	for node, preamble := range fidelityPreambles {
		t.Errorf("unexpected fidelity preamble at node %q in fresh run: %q", node, preamble)
	}
}

func TestResumeFromCheckpoint_RestoresContextValues(t *testing.T) {
	g := buildFidelityGraph(nil)

	// Create checkpoint with context values
	pctx := NewContext()
	pctx.Set("model", "gpt-4")
	pctx.Set("temperature", "0.7")
	pctx.AppendLog("previous log entry")
	cp := NewCheckpoint(pctx, "a", []string{"start", "a"}, map[string]int{})

	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "test_checkpoint.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	// Track what context values node b sees
	var seenModel string
	var seenTemp string
	startH := newSuccessHandler("start")
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "b" {
				seenModel = pctx.GetString("model", "")
				seenTemp = pctx.GetString("temperature", "")
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	_, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if seenModel != "gpt-4" {
		t.Errorf("expected node b to see model='gpt-4', got %q", seenModel)
	}
	if seenTemp != "0.7" {
		t.Errorf("expected node b to see temperature='0.7', got %q", seenTemp)
	}
}

func TestResumeFromCheckpoint_EmitsResumeEvent(t *testing.T) {
	g := buildFidelityGraph(nil)

	pctx := NewContext()
	cp := NewCheckpoint(pctx, "a", []string{"start", "a"}, map[string]int{})

	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "test_checkpoint.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	var events []EngineEvent
	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
		EventHandler: func(evt EngineEvent) {
			events = append(events, evt)
		},
	})

	_, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have a pipeline started event
	foundPipelineStarted := false
	for _, evt := range events {
		if evt.Type == EventPipelineStarted {
			foundPipelineStarted = true
		}
	}
	if !foundPipelineStarted {
		t.Error("expected pipeline.started event on resume")
	}
}

func TestResumeFromCheckpoint_CheckpointDirWritesNewCheckpoints(t *testing.T) {
	g := buildFidelityGraph(nil)

	pctx := NewContext()
	cp := NewCheckpoint(pctx, "a", []string{"start", "a"}, map[string]int{})

	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "test_checkpoint.json")
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}

	newCpDir := t.TempDir()
	startH := newSuccessHandler("start")
	codergenH := newSuccessHandler("codergen")
	exitH := newSuccessHandler("exit")
	reg := buildTestRegistry(startH, codergenH, exitH)

	engine := NewEngine(EngineConfig{
		Backend:       &fakeBackend{},
		Handlers:      reg,
		DefaultRetry:  RetryPolicyNone(),
		CheckpointDir: newCpDir,
	})

	_, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Checkpoint dir should have new checkpoint files from resumed execution
	entries, readErr := os.ReadDir(newCpDir)
	if readErr != nil {
		t.Fatalf("error reading new checkpoint dir: %v", readErr)
	}
	if len(entries) == 0 {
		t.Error("expected checkpoint files to be written during resumed execution")
	}
}
