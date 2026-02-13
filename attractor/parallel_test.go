// ABOUTME: Tests for parallel branch execution and context merging in the attractor pipeline.
// ABOUTME: Covers branch forking, concurrency limits, merge policies, failure handling, and context isolation.
package attractor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// buildParallelGraph creates a graph with a parallel fan-out, N branches, and a fan-in:
//
//	start -> parallel -> [branch_0, branch_1, ...] -> fanin -> exit
func buildParallelGraph(branchCount int) *Graph {
	g := &Graph{
		Name:         "parallel_test",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["parallel"] = &Node{ID: "parallel", Attrs: map[string]string{
		"shape":        "component",
		"join_policy":  "wait_all",
		"max_parallel": "4",
	}}
	g.Nodes["fanin"] = &Node{ID: "fanin", Attrs: map[string]string{"shape": "tripleoctagon"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}

	g.Edges = append(g.Edges, &Edge{From: "start", To: "parallel", Attrs: map[string]string{}})

	for i := 0; i < branchCount; i++ {
		branchID := fmt.Sprintf("branch_%d", i)
		g.Nodes[branchID] = &Node{ID: branchID, Attrs: map[string]string{
			"shape": "box",
			"label": fmt.Sprintf("Branch %d", i),
		}}
		g.Edges = append(g.Edges, &Edge{From: "parallel", To: branchID, Attrs: map[string]string{}})
		g.Edges = append(g.Edges, &Edge{From: branchID, To: "fanin", Attrs: map[string]string{}})
	}

	g.Edges = append(g.Edges, &Edge{From: "fanin", To: "exit", Attrs: map[string]string{}})
	return g
}

func TestExecuteParallelBranchesBasic(t *testing.T) {
	g := buildParallelGraph(3)

	pctx := NewContext()
	pctx.Set("_graph", g)
	pctx.Set("shared_key", "parent_value")
	store := NewArtifactStore("")

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return &Outcome{
				Status: StatusSuccess,
				ContextUpdates: map[string]any{
					"result_" + node.ID: "done",
				},
			}, nil
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for _, r := range results {
		if r.Error != nil {
			t.Errorf("branch %s had error: %v", r.NodeID, r.Error)
		}
		if r.Outcome == nil || r.Outcome.Status != StatusSuccess {
			t.Errorf("branch %s expected success outcome", r.NodeID)
		}
		if r.BranchContext == nil {
			t.Errorf("branch %s has nil context", r.NodeID)
		}
	}
}

func TestExecuteParallelBranchesContextIsolation(t *testing.T) {
	g := buildParallelGraph(2)

	pctx := NewContext()
	pctx.Set("_graph", g)
	pctx.Set("shared_key", "original")
	store := NewArtifactStore("")

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			// Each branch writes its own value to the same key
			pctx.Set("shared_key", node.ID+"_wrote_this")
			// Also verify we see the original value (not another branch's write)
			// Since Clone() happens before execution, each branch should see "original" initially
			return &Outcome{
				Status: StatusSuccess,
				ContextUpdates: map[string]any{
					"branch_" + node.ID + "_initial": pctx.GetString("shared_key", ""),
				},
			}, nil
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The parent context should still have the original value
	parentVal := pctx.GetString("shared_key", "")
	if parentVal != "original" {
		t.Errorf("parent context was mutated, expected 'original', got %q", parentVal)
	}

	// Each branch should have written its own value
	for _, r := range results {
		branchVal := r.BranchContext.GetString("shared_key", "")
		expected := r.NodeID + "_wrote_this"
		if branchVal != expected {
			t.Errorf("branch %s context expected %q, got %q", r.NodeID, expected, branchVal)
		}
	}

	// Branches should not see each other's writes
	if len(results) == 2 {
		val0 := results[0].BranchContext.GetString("shared_key", "")
		val1 := results[1].BranchContext.GetString("shared_key", "")
		if val0 == val1 {
			t.Errorf("branches should have isolated contexts, both got %q", val0)
		}
	}
}

func TestExecuteParallelBranchesSemaphoreLimitsConcurrency(t *testing.T) {
	g := buildParallelGraph(5)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	var currentConcurrency atomic.Int32
	var maxObservedConcurrency atomic.Int32

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			cur := currentConcurrency.Add(1)
			// Track maximum observed concurrency
			for {
				old := maxObservedConcurrency.Load()
				if cur <= old || maxObservedConcurrency.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			currentConcurrency.Add(-1)
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2", "branch_3", "branch_4"}
	config := ParallelConfig{
		MaxParallel: 2,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	maxConcur := maxObservedConcurrency.Load()
	if maxConcur > 2 {
		t.Errorf("max concurrency was %d, expected at most 2", maxConcur)
	}
	if maxConcur < 1 {
		t.Errorf("max concurrency was %d, expected at least 1", maxConcur)
	}
}

func TestExecuteParallelBranchesBranchFailure(t *testing.T) {
	g := buildParallelGraph(3)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "branch_1" {
				return &Outcome{Status: StatusFail, FailureReason: "branch_1 failed"}, nil
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All branches should have results, even the failed one
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Check that branch_1 has a fail outcome
	for _, r := range results {
		if r.NodeID == "branch_1" {
			if r.Outcome.Status != StatusFail {
				t.Errorf("branch_1 expected StatusFail, got %v", r.Outcome.Status)
			}
		} else {
			if r.Outcome.Status != StatusSuccess {
				t.Errorf("%s expected StatusSuccess, got %v", r.NodeID, r.Outcome.Status)
			}
		}
	}
}

func TestExecuteParallelBranchesBranchError(t *testing.T) {
	g := buildParallelGraph(3)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "branch_2" {
				return nil, fmt.Errorf("handler returned error for branch_2")
			}
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for _, r := range results {
		if r.NodeID == "branch_2" {
			if r.Error == nil {
				t.Error("branch_2 expected error, got nil")
			}
		}
	}
}

func TestExecuteParallelBranchesContextCancellation(t *testing.T) {
	g := buildParallelGraph(3)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	var started sync.WaitGroup
	started.Add(3)

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			started.Done()
			// Block until context is cancelled
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Run in goroutine so we can cancel
	type execResult struct {
		results []BranchResult
		err     error
	}
	ch := make(chan execResult, 1)
	go func() {
		results, err := ExecuteParallelBranches(ctx, g, pctx, store, reg, branches, config)
		ch <- execResult{results, err}
	}()

	// Wait for all branches to start, then cancel
	started.Wait()
	cancel()

	res := <-ch
	// Either the top-level returns an error, or all branches have errors
	if res.err != nil {
		// Top-level error from cancellation is acceptable
		return
	}

	// If no top-level error, all branches should have errors from cancellation
	for _, r := range res.results {
		if r.Error == nil && (r.Outcome == nil || r.Outcome.Status == StatusSuccess) {
			t.Errorf("branch %s should have error from cancellation", r.NodeID)
		}
	}
}

func TestMergeContextsWaitAll(t *testing.T) {
	parent := NewContext()
	parent.Set("parent_key", "parent_val")

	branch0Ctx := NewContext()
	branch0Ctx.Set("parent_key", "parent_val")
	branch0Ctx.Set("key_from_0", "value_0")

	branch1Ctx := NewContext()
	branch1Ctx.Set("parent_key", "parent_val")
	branch1Ctx.Set("key_from_1", "value_1")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch1Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Parent should have values from both branches
	if parent.GetString("key_from_0", "") != "value_0" {
		t.Error("expected key_from_0 in merged context")
	}
	if parent.GetString("key_from_1", "") != "value_1" {
		t.Error("expected key_from_1 in merged context")
	}
	// Original parent key should still be there
	if parent.GetString("parent_key", "") != "parent_val" {
		t.Error("expected parent_key preserved in merged context")
	}

	// parallel.results should be set
	resultsVal := parent.Get("parallel.results")
	if resultsVal == nil {
		t.Fatal("expected parallel.results to be set")
	}
	results, ok := resultsVal.([]BranchResult)
	if !ok {
		t.Fatalf("expected parallel.results to be []BranchResult, got %T", resultsVal)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results in parallel.results, got %d", len(results))
	}
}

func TestMergeContextsWaitAllFailure(t *testing.T) {
	parent := NewContext()

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "something broke"},
			BranchContext: NewContext(),
		},
	}

	err := MergeContexts(parent, branches, "wait_all")
	if err == nil {
		t.Fatal("expected error for wait_all with failed branch")
	}
}

func TestMergeContextsWaitAny(t *testing.T) {
	parent := NewContext()
	parent.Set("parent_key", "parent_val")

	branch0Ctx := NewContext()
	branch0Ctx.Set("key_from_0", "value_0")

	branch1Ctx := NewContext()
	branch1Ctx.Set("key_from_1", "value_1")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "branch_0 failed"},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch1Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only branch_1 succeeded, so only its context should be merged
	if parent.GetString("key_from_1", "") != "value_1" {
		t.Error("expected key_from_1 from the successful branch")
	}

	// parallel.results should be set
	resultsVal := parent.Get("parallel.results")
	if resultsVal == nil {
		t.Fatal("expected parallel.results to be set")
	}
}

func TestMergeContextsWaitAnyAllFailed(t *testing.T) {
	parent := NewContext()

	branches := []BranchResult{
		{
			NodeID:  "branch_0",
			Outcome: &Outcome{Status: StatusFail, FailureReason: "branch_0 failed"},
		},
		{
			NodeID:  "branch_1",
			Outcome: &Outcome{Status: StatusFail, FailureReason: "branch_1 failed"},
		},
	}

	err := MergeContexts(parent, branches, "wait_any")
	if err == nil {
		t.Fatal("expected error when all branches fail with wait_any")
	}
}

func TestMergeContextsLastWriteWins(t *testing.T) {
	parent := NewContext()
	parent.Set("conflict_key", "parent_original")

	branch0Ctx := NewContext()
	branch0Ctx.Set("conflict_key", "branch_0_value")

	branch1Ctx := NewContext()
	branch1Ctx.Set("conflict_key", "branch_1_value")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch1Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Last branch's value should win (last-write-wins)
	val := parent.GetString("conflict_key", "")
	if val != "branch_1_value" {
		t.Errorf("expected last-write-wins to produce 'branch_1_value', got %q", val)
	}
}

func TestParallelConfigFromContext(t *testing.T) {
	pctx := NewContext()
	pctx.Set("parallel.branches", []string{"a", "b", "c"})
	pctx.Set("parallel.join_policy", "wait_any")
	pctx.Set("parallel.error_policy", "fail_fast")
	pctx.Set("parallel.max_parallel", "8")

	config := ParallelConfigFromContext(pctx)

	if config.JoinPolicy != "wait_any" {
		t.Errorf("expected join_policy 'wait_any', got %q", config.JoinPolicy)
	}
	if config.ErrorPolicy != "fail_fast" {
		t.Errorf("expected error_policy 'fail_fast', got %q", config.ErrorPolicy)
	}
	if config.MaxParallel != 8 {
		t.Errorf("expected max_parallel 8, got %d", config.MaxParallel)
	}
}

func TestParallelConfigDefaults(t *testing.T) {
	pctx := NewContext()
	// No parallel values set

	config := ParallelConfigFromContext(pctx)

	if config.JoinPolicy != "wait_all" {
		t.Errorf("expected default join_policy 'wait_all', got %q", config.JoinPolicy)
	}
	if config.ErrorPolicy != "continue" {
		t.Errorf("expected default error_policy 'continue', got %q", config.ErrorPolicy)
	}
	if config.MaxParallel != 4 {
		t.Errorf("expected default max_parallel 4, got %d", config.MaxParallel)
	}
}

func TestExecuteParallelBranchesFollowsEdgesToFanIn(t *testing.T) {
	// Build a graph where each branch has a chain: branch_X -> step_X -> fanin
	g := &Graph{
		Name:         "chain_branches",
		Nodes:        make(map[string]*Node),
		Edges:        make([]*Edge, 0),
		Attrs:        make(map[string]string),
		NodeDefaults: make(map[string]string),
		EdgeDefaults: make(map[string]string),
	}
	g.Nodes["start"] = &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}
	g.Nodes["parallel"] = &Node{ID: "parallel", Attrs: map[string]string{"shape": "component"}}
	g.Nodes["branch_0"] = &Node{ID: "branch_0", Attrs: map[string]string{"shape": "box"}}
	g.Nodes["step_0"] = &Node{ID: "step_0", Attrs: map[string]string{"shape": "box"}}
	g.Nodes["branch_1"] = &Node{ID: "branch_1", Attrs: map[string]string{"shape": "box"}}
	g.Nodes["step_1"] = &Node{ID: "step_1", Attrs: map[string]string{"shape": "box"}}
	g.Nodes["fanin"] = &Node{ID: "fanin", Attrs: map[string]string{"shape": "tripleoctagon"}}
	g.Nodes["exit"] = &Node{ID: "exit", Attrs: map[string]string{"shape": "Msquare"}}

	g.Edges = append(g.Edges,
		&Edge{From: "start", To: "parallel", Attrs: map[string]string{}},
		&Edge{From: "parallel", To: "branch_0", Attrs: map[string]string{}},
		&Edge{From: "parallel", To: "branch_1", Attrs: map[string]string{}},
		&Edge{From: "branch_0", To: "step_0", Attrs: map[string]string{}},
		&Edge{From: "step_0", To: "fanin", Attrs: map[string]string{}},
		&Edge{From: "branch_1", To: "step_1", Attrs: map[string]string{}},
		&Edge{From: "step_1", To: "fanin", Attrs: map[string]string{}},
		&Edge{From: "fanin", To: "exit", Attrs: map[string]string{}},
	)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	var executedNodes sync.Map
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			executedNodes.Store(node.ID, true)
			return &Outcome{
				Status: StatusSuccess,
				ContextUpdates: map[string]any{
					"visited_" + node.ID: true,
				},
			}, nil
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify that intermediate steps were also executed
	for _, nodeID := range []string{"branch_0", "step_0", "branch_1", "step_1"} {
		if _, loaded := executedNodes.Load(nodeID); !loaded {
			t.Errorf("expected node %s to be executed", nodeID)
		}
	}

	// Verify fan-in node was NOT executed within parallel branches
	if _, loaded := executedNodes.Load("fanin"); loaded {
		t.Error("fanin should not be executed within parallel branches")
	}
}

func TestEngineParallelIntegration(t *testing.T) {
	g := buildParallelGraph(2)

	startH := newSuccessHandler("start")
	exitH := newSuccessHandler("exit")
	parallelH := &ParallelHandler{}
	faninH := &FanInHandler{}
	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return &Outcome{
				Status: StatusSuccess,
				ContextUpdates: map[string]any{
					"completed_" + node.ID: true,
				},
			}, nil
		},
	}
	reg := buildTestRegistry(startH, exitH, codergenH)
	reg.Register(parallelH)
	reg.Register(faninH)

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
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

	// Verify both branches were completed
	for _, branchID := range []string{"branch_0", "branch_1"} {
		val := result.Context.Get("completed_" + branchID)
		if val == nil || val != true {
			t.Errorf("expected completed_%s to be true in final context", branchID)
		}
	}

	// Verify parallel.results was populated
	results := result.Context.Get("parallel.results")
	if results == nil {
		t.Error("expected parallel.results to be set in final context")
	}

	// Verify fan-in was completed
	fanInCompleted := result.Context.Get("parallel.fan_in.completed")
	if fanInCompleted == nil || fanInCompleted != true {
		t.Error("expected parallel.fan_in.completed to be true")
	}
}

// --- Artifact Merging Tests ---

func TestMergeContextsArtifactConsolidation(t *testing.T) {
	parent := NewContext()
	store := NewArtifactStore("")

	// Each branch stores an artifact in the shared store
	branch0Ctx := NewContext()
	branch0Ctx.Set("artifact_id_0", "artifact_branch_0")
	store.Store("artifact_branch_0", "output_0.txt", []byte("output from branch 0"))

	branch1Ctx := NewContext()
	branch1Ctx.Set("artifact_id_1", "artifact_branch_1")
	store.Store("artifact_branch_1", "output_1.txt", []byte("output from branch 1"))

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch1Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify that the parent context has a merged artifact manifest
	manifestVal := parent.Get("parallel.artifacts")
	if manifestVal == nil {
		t.Fatal("expected parallel.artifacts to be set in merged context")
	}
	manifest, ok := manifestVal.(map[string][]string)
	if !ok {
		t.Fatalf("expected parallel.artifacts to be map[string][]string, got %T", manifestVal)
	}

	// Each branch should have its artifact IDs listed
	if ids, ok := manifest["branch_0"]; !ok || len(ids) == 0 {
		t.Error("expected branch_0 to have artifact IDs in manifest")
	} else {
		found := false
		for _, id := range ids {
			if id == "artifact_branch_0" {
				found = true
			}
		}
		if !found {
			t.Error("expected artifact_branch_0 in branch_0 manifest")
		}
	}

	if ids, ok := manifest["branch_1"]; !ok || len(ids) == 0 {
		t.Error("expected branch_1 to have artifact IDs in manifest")
	} else {
		found := false
		for _, id := range ids {
			if id == "artifact_branch_1" {
				found = true
			}
		}
		if !found {
			t.Error("expected artifact_branch_1 in branch_1 manifest")
		}
	}
}

func TestMergeContextsArtifactConsolidationNoArtifacts(t *testing.T) {
	parent := NewContext()

	branch0Ctx := NewContext()
	branch0Ctx.Set("some_key", "some_value")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// parallel.artifacts should still be set (empty manifest is fine)
	manifestVal := parent.Get("parallel.artifacts")
	if manifestVal == nil {
		t.Fatal("expected parallel.artifacts to be set even with no artifacts")
	}
	manifest, ok := manifestVal.(map[string][]string)
	if !ok {
		t.Fatalf("expected parallel.artifacts to be map[string][]string, got %T", manifestVal)
	}
	if ids, ok := manifest["branch_0"]; ok && len(ids) > 0 {
		t.Error("expected branch_0 to have no artifact IDs")
	}
}

func TestMergeContextsArtifactConsolidationWaitAny(t *testing.T) {
	parent := NewContext()

	// branch_0 fails, branch_1 succeeds with artifact
	branch0Ctx := NewContext()
	branch0Ctx.Set("artifact_id_0", "artifact_fail")

	branch1Ctx := NewContext()
	branch1Ctx.Set("artifact_id_1", "artifact_success")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "branch_0 failed"},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch1Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only successful branch artifacts should appear in manifest
	manifestVal := parent.Get("parallel.artifacts")
	if manifestVal == nil {
		t.Fatal("expected parallel.artifacts to be set")
	}
	manifest, ok := manifestVal.(map[string][]string)
	if !ok {
		t.Fatalf("expected parallel.artifacts to be map[string][]string, got %T", manifestVal)
	}

	// branch_0 failed, should not appear
	if ids, ok := manifest["branch_0"]; ok && len(ids) > 0 {
		t.Error("failed branch_0 should not have artifacts in manifest")
	}
}

// --- Merge Logging Tests ---

func TestMergeContextsLogsValueMerges(t *testing.T) {
	parent := NewContext()
	parent.Set("parent_key", "parent_val")

	branch0Ctx := NewContext()
	branch0Ctx.Set("key_from_0", "value_0")

	branch1Ctx := NewContext()
	branch1Ctx.Set("key_from_1", "value_1")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch1Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logs := parent.Logs()
	if len(logs) == 0 {
		t.Fatal("expected merge logs to be present")
	}

	// Should log which branches were merged
	foundBranch0 := false
	foundBranch1 := false
	for _, log := range logs {
		if strings.Contains(log, "branch_0") {
			foundBranch0 = true
		}
		if strings.Contains(log, "branch_1") {
			foundBranch1 = true
		}
	}
	if !foundBranch0 {
		t.Error("expected log entry mentioning branch_0")
	}
	if !foundBranch1 {
		t.Error("expected log entry mentioning branch_1")
	}
}

func TestMergeContextsLogsConflictResolution(t *testing.T) {
	parent := NewContext()
	parent.Set("conflict_key", "parent_original")

	branch0Ctx := NewContext()
	branch0Ctx.Set("conflict_key", "branch_0_value")

	branch1Ctx := NewContext()
	branch1Ctx.Set("conflict_key", "branch_1_value")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch1Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logs := parent.Logs()
	// Should have a log entry about the conflict on "conflict_key"
	foundConflict := false
	for _, log := range logs {
		if strings.Contains(log, "conflict_key") && strings.Contains(log, "last-write-wins") {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Errorf("expected log entry about conflict resolution for 'conflict_key', got logs: %v", logs)
	}
}

func TestMergeContextsLogsMergeSummary(t *testing.T) {
	parent := NewContext()

	branch0Ctx := NewContext()
	branch0Ctx.Set("key_a", "val_a")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
	}

	err := MergeContexts(parent, branches, "wait_all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logs := parent.Logs()
	// Should have a summary log about the merge operation
	foundSummary := false
	for _, log := range logs {
		if strings.Contains(log, "merge") && strings.Contains(log, "wait_all") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Errorf("expected summary log entry about merge operation, got logs: %v", logs)
	}
}

// --- k_of_n Join Policy Tests ---

func TestMergeContextsKOfNSuccess(t *testing.T) {
	parent := NewContext()
	parent.Set("parallel.k_required", "2")

	branch0Ctx := NewContext()
	branch0Ctx.Set("key_from_0", "value_0")

	branch1Ctx := NewContext()
	branch1Ctx.Set("key_from_1", "value_1")

	branch2Ctx := NewContext()
	branch2Ctx.Set("key_from_2", "value_2")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "branch_1 failed"},
			BranchContext: branch1Ctx,
		},
		{
			NodeID:        "branch_2",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch2Ctx,
		},
	}

	err := MergeContexts(parent, branches, "k_of_n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only successful branches should be merged
	if parent.GetString("key_from_0", "") != "value_0" {
		t.Error("expected key_from_0 from successful branch")
	}
	if parent.GetString("key_from_2", "") != "value_2" {
		t.Error("expected key_from_2 from successful branch")
	}
}

func TestMergeContextsKOfNInsufficientSuccesses(t *testing.T) {
	parent := NewContext()
	parent.Set("parallel.k_required", "3")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed"},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_2",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_3",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "also failed"},
			BranchContext: NewContext(),
		},
	}

	err := MergeContexts(parent, branches, "k_of_n")
	if err == nil {
		t.Fatal("expected error when fewer than K branches succeed")
	}
	if !strings.Contains(err.Error(), "2") || !strings.Contains(err.Error(), "3") {
		t.Errorf("expected error to mention actual (2) and required (3) counts, got: %v", err)
	}
}

func TestMergeContextsKOfNDefaultsToAll(t *testing.T) {
	// When k_required is not set, k_of_n defaults to requiring all branches
	parent := NewContext()

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed"},
			BranchContext: NewContext(),
		},
	}

	err := MergeContexts(parent, branches, "k_of_n")
	if err == nil {
		t.Fatal("expected error when k_required defaults to total and a branch fails")
	}
}

func TestMergeContextsKOfNErrorBranches(t *testing.T) {
	parent := NewContext()
	parent.Set("parallel.k_required", "1")

	branches := []BranchResult{
		{
			NodeID: "branch_0",
			Error:  fmt.Errorf("branch_0 crashed"),
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
	}

	// k=1, one succeeded, should pass
	err := MergeContexts(parent, branches, "k_of_n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Quorum Join Policy Tests ---

func TestMergeContextsQuorumSuccess(t *testing.T) {
	parent := NewContext()

	branch0Ctx := NewContext()
	branch0Ctx.Set("key_from_0", "value_0")

	branch1Ctx := NewContext()
	branch1Ctx.Set("key_from_1", "value_1")

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch0Ctx,
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: branch1Ctx,
		},
		{
			NodeID:        "branch_2",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed"},
			BranchContext: NewContext(),
		},
	}

	// 2 of 3 succeed = majority
	err := MergeContexts(parent, branches, "quorum")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both successful branches should be merged
	if parent.GetString("key_from_0", "") != "value_0" {
		t.Error("expected key_from_0 from successful branch")
	}
	if parent.GetString("key_from_1", "") != "value_1" {
		t.Error("expected key_from_1 from successful branch")
	}
}

func TestMergeContextsQuorumFailure(t *testing.T) {
	parent := NewContext()

	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed 1"},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_2",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed 2"},
			BranchContext: NewContext(),
		},
	}

	// 1 of 3 succeed = no majority
	err := MergeContexts(parent, branches, "quorum")
	if err == nil {
		t.Fatal("expected error when quorum not met")
	}
	if !strings.Contains(err.Error(), "quorum") {
		t.Errorf("expected error message to mention quorum, got: %v", err)
	}
}

func TestMergeContextsQuorumExactMajority(t *testing.T) {
	parent := NewContext()

	// 2 of 4 = exactly 50%, NOT a majority (need >50%)
	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_2",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed"},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_3",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed"},
			BranchContext: NewContext(),
		},
	}

	// 2 of 4 is exactly 50%, which is NOT majority (need >50%, i.e. 3 of 4)
	err := MergeContexts(parent, branches, "quorum")
	if err == nil {
		t.Fatal("expected error when exactly 50% (not a strict majority)")
	}
}

func TestMergeContextsQuorumThreeOfFive(t *testing.T) {
	parent := NewContext()

	// 3 of 5 = 60% = majority
	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_1",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_2",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_3",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed"},
			BranchContext: NewContext(),
		},
		{
			NodeID:        "branch_4",
			Outcome:       &Outcome{Status: StatusFail, FailureReason: "failed"},
			BranchContext: NewContext(),
		},
	}

	err := MergeContexts(parent, branches, "quorum")
	if err != nil {
		t.Fatalf("unexpected error with 3 of 5 quorum: %v", err)
	}
}

func TestMergeContextsQuorumWithErrors(t *testing.T) {
	parent := NewContext()

	// Branches with errors count as failures for quorum
	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
		{
			NodeID: "branch_1",
			Error:  fmt.Errorf("branch crashed"),
		},
		{
			NodeID:        "branch_2",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
	}

	// 2 of 3 succeed (error branch is a failure) = majority
	err := MergeContexts(parent, branches, "quorum")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- fail_fast Error Policy Tests ---

func TestExecuteParallelBranchesFailFastCancelsRemaining(t *testing.T) {
	g := buildParallelGraph(3)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	var executionOrder sync.Map
	var executionCount atomic.Int32

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			idx := executionCount.Add(1)
			executionOrder.Store(node.ID, idx)

			if node.ID == "branch_0" {
				// Small delay then fail
				time.Sleep(10 * time.Millisecond)
				return nil, fmt.Errorf("branch_0 exploded")
			}
			// Other branches should block until context is cancelled
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return &Outcome{Status: StatusSuccess}, nil
			}
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "fail_fast",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	// fail_fast should still return results (not a top-level error) so we can inspect them
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// branch_0 should have failed with an error
	foundFailure := false
	for _, r := range results {
		if r.NodeID == "branch_0" && r.Error != nil {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Error("expected branch_0 to have an error")
	}

	// Other branches should have been cancelled (context error)
	cancelledCount := 0
	for _, r := range results {
		if r.NodeID != "branch_0" && r.Error != nil {
			cancelledCount++
		}
	}
	if cancelledCount == 0 {
		t.Error("expected at least one other branch to be cancelled by fail_fast")
	}
}

func TestExecuteParallelBranchesFailFastWithOutcomeFailure(t *testing.T) {
	g := buildParallelGraph(3)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "branch_1" {
				time.Sleep(10 * time.Millisecond)
				return &Outcome{Status: StatusFail, FailureReason: "branch_1 failed via outcome"}, nil
			}
			// Block until cancelled
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return &Outcome{Status: StatusSuccess}, nil
			}
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "fail_fast",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}

	// branch_1 should have a fail outcome
	for _, r := range results {
		if r.NodeID == "branch_1" {
			if r.Outcome == nil || r.Outcome.Status != StatusFail {
				t.Error("expected branch_1 to have StatusFail outcome")
			}
		}
	}
}

func TestExecuteParallelBranchesFailFastAllSucceed(t *testing.T) {
	g := buildParallelGraph(3)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			return &Outcome{Status: StatusSuccess, ContextUpdates: map[string]any{"done_" + node.ID: true}}, nil
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "fail_fast",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// All branches should succeed when no failures occur
	for _, r := range results {
		if r.Error != nil {
			t.Errorf("branch %s had unexpected error: %v", r.NodeID, r.Error)
		}
		if r.Outcome == nil || r.Outcome.Status != StatusSuccess {
			t.Errorf("branch %s expected success", r.NodeID)
		}
	}
}

func TestExecuteParallelBranchesContinuePolicyDoesNotCancel(t *testing.T) {
	g := buildParallelGraph(3)

	pctx := NewContext()
	pctx.Set("_graph", g)
	store := NewArtifactStore("")

	codergenH := &testHandler{
		typeName: "codergen",
		executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
			if node.ID == "branch_0" {
				return nil, fmt.Errorf("branch_0 failed")
			}
			// Other branches should complete normally (not cancelled)
			time.Sleep(50 * time.Millisecond)
			return &Outcome{Status: StatusSuccess}, nil
		},
	}
	reg := buildTestRegistry(codergenH)

	branches := []string{"branch_0", "branch_1", "branch_2"}
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	results, err := ExecuteParallelBranches(context.Background(), g, pctx, store, reg, branches, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// branch_0 should have error, but branch_1 and branch_2 should succeed
	successCount := 0
	for _, r := range results {
		if r.Error == nil && r.Outcome != nil && r.Outcome.Status == StatusSuccess {
			successCount++
		}
	}
	if successCount != 2 {
		t.Errorf("expected 2 successful branches with continue policy, got %d", successCount)
	}
}

// --- ParallelConfig with K field Tests ---

func TestParallelConfigFromContextWithK(t *testing.T) {
	pctx := NewContext()
	pctx.Set("parallel.join_policy", "k_of_n")
	pctx.Set("parallel.k_required", "3")

	config := ParallelConfigFromContext(pctx)

	if config.JoinPolicy != "k_of_n" {
		t.Errorf("expected join_policy 'k_of_n', got %q", config.JoinPolicy)
	}
	if config.KRequired != 3 {
		t.Errorf("expected k_required 3, got %d", config.KRequired)
	}
}

func TestParallelConfigFromContextKDefaultsToZero(t *testing.T) {
	pctx := NewContext()
	config := ParallelConfigFromContext(pctx)

	if config.KRequired != 0 {
		t.Errorf("expected default k_required 0, got %d", config.KRequired)
	}
}

// --- Unknown Policy Tests ---

func TestMergeContextsUnknownPolicy(t *testing.T) {
	parent := NewContext()
	branches := []BranchResult{
		{
			NodeID:        "branch_0",
			Outcome:       &Outcome{Status: StatusSuccess},
			BranchContext: NewContext(),
		},
	}

	err := MergeContexts(parent, branches, "nonexistent_policy")
	if err == nil {
		t.Fatal("expected error for unknown policy")
	}
	if !strings.Contains(err.Error(), "nonexistent_policy") {
		t.Errorf("expected error to mention the unknown policy name, got: %v", err)
	}
}
