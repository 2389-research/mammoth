// ABOUTME: Tests for human gate handler timeout, default choice, and reminder interval features.
// ABOUTME: Validates that the handler respects timeouts and falls back to default choices when configured.
package attractor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// slowInterviewer simulates a human who takes a configurable amount of time to respond.
type slowInterviewer struct {
	delay  time.Duration
	answer string
}

func (s *slowInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	select {
	case <-time.After(s.delay):
		return s.answer, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// --- Timeout with default choice tests ---

func TestHumanHandler_TimeoutWithDefaultChoice_SelectsDefault(t *testing.T) {
	// A slow interviewer that takes 5 seconds but we only wait 100ms.
	// The default_choice should be selected on timeout.
	interviewer := &slowInterviewer{delay: 5 * time.Second, answer: "[N] No"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":          "hexagon",
		"label":          "Do you approve?",
		"timeout":        "100ms",
		"default_choice": "[Y] Yes",
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
		t.Errorf("expected status success on timeout with default_choice, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	if outcome.PreferredLabel != "[Y] Yes" {
		t.Errorf("expected PreferredLabel = '[Y] Yes', got %q", outcome.PreferredLabel)
	}
	if !strings.Contains(outcome.Notes, "timed out") {
		t.Errorf("expected notes to mention 'timed out', got %q", outcome.Notes)
	}
}

func TestHumanHandler_TimeoutWithDefaultChoice_SetsContextUpdates(t *testing.T) {
	interviewer := &slowInterviewer{delay: 5 * time.Second, answer: "[N] No"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":          "hexagon",
		"label":          "Do you approve?",
		"timeout":        "100ms",
		"default_choice": "[Y] Yes",
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

	timedOut, ok := outcome.ContextUpdates["human.timed_out"]
	if !ok {
		t.Fatal("expected human.timed_out in context updates")
	}
	if timedOut != true {
		t.Errorf("expected human.timed_out = true, got %v", timedOut)
	}

	responseTimeMs, ok := outcome.ContextUpdates["human.response_time_ms"]
	if !ok {
		t.Fatal("expected human.response_time_ms in context updates")
	}
	if ms, ok := responseTimeMs.(int64); !ok || ms < 0 {
		t.Errorf("expected human.response_time_ms to be a non-negative int64, got %v (%T)", responseTimeMs, responseTimeMs)
	}
}

// --- Timeout without default choice tests ---

func TestHumanHandler_TimeoutWithoutDefaultChoice_Fails(t *testing.T) {
	// Timeout with no default_choice should result in a failure.
	interviewer := &slowInterviewer{delay: 5 * time.Second, answer: "[Y] Yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":   "hexagon",
		"label":   "Do you approve?",
		"timeout": "100ms",
		// No default_choice
	})
	addNode(g, "approve", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail on timeout without default_choice, got %v", outcome.Status)
	}
	if outcome.FailureReason == "" {
		t.Error("expected a failure reason describing the timeout")
	}
	if !strings.Contains(outcome.FailureReason, "timeout") && !strings.Contains(outcome.FailureReason, "timed out") {
		t.Errorf("expected failure reason to mention timeout, got %q", outcome.FailureReason)
	}
}

func TestHumanHandler_TimeoutWithoutDefaultChoice_SetsTimedOutContext(t *testing.T) {
	interviewer := &slowInterviewer{delay: 5 * time.Second, answer: "[Y] Yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":   "hexagon",
		"label":   "Do you approve?",
		"timeout": "100ms",
	})
	addNode(g, "approve", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	timedOut, ok := outcome.ContextUpdates["human.timed_out"]
	if !ok {
		t.Fatal("expected human.timed_out in context updates")
	}
	if timedOut != true {
		t.Errorf("expected human.timed_out = true, got %v", timedOut)
	}
}

// --- No timeout (existing behavior preserved) ---

func TestHumanHandler_NoTimeout_WaitsForAnswer(t *testing.T) {
	// Without a timeout attribute, the handler should wait for the answer.
	interviewer := &slowInterviewer{delay: 50 * time.Millisecond, answer: "[Y] Yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape": "hexagon",
		"label": "Do you approve?",
		// No timeout attribute
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

	timedOut, ok := outcome.ContextUpdates["human.timed_out"]
	if !ok {
		t.Fatal("expected human.timed_out in context updates")
	}
	if timedOut != false {
		t.Errorf("expected human.timed_out = false, got %v", timedOut)
	}

	responseTimeMs, ok := outcome.ContextUpdates["human.response_time_ms"]
	if !ok {
		t.Fatal("expected human.response_time_ms in context updates")
	}
	if ms, ok := responseTimeMs.(int64); !ok || ms < 0 {
		t.Errorf("expected human.response_time_ms to be a non-negative int64, got %v (%T)", responseTimeMs, responseTimeMs)
	}
}

// --- Fast response within timeout ---

func TestHumanHandler_FastResponseWithinTimeout_Succeeds(t *testing.T) {
	// Human responds quickly, well within the timeout.
	interviewer := &slowInterviewer{delay: 10 * time.Millisecond, answer: "[N] No"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":          "hexagon",
		"label":          "Do you approve?",
		"timeout":        "5s",
		"default_choice": "[Y] Yes",
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
	// Should have selected [N] No since human answered in time
	if outcome.ContextUpdates["human.gate.label"] != "[N] No" {
		t.Errorf("expected human.gate.label = '[N] No', got %v", outcome.ContextUpdates["human.gate.label"])
	}

	timedOut := outcome.ContextUpdates["human.timed_out"]
	if timedOut != false {
		t.Errorf("expected human.timed_out = false, got %v", timedOut)
	}
}

// --- Invalid timeout duration ---

func TestHumanHandler_InvalidTimeoutDuration_Fails(t *testing.T) {
	interviewer := &stubInterviewer{answer: "[Y] Yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":   "hexagon",
		"label":   "Do you approve?",
		"timeout": "not-a-duration",
	})
	addNode(g, "approve", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail for invalid timeout, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "timeout") {
		t.Errorf("expected failure reason to mention timeout parsing, got %q", outcome.FailureReason)
	}
}

// --- Default choice that doesn't match any edge ---

func TestHumanHandler_TimeoutWithNonMatchingDefault_Fails(t *testing.T) {
	interviewer := &slowInterviewer{delay: 5 * time.Second, answer: "[N] No"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":          "hexagon",
		"label":          "Do you approve?",
		"timeout":        "100ms",
		"default_choice": "[X] NonExistent",
	})
	addNode(g, "approve", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail for non-matching default_choice, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "default_choice") {
		t.Errorf("expected failure reason to mention default_choice, got %q", outcome.FailureReason)
	}
}

// --- Reminder interval parsing ---

func TestHumanHandler_ReminderIntervalParsed(t *testing.T) {
	// Verify the reminder_interval attribute is parsed without error.
	// The current implementation records it but doesn't act on it since
	// no interviewer supports re-prompting yet.
	interviewer := &slowInterviewer{delay: 10 * time.Millisecond, answer: "[Y] Yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":             "hexagon",
		"label":             "Do you approve?",
		"timeout":           "5s",
		"default_choice":    "[Y] Yes",
		"reminder_interval": "1m",
	})
	addNode(g, "approve", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
}

func TestHumanHandler_InvalidReminderInterval_Fails(t *testing.T) {
	interviewer := &stubInterviewer{answer: "[Y] Yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":             "hexagon",
		"label":             "Do you approve?",
		"timeout":           "5s",
		"reminder_interval": "bad-interval",
	})
	addNode(g, "approve", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail for invalid reminder_interval, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "reminder_interval") {
		t.Errorf("expected failure reason to mention reminder_interval, got %q", outcome.FailureReason)
	}
}

// --- Parent context cancellation still respected ---

func TestHumanHandler_ParentContextCancelled_ReturnsError(t *testing.T) {
	interviewer := &slowInterviewer{delay: 5 * time.Second, answer: "[Y] Yes"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":   "hexagon",
		"label":   "Do you approve?",
		"timeout": "10s",
	})
	addNode(g, "approve", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := h.Execute(ctx, node, pctx, store)
	if err == nil {
		t.Error("expected error for cancelled parent context")
	}
}

// --- Timeout with accelerator key matching on default_choice ---

func TestHumanHandler_TimeoutDefaultChoiceMatchesByAccelerator(t *testing.T) {
	interviewer := &slowInterviewer{delay: 5 * time.Second, answer: "[N] No"}
	h := &WaitForHumanHandler{Interviewer: interviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":          "hexagon",
		"label":          "Approve deployment?",
		"timeout":        "100ms",
		"default_choice": "[A] Approve",
	})
	addNode(g, "approve", map[string]string{})
	addNode(g, "reject", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[A] Approve"})
	addEdge(g, "human_gate", "reject", map[string]string{"label": "[R] Reject"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	if outcome.PreferredLabel != "[A] Approve" {
		t.Errorf("expected PreferredLabel = '[A] Approve', got %q", outcome.PreferredLabel)
	}
	// Should route to the approve node
	if len(outcome.SuggestedNextIDs) != 1 || outcome.SuggestedNextIDs[0] != "approve" {
		t.Errorf("expected SuggestedNextIDs = [approve], got %v", outcome.SuggestedNextIDs)
	}
}

// --- Node ID injection into context ---

// spyInterviewer captures the context passed to Ask so we can inspect it.
type spyInterviewer struct {
	capturedCtx context.Context
	answer      string
}

func (s *spyInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	s.capturedCtx = ctx
	return s.answer, nil
}

func TestHumanHandlerInjectsNodeIDInContext(t *testing.T) {
	spy := &spyInterviewer{answer: "[Y] Yes"}
	h := &WaitForHumanHandler{Interviewer: spy}

	g := newTestGraph()
	node := addNode(g, "deploy_gate", map[string]string{
		"shape": "hexagon",
		"label": "Approve deployment?",
	})
	addNode(g, "deploy", map[string]string{})
	addEdge(g, "deploy_gate", "deploy", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected success, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}

	// Verify the node ID was injected into the context passed to Ask
	nodeID := NodeIDFromContext(spy.capturedCtx)
	if nodeID != "deploy_gate" {
		t.Errorf("expected node ID 'deploy_gate' in context, got %q", nodeID)
	}
}

// --- Interviewer error with timeout configured ---

func TestHumanHandler_InterviewerErrorWithTimeout_ReturnsFailure(t *testing.T) {
	errInterviewer := &stubInterviewer{answer: "", err: fmt.Errorf("connection lost")}
	h := &WaitForHumanHandler{Interviewer: errInterviewer}

	g := newTestGraph()
	node := addNode(g, "human_gate", map[string]string{
		"shape":          "hexagon",
		"label":          "Approve?",
		"timeout":        "5s",
		"default_choice": "[Y] Yes",
	})
	addNode(g, "approve", map[string]string{})
	addEdge(g, "human_gate", "approve", map[string]string{"label": "[Y] Yes"})

	pctx := newContextWithGraph(g)
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail on interviewer error, got %v", outcome.Status)
	}
}
