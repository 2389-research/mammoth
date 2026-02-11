// ABOUTME: End-to-end integration smoke tests exercising the full pipeline lifecycle.
// ABOUTME: Covers parse -> validate -> execute -> edge selection -> goal gate -> checkpoint -> complete.
package attractor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// testCodergenBackend is a test double implementing CodergenBackend that returns
// pre-configured responses in sequence, then falls back to a default success.
type testCodergenBackend struct {
	mu        sync.Mutex
	responses []AgentRunResult
	callCount int
	calls     []AgentRunConfig
}

func (b *testCodergenBackend) RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, config)
	if b.callCount < len(b.responses) {
		result := b.responses[b.callCount]
		b.callCount++
		return &result, nil
	}
	b.callCount++
	return &AgentRunResult{Output: "default", Success: true}, nil
}

// --- Test 1: Simple 3-node pipeline ---

func TestIntegrationSimplePipeline(t *testing.T) {
	source := `digraph test {
		graph [goal="Test pipeline"]
		start [shape=Mdiamond]
		work [label="Do work", prompt="Execute task for: $goal"]
		done [shape=Msquare]
		start -> work -> done
	}`

	backend := &testCodergenBackend{
		responses: []AgentRunResult{
			{Output: "work completed", ToolCalls: 2, TokensUsed: 100, Success: true},
		},
	}

	engine := NewEngine(EngineConfig{
		Backend:      backend,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all 3 nodes completed
	if len(result.CompletedNodes) != 3 {
		t.Errorf("expected 3 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}

	// Verify completion order: start, work, done
	expectedOrder := []string{"start", "work", "done"}
	for i, expected := range expectedOrder {
		if i >= len(result.CompletedNodes) {
			t.Errorf("missing completed node at index %d: expected %q", i, expected)
			continue
		}
		if result.CompletedNodes[i] != expected {
			t.Errorf("completed node at index %d: expected %q, got %q", i, expected, result.CompletedNodes[i])
		}
	}

	// Verify final outcome is success
	if result.FinalOutcome == nil {
		t.Fatal("expected non-nil final outcome")
	}
	if result.FinalOutcome.Status != StatusSuccess {
		t.Errorf("expected final status success, got %v", result.FinalOutcome.Status)
	}

	// Verify context has goal from graph attrs
	goal := result.Context.GetString("goal", "")
	if goal != "Test pipeline" {
		t.Errorf("expected context goal = 'Test pipeline', got %q", goal)
	}

	// Verify the prompt was expanded with $goal
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.calls) != 1 {
		t.Fatalf("expected 1 backend call, got %d", len(backend.calls))
	}
	if !strings.Contains(backend.calls[0].Prompt, "Test pipeline") {
		t.Errorf("expected prompt to contain expanded goal, got %q", backend.calls[0].Prompt)
	}
}

// --- Test 2: Conditional branching ---

func TestIntegrationConditionalBranching(t *testing.T) {
	// Uses a codergen node (box) as the branch point. The codergen handler's
	// outcome directly controls edge selection via the "outcome" condition key.
	// Flow when work fails: start -> work(fail) -> retry -> work(success) -> done
	source := `digraph test {
		graph [goal="Test branching"]
		start [shape=Mdiamond]
		work [prompt="Do work"]
		done [shape=Msquare]
		retry [prompt="Fix it"]
		start -> work
		work -> done [condition="outcome=success"]
		work -> retry [condition="outcome=fail"]
		retry -> work
	}`

	backend := &testCodergenBackend{
		responses: []AgentRunResult{
			// First call (work node, attempt 1): fails
			{Output: "work attempt 1 - failed", Success: false},
			// Second call (retry node): succeeds
			{Output: "retry fixed it", Success: true},
			// Third call (work node, attempt 2): succeeds
			{Output: "work attempt 2 - succeeded", Success: true},
		},
	}

	engine := NewEngine(EngineConfig{
		Backend:      backend,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify retry was visited
	foundRetry := false
	for _, n := range result.CompletedNodes {
		if n == "retry" {
			foundRetry = true
		}
	}
	if !foundRetry {
		t.Errorf("expected retry in completed nodes, got: %v", result.CompletedNodes)
	}

	// Verify done was reached
	foundDone := false
	for _, n := range result.CompletedNodes {
		if n == "done" {
			foundDone = true
		}
	}
	if !foundDone {
		t.Errorf("expected done in completed nodes, got: %v", result.CompletedNodes)
	}

	// Verify work was visited twice
	workCount := 0
	for _, n := range result.CompletedNodes {
		if n == "work" {
			workCount++
		}
	}
	if workCount != 2 {
		t.Errorf("expected work visited 2 times, got %d (nodes: %v)", workCount, result.CompletedNodes)
	}

	// Final outcome should be success
	if result.FinalOutcome == nil || result.FinalOutcome.Status != StatusSuccess {
		status := "nil"
		if result.FinalOutcome != nil {
			status = string(result.FinalOutcome.Status)
		}
		t.Errorf("expected final status success, got %s", status)
	}
}

// --- Test 3: Goal gate enforcement ---

func TestIntegrationGoalGate(t *testing.T) {
	source := `digraph test {
		graph [goal="Test goal gate"]
		start [shape=Mdiamond]
		work [prompt="Do work", goal_gate="true", retry_target="work"]
		done [shape=Msquare]
		start -> work -> done
	}`

	backend := &testCodergenBackend{
		responses: []AgentRunResult{
			// First call: fails (goal gate will not be satisfied)
			{Output: "unsatisfying result", Success: false},
			// Second call after goal gate retry: succeeds
			{Output: "satisfying result", Success: true},
		},
	}

	engine := NewEngine(EngineConfig{
		Backend:      backend,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The work node should have been visited at least twice due to goal gate retry
	workCount := 0
	for _, n := range result.CompletedNodes {
		if n == "work" {
			workCount++
		}
	}
	if workCount < 2 {
		t.Errorf("expected work visited at least 2 times due to goal gate retry, got %d (nodes: %v)", workCount, result.CompletedNodes)
	}

	// Final outcome should be success
	if result.FinalOutcome == nil || result.FinalOutcome.Status != StatusSuccess {
		status := "nil"
		if result.FinalOutcome != nil {
			status = string(result.FinalOutcome.Status)
		}
		t.Errorf("expected final status success, got %s", status)
	}

	// Backend should have been called at least twice
	backend.mu.Lock()
	calls := backend.callCount
	backend.mu.Unlock()
	if calls < 2 {
		t.Errorf("expected at least 2 backend calls, got %d", calls)
	}
}

// --- Test 4: Human gate ---

func TestIntegrationHumanGate(t *testing.T) {
	source := `digraph test {
		graph [goal="Test human gate"]
		start [shape=Mdiamond]
		review [shape=hexagon, label="Approve deployment?"]
		deploy [prompt="Deploy the app"]
		done [shape=Msquare]
		start -> review
		review -> deploy [label="[Y] Yes"]
		review -> done [label="[N] No"]
		deploy -> done
	}`

	// Parse the graph so we can inject it for the human handler
	graph, err := Parse(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Apply transforms (the engine normally does this)
	transforms := DefaultTransforms()
	graph = ApplyTransforms(graph, transforms...)

	// Create an auto-approve interviewer
	interviewer := NewAutoApproveInterviewer("[Y] Yes")

	// Build a registry with a human handler that injects the graph reference
	registry := DefaultHandlerRegistry()
	registry.Register(&graphInjectingHumanHandler{
		inner: &WaitForHumanHandler{Interviewer: interviewer},
		graph: graph,
	})

	backend := &testCodergenBackend{
		responses: []AgentRunResult{
			{Output: "deployed!", Success: true},
		},
	}

	engine := NewEngine(EngineConfig{
		Backend:      backend,
		Handlers:     registry,
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.RunGraph(context.Background(), graph)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have completed: start -> review -> deploy -> done
	if len(result.CompletedNodes) < 3 {
		t.Errorf("expected at least 3 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}

	// Verify the human gate was visited
	foundReview := false
	for _, n := range result.CompletedNodes {
		if n == "review" {
			foundReview = true
		}
	}
	if !foundReview {
		t.Errorf("expected review in completed nodes, got: %v", result.CompletedNodes)
	}

	// Verify the pipeline continued after the human gate
	foundDone := false
	for _, n := range result.CompletedNodes {
		if n == "done" {
			foundDone = true
		}
	}
	if !foundDone {
		t.Errorf("expected done in completed nodes, got: %v", result.CompletedNodes)
	}
}

// graphInjectingHumanHandler wraps a WaitForHumanHandler and injects the graph
// into the pipeline context before delegating, working around the engine not
// setting _graph in context.
type graphInjectingHumanHandler struct {
	inner *WaitForHumanHandler
	graph *Graph
}

func (h *graphInjectingHumanHandler) Type() string {
	return "wait.human"
}

func (h *graphInjectingHumanHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	pctx.Set("_graph", h.graph)
	return h.inner.Execute(ctx, node, pctx, store)
}

// --- Test 5: Tool node ---

func TestIntegrationToolNode(t *testing.T) {
	source := `digraph test {
		graph [goal="Test tool node"]
		start [shape=Mdiamond]
		run_cmd [shape=parallelogram, command="echo hello_from_tool"]
		done [shape=Msquare]
		start -> run_cmd -> done
	}`

	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		DefaultRetry: RetryPolicyNone(),
	})

	result, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 3 nodes should be completed
	if len(result.CompletedNodes) != 3 {
		t.Errorf("expected 3 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}

	// Verify the tool output was captured in context
	stdout := result.Context.GetString("tool.stdout", "")
	if !strings.Contains(stdout, "hello_from_tool") {
		t.Errorf("expected tool.stdout to contain 'hello_from_tool', got %q", stdout)
	}

	// Verify the exit code was recorded
	exitCodeVal := result.Context.Get("tool.exit_code")
	if exitCodeVal == nil {
		t.Error("expected tool.exit_code in context")
	}
}

// --- Test 6: Checkpointing ---

func TestIntegrationCheckpointing(t *testing.T) {
	cpDir := t.TempDir()

	source := `digraph test {
		graph [goal="Test checkpointing"]
		start [shape=Mdiamond]
		step1 [prompt="Step 1"]
		step2 [prompt="Step 2"]
		done [shape=Msquare]
		start -> step1 -> step2 -> done
	}`

	backend := &testCodergenBackend{
		responses: []AgentRunResult{
			{Output: "step1 done", Success: true},
			{Output: "step2 done", Success: true},
		},
	}

	engine := NewEngine(EngineConfig{
		Backend:       backend,
		CheckpointDir: cpDir,
		DefaultRetry:  RetryPolicyNone(),
	})

	result, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 4 nodes completed
	if len(result.CompletedNodes) != 4 {
		t.Errorf("expected 4 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}

	// Verify checkpoint files were created
	entries, err := os.ReadDir(cpDir)
	if err != nil {
		t.Fatalf("error reading checkpoint dir: %v", err)
	}
	// Engine saves a checkpoint after each non-terminal node: start, step1, step2 = 3 checkpoints
	if len(entries) < 3 {
		t.Errorf("expected at least 3 checkpoint files, got %d", len(entries))
	}

	// Load and verify the last checkpoint contains all expected completed nodes
	var latestCheckpoint *Checkpoint
	for _, entry := range entries {
		cp, err := LoadCheckpoint(filepath.Join(cpDir, entry.Name()))
		if err != nil {
			t.Errorf("failed to load checkpoint %q: %v", entry.Name(), err)
			continue
		}
		if latestCheckpoint == nil || len(cp.CompletedNodes) > len(latestCheckpoint.CompletedNodes) {
			latestCheckpoint = cp
		}
	}

	if latestCheckpoint == nil {
		t.Fatal("no valid checkpoints found")
	}

	// The latest checkpoint should contain the most completed nodes
	if len(latestCheckpoint.CompletedNodes) < 3 {
		t.Errorf("expected latest checkpoint to have at least 3 completed nodes, got %d: %v",
			len(latestCheckpoint.CompletedNodes), latestCheckpoint.CompletedNodes)
	}

	// Verify checkpoint has context values including the goal
	cpData, err := json.Marshal(latestCheckpoint.ContextValues)
	if err != nil {
		t.Fatalf("failed to marshal checkpoint context: %v", err)
	}
	if !strings.Contains(string(cpData), "Test checkpointing") {
		t.Errorf("expected checkpoint context to contain goal, got %s", string(cpData))
	}
}

// --- Test 7: Event emission ---

func TestIntegrationEventEmission(t *testing.T) {
	source := `digraph test {
		graph [goal="Test events"]
		start [shape=Mdiamond]
		work [prompt="Do work"]
		done [shape=Msquare]
		start -> work -> done
	}`

	var mu sync.Mutex
	var events []EngineEvent

	backend := &testCodergenBackend{
		responses: []AgentRunResult{
			{Output: "work done", Success: true},
		},
	}

	engine := NewEngine(EngineConfig{
		Backend:      backend,
		DefaultRetry: RetryPolicyNone(),
		EventHandler: func(evt EngineEvent) {
			mu.Lock()
			events = append(events, evt)
			mu.Unlock()
		},
	})

	_, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Verify we got events
	if len(events) == 0 {
		t.Fatal("expected at least some events")
	}

	// First event should be pipeline.started
	if events[0].Type != EventPipelineStarted {
		t.Errorf("expected first event to be pipeline.started, got %v", events[0].Type)
	}

	// Last event should be pipeline.completed
	if events[len(events)-1].Type != EventPipelineCompleted {
		t.Errorf("expected last event to be pipeline.completed, got %v", events[len(events)-1].Type)
	}

	// Collect event type counts
	eventTypes := make(map[EngineEventType]int)
	for _, evt := range events {
		eventTypes[evt.Type]++
	}

	if eventTypes[EventStageStarted] == 0 {
		t.Error("expected at least one stage.started event")
	}
	if eventTypes[EventStageCompleted] == 0 {
		t.Error("expected at least one stage.completed event")
	}

	// Verify stage events reference node IDs
	for _, evt := range events {
		if evt.Type == EventStageStarted || evt.Type == EventStageCompleted {
			if evt.NodeID == "" {
				t.Errorf("stage event %v has empty NodeID", evt.Type)
			}
		}
	}

	// Verify specific node events exist
	stageStartedNodes := make(map[string]bool)
	stageCompletedNodes := make(map[string]bool)
	for _, evt := range events {
		if evt.Type == EventStageStarted {
			stageStartedNodes[evt.NodeID] = true
		}
		if evt.Type == EventStageCompleted {
			stageCompletedNodes[evt.NodeID] = true
		}
	}

	for _, nodeID := range []string{"start", "work", "done"} {
		if !stageStartedNodes[nodeID] {
			t.Errorf("expected stage.started event for node %q", nodeID)
		}
		if !stageCompletedNodes[nodeID] {
			t.Errorf("expected stage.completed event for node %q", nodeID)
		}
	}
}

// --- Test: Parse real example files ---

func TestIntegrationParseExampleFiles(t *testing.T) {
	exampleDir := filepath.Join("..", "examples")
	entries, err := os.ReadDir(exampleDir)
	if err != nil {
		exampleDir = "examples"
		entries, err = os.ReadDir(exampleDir)
		if err != nil {
			t.Skipf("examples directory not found, skipping: %v", err)
		}
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".dot") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(exampleDir, entry.Name()))
			if err != nil {
				t.Fatalf("failed to read %s: %v", entry.Name(), err)
			}
			graph, err := Parse(string(data))
			if err != nil {
				t.Fatalf("failed to parse %s: %v", entry.Name(), err)
			}
			if graph == nil {
				t.Fatal("parsed graph is nil")
			}
			if len(graph.Nodes) == 0 {
				t.Error("parsed graph has no nodes")
			}
		})
	}
}

// --- Test: Full pipeline with events and checkpoints combined ---

func TestIntegrationFullPipelineWithEventsAndCheckpoints(t *testing.T) {
	cpDir := t.TempDir()

	source := `digraph test {
		graph [goal="Full integration"]
		start [shape=Mdiamond]
		analyze [prompt="Analyze requirements"]
		build [prompt="Build implementation"]
		done [shape=Msquare]
		start -> analyze -> build -> done
	}`

	var mu sync.Mutex
	var events []EngineEvent
	checkpointSaved := false

	backend := &testCodergenBackend{
		responses: []AgentRunResult{
			{Output: "requirements analyzed", ToolCalls: 1, TokensUsed: 50, Success: true},
			{Output: "implementation built", ToolCalls: 5, TokensUsed: 500, Success: true},
		},
	}

	engine := NewEngine(EngineConfig{
		Backend:       backend,
		CheckpointDir: cpDir,
		DefaultRetry:  RetryPolicyNone(),
		EventHandler: func(evt EngineEvent) {
			mu.Lock()
			events = append(events, evt)
			if evt.Type == EventCheckpointSaved {
				checkpointSaved = true
			}
			mu.Unlock()
		},
	})

	result, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all nodes completed
	if len(result.CompletedNodes) != 4 {
		t.Errorf("expected 4 completed nodes, got %d: %v", len(result.CompletedNodes), result.CompletedNodes)
	}

	// Verify checkpoint events were emitted
	mu.Lock()
	cpSaved := checkpointSaved
	mu.Unlock()
	if !cpSaved {
		t.Error("expected checkpoint.saved event to be emitted")
	}

	// Verify context contains expected values
	goal := result.Context.GetString("goal", "")
	if goal != "Full integration" {
		t.Errorf("expected goal = 'Full integration', got %q", goal)
	}

	// Verify the backend received the correct goal
	backend.mu.Lock()
	defer backend.mu.Unlock()
	for _, call := range backend.calls {
		if call.Goal != "Full integration" {
			t.Errorf("expected backend call goal = 'Full integration', got %q", call.Goal)
		}
	}
}

// --- Test: Plan -> Implement -> Review -> Done (DoD 11.13) ---

// artifactWritingBackend wraps a testCodergenBackend and writes per-node artifacts
// (prompt.md, response.md, status.json) to a RunDirectory on each call.
type artifactWritingBackend struct {
	inner *testCodergenBackend
	rd    *RunDirectory
}

func (b *artifactWritingBackend) RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
	// Delegate to the inner backend for the actual agent result
	result, err := b.inner.RunAgent(ctx, config)
	if err != nil {
		return nil, err
	}

	// Write artifacts to the RunDirectory for this node
	nodeID := config.NodeID
	if nodeID == "" {
		return result, nil
	}

	if writeErr := b.rd.WritePrompt(nodeID, config.Prompt); writeErr != nil {
		return nil, writeErr
	}
	if writeErr := b.rd.WriteResponse(nodeID, result.Output); writeErr != nil {
		return nil, writeErr
	}

	// Write status.json with the outcome
	statusJSON := `{"success":` + boolStr(result.Success) + `,"tool_calls":` + intStr(result.ToolCalls) + `,"tokens_used":` + intStr(result.TokensUsed) + `}`
	if writeErr := b.rd.WriteNodeArtifact(nodeID, "status.json", []byte(statusJSON)); writeErr != nil {
		return nil, writeErr
	}

	return result, nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func intStr(n int) string {
	return fmt.Sprintf("%d", n)
}

func TestIntegrationPlanImplementReview(t *testing.T) {
	// Load the pipeline DOT file from the examples directory
	exampleDir := filepath.Join("..", "examples")
	dotPath := filepath.Join(exampleDir, "plan_implement_review.dot")
	dotData, err := os.ReadFile(dotPath)
	if err != nil {
		// Try alternate relative path
		dotPath = filepath.Join("examples", "plan_implement_review.dot")
		dotData, err = os.ReadFile(dotPath)
		if err != nil {
			t.Fatalf("failed to read plan_implement_review.dot: %v", err)
		}
	}
	source := string(dotData)

	// --- Step 1: Parse ---
	graph, err := Parse(source)
	if err != nil {
		t.Fatalf("failed to parse pipeline: %v", err)
	}
	if graph.Attrs["goal"] != "Create a hello world Python script" {
		t.Errorf("expected goal = 'Create a hello world Python script', got %q", graph.Attrs["goal"])
	}
	if len(graph.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 6 {
		t.Errorf("expected 6 edges, got %d", len(graph.Edges))
	}

	// --- Step 2: Validate ---
	transforms := DefaultTransforms()
	transformed := ApplyTransforms(graph, transforms...)
	results, err := ValidateOrError(transformed)
	if err != nil {
		t.Fatalf("validation failed: %v (results: %v)", err, results)
	}

	// --- Step 3: Set up RunDirectory for artifact storage ---
	baseDir := t.TempDir()
	rd, err := NewRunDirectory(baseDir, "smoke-test-run")
	if err != nil {
		t.Fatalf("failed to create run directory: %v", err)
	}

	// Set up checkpoint directory
	cpDir := t.TempDir()

	// Configure backend: plan succeeds, implement succeeds, review succeeds
	innerBackend := &testCodergenBackend{
		responses: []AgentRunResult{
			{Output: "Plan: create main.py with print('hello world')", ToolCalls: 0, TokensUsed: 50, Success: true},
			{Output: "print('hello world')", ToolCalls: 3, TokensUsed: 200, Success: true},
			{Output: "Code review: looks good, simple and correct", ToolCalls: 1, TokensUsed: 80, Success: true},
		},
	}

	backend := &artifactWritingBackend{
		inner: innerBackend,
		rd:    rd,
	}

	// --- Step 3: Execute the pipeline ---
	engine := NewEngine(EngineConfig{
		Backend:       backend,
		CheckpointDir: cpDir,
		DefaultRetry:  RetryPolicyNone(),
	})

	result, err := engine.Run(context.Background(), source)
	if err != nil {
		t.Fatalf("pipeline execution failed: %v", err)
	}

	// --- Step 4: Verify outcome ---
	if result.FinalOutcome == nil {
		t.Fatal("expected non-nil final outcome")
	}
	if result.FinalOutcome.Status != StatusSuccess {
		t.Errorf("expected final status success, got %v", result.FinalOutcome.Status)
	}

	// Verify all expected nodes were completed
	expectedNodes := []string{"start", "plan", "implement", "review", "done"}
	for _, expected := range expectedNodes {
		found := false
		for _, completed := range result.CompletedNodes {
			if completed == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in completed nodes, got: %v", expected, result.CompletedNodes)
		}
	}

	// Verify implement was completed (specifically called out in spec)
	implementCompleted := false
	for _, n := range result.CompletedNodes {
		if n == "implement" {
			implementCompleted = true
			break
		}
	}
	if !implementCompleted {
		t.Errorf("expected 'implement' in completed nodes: %v", result.CompletedNodes)
	}

	// --- Step 5: Verify artifacts exist for plan, implement, review ---
	artifactNodes := []string{"plan", "implement", "review"}
	requiredArtifacts := []string{"prompt.md", "response.md", "status.json"}

	for _, nodeID := range artifactNodes {
		artifacts, err := rd.ListNodeArtifacts(nodeID)
		if err != nil {
			t.Errorf("failed to list artifacts for %q: %v", nodeID, err)
			continue
		}

		for _, requiredFile := range requiredArtifacts {
			found := false
			for _, a := range artifacts {
				if a == requiredFile {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected artifact %q for node %q, found: %v", requiredFile, nodeID, artifacts)
			}
		}

		// Verify prompt.md content is non-empty
		promptData, err := rd.ReadNodeArtifact(nodeID, "prompt.md")
		if err != nil {
			t.Errorf("failed to read prompt.md for %q: %v", nodeID, err)
		} else if len(promptData) == 0 {
			t.Errorf("prompt.md for %q is empty", nodeID)
		}

		// Verify response.md content is non-empty
		responseData, err := rd.ReadNodeArtifact(nodeID, "response.md")
		if err != nil {
			t.Errorf("failed to read response.md for %q: %v", nodeID, err)
		} else if len(responseData) == 0 {
			t.Errorf("response.md for %q is empty", nodeID)
		}

		// Verify status.json content is valid
		statusData, err := rd.ReadNodeArtifact(nodeID, "status.json")
		if err != nil {
			t.Errorf("failed to read status.json for %q: %v", nodeID, err)
		} else {
			var statusMap map[string]any
			if jsonErr := json.Unmarshal(statusData, &statusMap); jsonErr != nil {
				t.Errorf("status.json for %q is not valid JSON: %v", nodeID, jsonErr)
			}
		}
	}

	// --- Step 6: Verify goal gate was satisfied on implement ---
	implementOutcome, ok := result.NodeOutcomes["implement"]
	if !ok {
		t.Fatal("expected outcome for 'implement' node")
	}
	if implementOutcome.Status != StatusSuccess && implementOutcome.Status != StatusPartialSuccess {
		t.Errorf("goal_gate on implement requires success, got %v", implementOutcome.Status)
	}

	// Also verify via checkGoalGates helper directly
	// Re-parse for the gate check since the engine consumed the original graph
	checkGraph, _ := Parse(source)
	checkGraph = ApplyTransforms(checkGraph, DefaultTransforms()...)
	gateOK, failedNode := checkGoalGates(checkGraph, result.NodeOutcomes)
	if !gateOK {
		failedID := ""
		if failedNode != nil {
			failedID = failedNode.ID
		}
		t.Errorf("goal gate check failed, unsatisfied node: %q", failedID)
	}

	// --- Step 7: Verify checkpoint ---
	entries, err := os.ReadDir(cpDir)
	if err != nil {
		t.Fatalf("failed to read checkpoint dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one checkpoint file")
	}

	// Find the latest checkpoint (most completed nodes)
	var latestCheckpoint *Checkpoint
	for _, entry := range entries {
		cp, err := LoadCheckpoint(filepath.Join(cpDir, entry.Name()))
		if err != nil {
			t.Errorf("failed to load checkpoint %q: %v", entry.Name(), err)
			continue
		}
		if latestCheckpoint == nil || len(cp.CompletedNodes) > len(latestCheckpoint.CompletedNodes) {
			latestCheckpoint = cp
		}
	}

	if latestCheckpoint == nil {
		t.Fatal("no valid checkpoints found")
	}

	// The latest checkpoint's current_node should be the last non-terminal node processed
	// (the engine saves checkpoints after non-terminal nodes).
	// Verify that plan, implement, and review are in the completed nodes.
	checkpointCompletedSet := make(map[string]bool)
	for _, n := range latestCheckpoint.CompletedNodes {
		checkpointCompletedSet[n] = true
	}

	for _, expected := range []string{"plan", "implement", "review"} {
		if !checkpointCompletedSet[expected] {
			t.Errorf("expected %q in checkpoint completed nodes, got: %v", expected, latestCheckpoint.CompletedNodes)
		}
	}

	// Verify context in checkpoint contains the goal
	goalVal, ok := latestCheckpoint.ContextValues["goal"]
	if !ok {
		t.Error("expected 'goal' in checkpoint context values")
	} else if goalStr, ok := goalVal.(string); !ok || goalStr != "Create a hello world Python script" {
		t.Errorf("expected checkpoint goal = 'Create a hello world Python script', got %v", goalVal)
	}
}
