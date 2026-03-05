// ABOUTME: Tests for MCP server core types.
// ABOUTME: Validates RunStatus constants, ActiveRun state transitions, and PendingQuestion serialization.
package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

func TestRunStatusConstants(t *testing.T) {
	statuses := []RunStatus{StatusRunning, StatusPaused, StatusCompleted, StatusFailed}
	expected := []string{"running", "paused", "completed", "failed"}
	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("status %d: got %q, want %q", i, s, expected[i])
		}
	}
}

func TestPendingQuestionJSON(t *testing.T) {
	q := &PendingQuestion{
		ID:      "q1",
		Text:    "Continue?",
		Options: []string{"yes", "no"},
		NodeID:  "gate_1",
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PendingQuestion
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != q.ID || got.Text != q.Text || got.NodeID != q.NodeID {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
	if len(got.Options) != 2 || got.Options[0] != "yes" {
		t.Errorf("options mismatch: got %v", got.Options)
	}
}

func TestRunConfigDefaults(t *testing.T) {
	cfg := RunConfig{}
	if cfg.RetryPolicy != "" {
		t.Errorf("expected empty default retry policy, got %q", cfg.RetryPolicy)
	}
}

func TestActiveRunFields(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	run := &ActiveRun{
		ID:             "test-run",
		Status:         StatusRunning,
		Source:         "digraph { a -> b }",
		CompletedNodes: make([]string, 0),
		EventBuffer:    make([]attractor.EngineEvent, 0, maxEventBuffer),
		CreatedAt:      time.Now(),
		answerCh:       make(chan string, 1),
		cancel:         cancel,
	}

	// Verify mutex works for concurrent access.
	run.mu.Lock()
	run.CurrentNode = "node_1"
	run.mu.Unlock()

	run.mu.RLock()
	node := run.CurrentNode
	run.mu.RUnlock()
	if node != "node_1" {
		t.Errorf("CurrentNode: got %q, want %q", node, "node_1")
	}

	// Verify answerCh is buffered.
	run.answerCh <- "answer"
	got := <-run.answerCh
	if got != "answer" {
		t.Errorf("answerCh: got %q, want %q", got, "answer")
	}

	// Verify cancel function works.
	run.cancel()
	if ctx.Err() == nil {
		t.Error("expected context to be cancelled")
	}

	// Verify maxEventBuffer constant.
	if maxEventBuffer != 500 {
		t.Errorf("maxEventBuffer: got %d, want 500", maxEventBuffer)
	}
}
