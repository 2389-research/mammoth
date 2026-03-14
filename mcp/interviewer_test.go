// ABOUTME: Tests for the MCP channel-based interviewer.
// ABOUTME: Validates blocking behavior, answer delivery, and context cancellation.
package mcp

import (
	"context"
	"testing"
	"time"
)

func TestMCPInterviewerReceivesAnswer(t *testing.T) {
	ctx := context.Background()
	run := &ActiveRun{
		ID:       "test-run",
		Status:   StatusRunning,
		answerCh: make(chan string, 1),
	}
	iv := &mcpInterviewer{run: run, ctx: ctx}
	go func() {
		time.Sleep(10 * time.Millisecond)
		run.answerCh <- "yes"
	}()
	answer, err := iv.Ask("Continue?", []string{"yes", "no"}, "yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "yes" {
		t.Errorf("expected %q, got %q", "yes", answer)
	}
	// After Ask returns, PendingQuestion is cleared and status is back to running.
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.PendingQuestion != nil {
		t.Error("expected pending question to be cleared after answer received")
	}
	if run.Status != StatusRunning {
		t.Errorf("expected status %q after answer, got %q", StatusRunning, run.Status)
	}
}

func TestMCPInterviewerContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	run := &ActiveRun{
		ID:       "test-run",
		Status:   StatusRunning,
		answerCh: make(chan string, 1),
	}
	iv := &mcpInterviewer{run: run, ctx: ctx}
	_, err := iv.Ask("Will timeout?", nil, "")
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestMCPInterviewerSetsPausedStatus(t *testing.T) {
	ctx := context.Background()
	run := &ActiveRun{
		ID:       "test-run",
		Status:   StatusRunning,
		answerCh: make(chan string, 1),
	}
	iv := &mcpInterviewer{run: run, ctx: ctx}
	go func() {
		for {
			run.mu.RLock()
			s := run.Status
			run.mu.RUnlock()
			if s == StatusPaused {
				run.answerCh <- "proceed"
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	answer, err := iv.Ask("Gate check", nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "proceed" {
		t.Errorf("expected %q, got %q", "proceed", answer)
	}
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.Status != StatusRunning {
		t.Errorf("expected status %q, got %q", StatusRunning, run.Status)
	}
	if run.PendingQuestion != nil {
		t.Error("expected pending question to be cleared")
	}
}

func TestMCPInterviewerAskFreeform(t *testing.T) {
	ctx := context.Background()
	run := &ActiveRun{
		ID:       "test-run",
		Status:   StatusRunning,
		answerCh: make(chan string, 1),
	}
	iv := &mcpInterviewer{run: run, ctx: ctx}
	go func() {
		time.Sleep(10 * time.Millisecond)
		run.answerCh <- "freeform answer"
	}()
	answer, err := iv.AskFreeform("What do you think?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "freeform answer" {
		t.Errorf("expected %q, got %q", "freeform answer", answer)
	}
}
