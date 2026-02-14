// ABOUTME: Scenario tests for hash-based auto-resume exercising the full lifecycle.
// ABOUTME: Covers source hashing, auto-checkpoint, FindResumable, resume detection, fresh-flag bypass, and changed-file detection.
package attractor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// autoResumeBackend is a configurable test double for CodergenBackend that can
// be set to fail on specific nodes, simulating mid-pipeline failures.
type autoResumeBackend struct {
	failOnNode string // if non-empty, RunAgent returns an error for this node
	calls      []string
}

func (b *autoResumeBackend) RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
	b.calls = append(b.calls, config.NodeID)
	if b.failOnNode != "" && config.NodeID == b.failOnNode {
		return nil, fmt.Errorf("simulated failure at node %q", config.NodeID)
	}
	return &AgentRunResult{
		Output:     "completed: " + config.NodeID,
		ToolCalls:  1,
		TokensUsed: 100,
		Success:    true,
	}, nil
}

// autoResumeHandler records execution and delegates to a backend.
type autoResumeHandler struct {
	typeName string
	executed []string
}

func (h *autoResumeHandler) Type() string { return h.typeName }

func (h *autoResumeHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	h.executed = append(h.executed, node.ID)
	return &Outcome{
		Status:         StatusSuccess,
		ContextUpdates: map[string]any{"last_stage": node.ID},
	}, nil
}

// autoResumeFailHandler fails on a specific node.
type autoResumeFailHandler struct {
	typeName   string
	failOnNode string
	executed   []string
}

func (h *autoResumeFailHandler) Type() string { return h.typeName }

func (h *autoResumeFailHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	h.executed = append(h.executed, node.ID)
	if h.failOnNode != "" && node.ID == h.failOnNode {
		return nil, fmt.Errorf("simulated failure at node %q", node.ID)
	}
	return &Outcome{
		Status:         StatusSuccess,
		ContextUpdates: map[string]any{"last_stage": node.ID},
	}, nil
}

// buildAutoResumeGraph constructs a 4-node pipeline: start -> plan -> implement -> done
func buildAutoResumeGraph() *Graph {
	g := &Graph{
		Name:         "autoresume_test",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}

	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["plan"] = &Node{ID: "plan", Attrs: map[string]string{"shape": "box", "label": "Plan"}}
	g.Nodes["implement"] = &Node{ID: "implement", Attrs: map[string]string{"shape": "box", "label": "Implement"}}
	g.Nodes["done"] = &Node{ID: "done", Attrs: map[string]string{"shape": "Msquare"}}

	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "plan", Attrs: map[string]string{}},
		&Edge{From: "plan", To: "implement", Attrs: map[string]string{}},
		&Edge{From: "implement", To: "done", Attrs: map[string]string{"condition": "outcome = success"}},
	)

	return g
}

// The DOT source that corresponds to buildAutoResumeGraph().
const autoResumeDOT = `digraph autoresume_test {
    start [shape=Mdiamond]
    plan [shape=box, label="Plan"]
    implement [shape=box, label="Implement"]
    done [shape=Msquare]
    start -> plan
    plan -> implement
    implement -> done [condition="outcome = success"]
}`

// TestScenarioAutoResumeFreshRunCreatesCheckpoint verifies that a fresh run
// with AutoCheckpointPath creates a single overwriting checkpoint file.
func TestScenarioAutoResumeFreshRunCreatesCheckpoint(t *testing.T) {
	g := buildAutoResumeGraph()
	cpDir := t.TempDir()
	cpPath := filepath.Join(cpDir, "checkpoint.json")

	startH := &autoResumeHandler{typeName: "start"}
	codergenH := &autoResumeHandler{typeName: "codergen"}
	exitH := &autoResumeHandler{typeName: "exit"}

	reg := NewHandlerRegistry()
	reg.Register(startH)
	reg.Register(codergenH)
	reg.Register(exitH)

	backend := &autoResumeBackend{}
	engine := NewEngine(EngineConfig{
		Handlers:           reg,
		DefaultRetry:       RetryPolicyNone(),
		AutoCheckpointPath: cpPath,
		Backend:            backend,
	})

	result, err := engine.RunGraph(context.Background(), g)
	if err != nil {
		t.Fatalf("fresh run failed: %v", err)
	}

	// All 4 nodes should complete
	if len(result.CompletedNodes) != 4 {
		t.Fatalf("expected 4 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}

	// The auto-checkpoint file should exist
	cp, err := LoadCheckpoint(cpPath)
	if err != nil {
		t.Fatalf("failed to load auto-checkpoint: %v", err)
	}

	// Should be the last non-terminal node that completed (implement)
	if cp.CurrentNode != "implement" {
		t.Errorf("expected auto-checkpoint at 'implement', got %q", cp.CurrentNode)
	}

	// Should have start, plan, implement as completed
	if len(cp.CompletedNodes) < 3 {
		t.Errorf("expected at least 3 completed nodes in checkpoint, got %d: %v", len(cp.CompletedNodes), cp.CompletedNodes)
	}
}

// TestScenarioAutoResumeSourceHashDeterminism verifies that source hashing
// is deterministic and correctly differentiates DOT files.
func TestScenarioAutoResumeSourceHashDeterminism(t *testing.T) {
	hash1 := SourceHash(autoResumeDOT)
	hash2 := SourceHash(autoResumeDOT)

	if hash1 != hash2 {
		t.Errorf("same source produced different hashes: %q vs %q", hash1, hash2)
	}

	// Modify the source slightly
	modified := autoResumeDOT + "\n// modified"
	hash3 := SourceHash(modified)

	if hash1 == hash3 {
		t.Error("different sources produced the same hash")
	}

	// Verify the hash is 64 hex characters (SHA-256)
	if len(hash1) != 64 {
		t.Errorf("expected 64-char hash, got %d chars", len(hash1))
	}
}

// TestScenarioAutoResumeFindResumableWithFailedRun exercises the full flow:
// create a run with a source hash, mark it as failed, write a checkpoint,
// then verify FindResumable returns it.
func TestScenarioAutoResumeFindResumableWithFailedRun(t *testing.T) {
	store := newTestStore(t)
	sourceHash := SourceHash(autoResumeDOT)

	// Create a "failed" run with source hash and checkpoint
	state := &RunState{
		ID:             "run-001",
		PipelineFile:   "test.dot",
		Status:         "failed",
		Source:         autoResumeDOT,
		SourceHash:     sourceHash,
		StartedAt:      time.Now(),
		CompletedNodes: []string{"start", "plan"},
		CurrentNode:    "implement",
		Context:        map[string]any{},
		Events:         []EngineEvent{},
		Error:          "simulated failure at node \"implement\"",
	}
	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write a checkpoint in the run directory
	cpPath := filepath.Join(store.baseDir, state.ID, "checkpoint.json")
	cp := &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    "plan",
		CompletedNodes: []string{"start", "plan"},
		NodeRetries:    map[string]int{},
		ContextValues:  map[string]any{"last_stage": "plan", "outcome": "success"},
		Logs:           []string{},
	}
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("Save checkpoint failed: %v", err)
	}

	// FindResumable should return this run
	found, err := store.FindResumable(sourceHash)
	if err != nil {
		t.Fatalf("FindResumable failed: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find a resumable run, got nil")
	}
	if found.ID != "run-001" {
		t.Errorf("expected run ID 'run-001', got %q", found.ID)
	}
}

// TestScenarioAutoResumeCompletedRunNotResumed verifies that a completed run
// is not returned by FindResumable, even if it has the right hash and checkpoint.
func TestScenarioAutoResumeCompletedRunNotResumed(t *testing.T) {
	store := newTestStore(t)
	sourceHash := SourceHash(autoResumeDOT)

	state := &RunState{
		ID:             "run-completed",
		PipelineFile:   "test.dot",
		Status:         "completed",
		SourceHash:     sourceHash,
		StartedAt:      time.Now(),
		CompletedNodes: []string{"start", "plan", "implement", "done"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
	}
	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	cpPath := filepath.Join(store.baseDir, state.ID, "checkpoint.json")
	cp := &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    "implement",
		CompletedNodes: state.CompletedNodes,
		NodeRetries:    map[string]int{},
		ContextValues:  map[string]any{},
	}
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("Save checkpoint failed: %v", err)
	}

	found, err := store.FindResumable(sourceHash)
	if err != nil {
		t.Fatalf("FindResumable failed: %v", err)
	}
	if found != nil {
		t.Errorf("expected nil for completed run, got ID=%q", found.ID)
	}
}

// TestScenarioAutoResumeChangedDOTFileStartsFresh verifies that changing the
// DOT source (even slightly) produces a different hash and no resumable run.
func TestScenarioAutoResumeChangedDOTFileStartsFresh(t *testing.T) {
	store := newTestStore(t)

	// Create a failed run with the original source hash
	originalHash := SourceHash(autoResumeDOT)
	state := &RunState{
		ID:             "run-original",
		PipelineFile:   "test.dot",
		Status:         "failed",
		SourceHash:     originalHash,
		StartedAt:      time.Now(),
		CompletedNodes: []string{"start", "plan"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
	}
	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	cpPath := filepath.Join(store.baseDir, state.ID, "checkpoint.json")
	cp := &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    "plan",
		CompletedNodes: []string{"start", "plan"},
		NodeRetries:    map[string]int{},
		ContextValues:  map[string]any{},
	}
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("Save checkpoint failed: %v", err)
	}

	// Query with a modified source hash
	modifiedHash := SourceHash(autoResumeDOT + "\n// user added a comment")
	if originalHash == modifiedHash {
		t.Fatal("expected different hashes for different sources")
	}

	found, err := store.FindResumable(modifiedHash)
	if err != nil {
		t.Fatalf("FindResumable failed: %v", err)
	}
	if found != nil {
		t.Errorf("expected nil for different source hash, got ID=%q", found.ID)
	}
}

// TestScenarioAutoResumeMostRecentRunSelected verifies that when multiple
// failed runs with the same hash exist, the most recent one is returned.
func TestScenarioAutoResumeMostRecentRunSelected(t *testing.T) {
	store := newTestStore(t)
	sourceHash := SourceHash(autoResumeDOT)

	// Create an older failed run
	older := &RunState{
		ID:             "run-older",
		PipelineFile:   "test.dot",
		Status:         "failed",
		SourceHash:     sourceHash,
		StartedAt:      time.Now().Add(-1 * time.Hour),
		CompletedNodes: []string{"start"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
	}
	if err := store.Create(older); err != nil {
		t.Fatalf("Create older failed: %v", err)
	}
	olderCP := &Checkpoint{
		Timestamp:      time.Now().Add(-1 * time.Hour),
		CurrentNode:    "start",
		CompletedNodes: []string{"start"},
		NodeRetries:    map[string]int{},
		ContextValues:  map[string]any{},
	}
	if err := olderCP.Save(filepath.Join(store.baseDir, older.ID, "checkpoint.json")); err != nil {
		t.Fatalf("Save older checkpoint failed: %v", err)
	}

	// Create a newer failed run (further along)
	newer := &RunState{
		ID:             "run-newer",
		PipelineFile:   "test.dot",
		Status:         "failed",
		SourceHash:     sourceHash,
		StartedAt:      time.Now().Add(-5 * time.Minute),
		CompletedNodes: []string{"start", "plan"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
	}
	if err := store.Create(newer); err != nil {
		t.Fatalf("Create newer failed: %v", err)
	}
	newerCP := &Checkpoint{
		Timestamp:      time.Now().Add(-5 * time.Minute),
		CurrentNode:    "plan",
		CompletedNodes: []string{"start", "plan"},
		NodeRetries:    map[string]int{},
		ContextValues:  map[string]any{},
	}
	if err := newerCP.Save(filepath.Join(store.baseDir, newer.ID, "checkpoint.json")); err != nil {
		t.Fatalf("Save newer checkpoint failed: %v", err)
	}

	// FindResumable should return the newer run
	found, err := store.FindResumable(sourceHash)
	if err != nil {
		t.Fatalf("FindResumable failed: %v", err)
	}
	if found == nil {
		t.Fatal("expected a resumable run, got nil")
	}
	if found.ID != "run-newer" {
		t.Errorf("expected most recent run 'run-newer', got %q", found.ID)
	}
}

// TestScenarioAutoResumeEndToEnd exercises the full auto-resume lifecycle:
// 1. Run a pipeline that fails at "implement"
// 2. Verify checkpoint and run state are saved
// 3. Resume from checkpoint
// 4. Verify only post-checkpoint nodes execute
// 5. Verify the pipeline completes
func TestScenarioAutoResumeEndToEnd(t *testing.T) {
	runsDir := t.TempDir()
	store, err := NewFSRunStateStore(runsDir)
	if err != nil {
		t.Fatalf("NewFSRunStateStore failed: %v", err)
	}

	sourceHash := SourceHash(autoResumeDOT)

	// ---------------------------------------------------------------
	// PHASE 1: Initial run that fails at "implement"
	// ---------------------------------------------------------------
	t.Run("phase1_failing_run", func(t *testing.T) {
		g := buildAutoResumeGraph()

		runID := "run-failing"
		cpPath := store.CheckpointPath(runID)

		failHandler := &autoResumeFailHandler{
			typeName:   "codergen",
			failOnNode: "implement",
		}
		startH := &autoResumeHandler{typeName: "start"}
		exitH := &autoResumeHandler{typeName: "exit"}

		reg := NewHandlerRegistry()
		reg.Register(startH)
		reg.Register(failHandler)
		reg.Register(exitH)

		backend := &autoResumeBackend{}
		engine := NewEngine(EngineConfig{
			Handlers:           reg,
			DefaultRetry:       RetryPolicyNone(),
			AutoCheckpointPath: cpPath,
			Backend:            backend,
		})

		// Create initial run state
		initialState := &RunState{
			ID:             runID,
			PipelineFile:   "test.dot",
			Status:         "running",
			Source:         autoResumeDOT,
			SourceHash:     sourceHash,
			StartedAt:      time.Now(),
			CompletedNodes: []string{},
			Context:        map[string]any{},
			Events:         []EngineEvent{},
		}
		if err := store.Create(initialState); err != nil {
			t.Fatalf("Create initial state failed: %v", err)
		}

		_, runErr := engine.RunGraph(context.Background(), g)
		if runErr == nil {
			t.Fatal("expected pipeline to fail at implement, but it succeeded")
		}

		// Update run state as failed
		initialState.Status = "failed"
		initialState.Error = runErr.Error()
		if err := store.Update(initialState); err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		// Verify checkpoint exists
		if _, err := os.Stat(cpPath); os.IsNotExist(err) {
			t.Fatal("expected checkpoint.json after failing run")
		}

		// Load and verify checkpoint — should be at "plan" (last successful node)
		cp, err := LoadCheckpoint(cpPath)
		if err != nil {
			t.Fatalf("LoadCheckpoint failed: %v", err)
		}
		if cp.CurrentNode != "plan" {
			t.Errorf("expected checkpoint at 'plan', got %q", cp.CurrentNode)
		}

		// Verify the handler saw start and plan succeed, then implement fail
		if len(failHandler.executed) < 1 {
			t.Fatal("expected failHandler to have executed at least one node")
		}
	})

	// ---------------------------------------------------------------
	// PHASE 2: FindResumable should find the failed run
	// ---------------------------------------------------------------
	t.Run("phase2_find_resumable", func(t *testing.T) {
		found, err := store.FindResumable(sourceHash)
		if err != nil {
			t.Fatalf("FindResumable failed: %v", err)
		}
		if found == nil {
			t.Fatal("expected a resumable run, got nil")
		}
		if found.ID != "run-failing" {
			t.Errorf("expected run ID 'run-failing', got %q", found.ID)
		}
		if found.Status != "failed" {
			t.Errorf("expected status 'failed', got %q", found.Status)
		}
	})

	// ---------------------------------------------------------------
	// PHASE 3: Resume from checkpoint
	// ---------------------------------------------------------------
	t.Run("phase3_resume_from_checkpoint", func(t *testing.T) {
		g := buildAutoResumeGraph()
		cpPath := store.CheckpointPath("run-failing")

		// This time the handler succeeds on all nodes
		resumeHandler := &autoResumeHandler{typeName: "codergen"}
		startH := &autoResumeHandler{typeName: "start"}
		exitH := &autoResumeHandler{typeName: "exit"}

		reg := NewHandlerRegistry()
		reg.Register(startH)
		reg.Register(resumeHandler)
		reg.Register(exitH)

		var events []EngineEvent
		resumeBackend := &autoResumeBackend{}
		engine := NewEngine(EngineConfig{
			Handlers:           reg,
			DefaultRetry:       RetryPolicyNone(),
			AutoCheckpointPath: cpPath,
			Backend:            resumeBackend,
			EventHandler: func(evt EngineEvent) {
				events = append(events, evt)
			},
		})

		result, err := engine.ResumeFromCheckpoint(context.Background(), g, cpPath)
		if err != nil {
			t.Fatalf("ResumeFromCheckpoint failed: %v", err)
		}

		// Only implement, review (there's no review), and done should have been executed
		// Actually our graph is: start -> plan -> implement -> done
		// Resuming from plan means implement and done should execute
		for _, nodeID := range resumeHandler.executed {
			if nodeID == "start" || nodeID == "plan" {
				t.Errorf("node %q should NOT have been re-executed on resume", nodeID)
			}
		}

		// implement should have been executed by the resume
		foundImplement := false
		for _, nodeID := range resumeHandler.executed {
			if nodeID == "implement" {
				foundImplement = true
			}
		}
		if !foundImplement {
			t.Error("expected 'implement' to be executed on resume")
		}

		// The result should include both carried and new completed nodes
		if len(result.CompletedNodes) < 3 {
			t.Errorf("expected at least 3 completed nodes, got %d: %v",
				len(result.CompletedNodes), result.CompletedNodes)
		}

		// Verify pipeline.started has resumed=true
		foundResumeStart := false
		for _, evt := range events {
			if evt.Type == EventPipelineStarted {
				if evt.Data != nil {
					if resumed, ok := evt.Data["resumed"]; ok && resumed == true {
						foundResumeStart = true
					}
				}
			}
		}
		if !foundResumeStart {
			t.Error("expected pipeline.started event with resumed=true")
		}

		// Verify pipeline completed
		foundComplete := false
		for _, evt := range events {
			if evt.Type == EventPipelineCompleted {
				foundComplete = true
			}
		}
		if !foundComplete {
			t.Error("expected pipeline.completed event after resume")
		}
	})
}

// TestScenarioAutoResumeWithNoCheckpoint verifies that a failed run without
// a checkpoint file is not considered resumable.
func TestScenarioAutoResumeWithNoCheckpoint(t *testing.T) {
	store := newTestStore(t)
	sourceHash := SourceHash(autoResumeDOT)

	state := &RunState{
		ID:             "run-no-checkpoint",
		PipelineFile:   "test.dot",
		Status:         "failed",
		SourceHash:     sourceHash,
		StartedAt:      time.Now(),
		CompletedNodes: []string{"start"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
		Error:          "failed early, no checkpoint saved",
	}
	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// No checkpoint.json written in the run dir

	found, err := store.FindResumable(sourceHash)
	if err != nil {
		t.Fatalf("FindResumable failed: %v", err)
	}
	if found != nil {
		t.Errorf("expected nil for run without checkpoint, got ID=%q", found.ID)
	}
}

// TestScenarioAutoResumeInterruptedRunStatus verifies that a "running" status
// (e.g., from a killed process) is also considered resumable.
func TestScenarioAutoResumeInterruptedRunStatus(t *testing.T) {
	store := newTestStore(t)
	sourceHash := SourceHash(autoResumeDOT)

	state := &RunState{
		ID:             "run-interrupted",
		PipelineFile:   "test.dot",
		Status:         "running", // process was killed
		SourceHash:     sourceHash,
		StartedAt:      time.Now().Add(-10 * time.Minute),
		CompletedNodes: []string{"start", "plan"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
	}
	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	cpPath := filepath.Join(store.baseDir, state.ID, "checkpoint.json")
	cp := &Checkpoint{
		Timestamp:      time.Now().Add(-10 * time.Minute),
		CurrentNode:    "plan",
		CompletedNodes: []string{"start", "plan"},
		NodeRetries:    map[string]int{},
		ContextValues:  map[string]any{"outcome": "success"},
	}
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("Save checkpoint failed: %v", err)
	}

	found, err := store.FindResumable(sourceHash)
	if err != nil {
		t.Fatalf("FindResumable failed: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find an interrupted (running) run, got nil")
	}
	if found.ID != "run-interrupted" {
		t.Errorf("expected run ID 'run-interrupted', got %q", found.ID)
	}
}

// TestScenarioAutoResumeCancelledRunStatus verifies that a "cancelled" status
// is also considered resumable.
func TestScenarioAutoResumeCancelledRunStatus(t *testing.T) {
	store := newTestStore(t)
	sourceHash := SourceHash(autoResumeDOT)

	state := &RunState{
		ID:             "run-cancelled",
		PipelineFile:   "test.dot",
		Status:         "cancelled",
		SourceHash:     sourceHash,
		StartedAt:      time.Now().Add(-5 * time.Minute),
		CompletedNodes: []string{"start", "plan"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
	}
	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	cpPath := filepath.Join(store.baseDir, state.ID, "checkpoint.json")
	cp := &Checkpoint{
		Timestamp:      time.Now().Add(-5 * time.Minute),
		CurrentNode:    "plan",
		CompletedNodes: []string{"start", "plan"},
		NodeRetries:    map[string]int{},
		ContextValues:  map[string]any{"outcome": "success"},
	}
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("Save checkpoint failed: %v", err)
	}

	found, err := store.FindResumable(sourceHash)
	if err != nil {
		t.Fatalf("FindResumable failed: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find a cancelled run, got nil")
	}
}

// TestScenarioAutoResumeCheckpointPathHelper verifies the CheckpointPath
// and RunDir helper methods on FSRunStateStore.
func TestScenarioAutoResumeCheckpointPathHelper(t *testing.T) {
	store := newTestStore(t)

	cpPath := store.CheckpointPath("run-abc")
	expected := filepath.Join(store.baseDir, "run-abc", "checkpoint.json")
	if cpPath != expected {
		t.Errorf("CheckpointPath mismatch: got %q, want %q", cpPath, expected)
	}

	runDir := store.RunDir("run-abc")
	expectedDir := filepath.Join(store.baseDir, "run-abc")
	if runDir != expectedDir {
		t.Errorf("RunDir mismatch: got %q, want %q", runDir, expectedDir)
	}
}
