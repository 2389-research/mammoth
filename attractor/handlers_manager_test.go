// ABOUTME: Tests for the ManagerLoopHandler supervision loop and ManagerBackend interface.
// ABOUTME: Covers nil backend (stub), custom backend, guard condition evaluation, iteration limits, and context cancellation.
package attractor

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// --- ManagerBackend interface stub for testing ---

// recordingManagerBackend records all calls to Observe, Guard, and Steer for test assertions.
type recordingManagerBackend struct {
	observeCalls []observeCall
	guardResults []bool
	steerCalls   []steerCall

	// guardReturnValues is a queue of booleans to return from Guard.
	// When exhausted, Guard returns true (on-track).
	guardReturnValues []bool
	guardIndex        int
}

type observeCall struct {
	nodeID  string
	iteration int
}

type steerCall struct {
	nodeID    string
	iteration int
	prompt    string
}

func (r *recordingManagerBackend) Observe(ctx context.Context, nodeID string, iteration int, pctx *Context) (string, error) {
	r.observeCalls = append(r.observeCalls, observeCall{nodeID: nodeID, iteration: iteration})
	return fmt.Sprintf("observation at iteration %d", iteration), nil
}

func (r *recordingManagerBackend) Guard(ctx context.Context, nodeID string, iteration int, observation string, guardCondition string, pctx *Context) (bool, error) {
	var result bool
	if r.guardIndex < len(r.guardReturnValues) {
		result = r.guardReturnValues[r.guardIndex]
		r.guardIndex++
	} else {
		result = true
	}
	r.guardResults = append(r.guardResults, result)
	return result, nil
}

func (r *recordingManagerBackend) Steer(ctx context.Context, nodeID string, iteration int, steerPrompt string, pctx *Context) (string, error) {
	r.steerCalls = append(r.steerCalls, steerCall{nodeID: nodeID, iteration: iteration, prompt: steerPrompt})
	return fmt.Sprintf("steering correction at iteration %d", iteration), nil
}

// errorManagerBackend returns errors from its methods for testing error paths.
type errorManagerBackend struct {
	observeErr error
	guardErr   error
	steerErr   error
}

func (e *errorManagerBackend) Observe(ctx context.Context, nodeID string, iteration int, pctx *Context) (string, error) {
	if e.observeErr != nil {
		return "", e.observeErr
	}
	return "ok", nil
}

func (e *errorManagerBackend) Guard(ctx context.Context, nodeID string, iteration int, observation string, guardCondition string, pctx *Context) (bool, error) {
	if e.guardErr != nil {
		return false, e.guardErr
	}
	return true, nil
}

func (e *errorManagerBackend) Steer(ctx context.Context, nodeID string, iteration int, steerPrompt string, pctx *Context) (string, error) {
	if e.steerErr != nil {
		return "", e.steerErr
	}
	return "ok", nil
}

// --- Tests for ManagerBackend interface ---

func TestManagerLoopHandlerWithNilBackendReturnsSuccess(t *testing.T) {
	h := &ManagerLoopHandler{} // nil backend
	g := newTestGraph()
	node := addNode(g, "manager", map[string]string{
		"shape":          "house",
		"max_iterations": "3",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success with nil backend, got %v", outcome.Status)
	}
	// Should mention stub behavior in notes
	if !strings.Contains(outcome.Notes, "stub") {
		t.Errorf("expected notes to mention stub behavior, got %q", outcome.Notes)
	}
}

func TestManagerLoopHandlerWithBackendRunsSupervisionLoop(t *testing.T) {
	backend := &recordingManagerBackend{
		// All guards pass -> no steering needed
		guardReturnValues: []bool{true, true, true},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":           "house",
		"observe_prompt":  "Check agent progress",
		"guard_condition": "context.status = ok",
		"steer_prompt":    "Redirect the agent",
		"max_iterations":  "3",
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

	// Should have observed 3 times
	if len(backend.observeCalls) != 3 {
		t.Errorf("expected 3 observe calls, got %d", len(backend.observeCalls))
	}

	// Should have guarded 3 times
	if len(backend.guardResults) != 3 {
		t.Errorf("expected 3 guard calls, got %d", len(backend.guardResults))
	}

	// No steering needed since all guards passed
	if len(backend.steerCalls) != 0 {
		t.Errorf("expected 0 steer calls, got %d", len(backend.steerCalls))
	}
}

func TestManagerLoopHandlerSteeringTriggeredOnGuardFailure(t *testing.T) {
	backend := &recordingManagerBackend{
		// First guard fails -> steering happens, second and third pass
		guardReturnValues: []bool{false, true, true},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":           "house",
		"observe_prompt":  "Watch the agent",
		"guard_condition": "context.on_track = true",
		"steer_prompt":    "Get back on track",
		"max_iterations":  "3",
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

	// Steering should have been called once (when guard failed at iteration 1)
	if len(backend.steerCalls) != 1 {
		t.Errorf("expected 1 steer call, got %d", len(backend.steerCalls))
	}
	if len(backend.steerCalls) > 0 && backend.steerCalls[0].iteration != 1 {
		t.Errorf("expected steer at iteration 1, got %d", backend.steerCalls[0].iteration)
	}
	if len(backend.steerCalls) > 0 && backend.steerCalls[0].prompt != "Get back on track" {
		t.Errorf("expected steer prompt 'Get back on track', got %q", backend.steerCalls[0].prompt)
	}
}

func TestManagerLoopHandlerDefaultMaxIterations(t *testing.T) {
	backend := &recordingManagerBackend{
		// All guards pass
		guardReturnValues: make([]bool, 10),
	}
	// Fill with true
	for i := range backend.guardReturnValues {
		backend.guardReturnValues[i] = true
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape": "house",
		// No max_iterations set -> default to 10
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

	// Default is 10 iterations
	if len(backend.observeCalls) != 10 {
		t.Errorf("expected 10 observe calls (default), got %d", len(backend.observeCalls))
	}
}

func TestManagerLoopHandlerContextUpdatesRecorded(t *testing.T) {
	backend := &recordingManagerBackend{
		guardReturnValues: []bool{true, true},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":           "house",
		"observe_prompt":  "Observe",
		"guard_condition": "context.ok = yes",
		"steer_prompt":    "Steer",
		"max_iterations":  "2",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should record last_stage
	if outcome.ContextUpdates["last_stage"] != "supervisor" {
		t.Errorf("expected last_stage = supervisor, got %v", outcome.ContextUpdates["last_stage"])
	}

	// Should record iteration count
	iterations, ok := outcome.ContextUpdates["manager.iterations_completed"]
	if !ok {
		t.Error("expected manager.iterations_completed in context updates")
	}
	if iterations != 2 {
		t.Errorf("expected 2 iterations completed, got %v", iterations)
	}

	// Should record steer count
	steers, ok := outcome.ContextUpdates["manager.steers_applied"]
	if !ok {
		t.Error("expected manager.steers_applied in context updates")
	}
	if steers != 0 {
		t.Errorf("expected 0 steers applied, got %v", steers)
	}
}

func TestManagerLoopHandlerObserveErrorReturnsFail(t *testing.T) {
	backend := &errorManagerBackend{
		observeErr: fmt.Errorf("observation system offline"),
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":          "house",
		"max_iterations": "3",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail on observe error, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "observe") {
		t.Errorf("expected failure reason mentioning observe, got %q", outcome.FailureReason)
	}
}

func TestManagerLoopHandlerGuardErrorReturnsFail(t *testing.T) {
	backend := &errorManagerBackend{
		guardErr: fmt.Errorf("guard evaluation crashed"),
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":          "house",
		"max_iterations": "3",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail on guard error, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "guard") {
		t.Errorf("expected failure reason mentioning guard, got %q", outcome.FailureReason)
	}
}

func TestManagerLoopHandlerSteerErrorReturnsFail(t *testing.T) {
	// Need guard to fail so steering is attempted, and then steer returns error
	h := &ManagerLoopHandler{Backend: &steerErrorBackend{
		steerErr: fmt.Errorf("steering mechanism jammed"),
	}}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":          "house",
		"max_iterations": "3",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail on steer error, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "steer") {
		t.Errorf("expected failure reason mentioning steer, got %q", outcome.FailureReason)
	}
}

// steerErrorBackend is a backend that makes guard fail (to trigger steering) and then returns an error from Steer.
type steerErrorBackend struct {
	steerErr error
}

func (s *steerErrorBackend) Observe(ctx context.Context, nodeID string, iteration int, pctx *Context) (string, error) {
	return "observed", nil
}

func (s *steerErrorBackend) Guard(ctx context.Context, nodeID string, iteration int, observation string, guardCondition string, pctx *Context) (bool, error) {
	return false, nil // guard fails, triggering steer
}

func (s *steerErrorBackend) Steer(ctx context.Context, nodeID string, iteration int, steerPrompt string, pctx *Context) (string, error) {
	return "", s.steerErr
}

func TestManagerLoopHandlerRespectsContextCancellation(t *testing.T) {
	backend := &recordingManagerBackend{
		guardReturnValues: []bool{true, true, true},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":          "house",
		"max_iterations": "100",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := h.Execute(ctx, node, pctx, store)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestManagerLoopHandlerInvalidMaxIterations(t *testing.T) {
	backend := &recordingManagerBackend{
		guardReturnValues: []bool{true},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":          "house",
		"max_iterations": "not_a_number",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to default (10) and still succeed
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success with invalid max_iterations (using default), got %v", outcome.Status)
	}
}

func TestManagerLoopHandlerSubPipelineAttributeRecorded(t *testing.T) {
	backend := &recordingManagerBackend{
		guardReturnValues: []bool{true},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":          "house",
		"sub_pipeline":   "child_workflow.dot",
		"max_iterations": "1",
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
	if outcome.ContextUpdates["manager.sub_pipeline"] != "child_workflow.dot" {
		t.Errorf("expected manager.sub_pipeline = 'child_workflow.dot', got %v", outcome.ContextUpdates["manager.sub_pipeline"])
	}
}

func TestManagerLoopHandlerMultipleSteeringCorrections(t *testing.T) {
	backend := &recordingManagerBackend{
		// All guards fail -> steering at every iteration
		guardReturnValues: []bool{false, false, false},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":          "house",
		"steer_prompt":   "Fix it",
		"max_iterations": "3",
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

	// All 3 iterations should have triggered steering
	if len(backend.steerCalls) != 3 {
		t.Errorf("expected 3 steer calls, got %d", len(backend.steerCalls))
	}

	steers, ok := outcome.ContextUpdates["manager.steers_applied"]
	if !ok {
		t.Error("expected manager.steers_applied in context updates")
	}
	if steers != 3 {
		t.Errorf("expected 3 steers applied, got %v", steers)
	}
}

func TestManagerLoopHandlerObservationsRecordedInContext(t *testing.T) {
	backend := &recordingManagerBackend{
		guardReturnValues: []bool{true, true},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "supervisor", map[string]string{
		"shape":          "house",
		"max_iterations": "2",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Last observation should be recorded
	lastObs, ok := outcome.ContextUpdates["manager.last_observation"]
	if !ok {
		t.Error("expected manager.last_observation in context updates")
	}
	if lastObs == nil || lastObs == "" {
		t.Error("expected non-empty last_observation")
	}
}

func TestManagerLoopHandlerBackwardCompatNilAttrs(t *testing.T) {
	h := &ManagerLoopHandler{} // nil backend
	g := newTestGraph()
	node := &Node{ID: "manager", Attrs: nil}
	g.Nodes["manager"] = node
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success with nil attrs, got %v", outcome.Status)
	}
}

func TestManagerLoopHandlerIterationNodeIDs(t *testing.T) {
	backend := &recordingManagerBackend{
		guardReturnValues: []bool{true, true, true},
	}
	h := &ManagerLoopHandler{Backend: backend}
	g := newTestGraph()
	node := addNode(g, "my_supervisor", map[string]string{
		"shape":          "house",
		"max_iterations": "3",
	})
	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All observe calls should reference the correct node ID
	for i, call := range backend.observeCalls {
		if call.nodeID != "my_supervisor" {
			t.Errorf("observe call %d: expected nodeID 'my_supervisor', got %q", i, call.nodeID)
		}
		if call.iteration != i+1 {
			t.Errorf("observe call %d: expected iteration %d, got %d", i, i+1, call.iteration)
		}
	}
}

// --- Backward compatibility with existing tests in handlers_test.go ---
// The existing TestManagerLoopHandlerRecordsConfig and TestManagerLoopHandlerDefaultConfig
// use the old manager.poll_interval / manager.max_cycles attributes.
// Those tests remain in handlers_test.go. We need to verify that the new handler
// still returns success with nil backend and those old-style attributes.

func TestManagerLoopHandlerBackwardCompatOldStyleAttrs(t *testing.T) {
	h := &ManagerLoopHandler{} // nil backend
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
}
