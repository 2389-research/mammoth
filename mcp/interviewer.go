// ABOUTME: Channel-based interviewer that bridges MCP tool calls to tracker human gates.
// ABOUTME: Blocks the pipeline goroutine until an answer arrives via the answer_question tool.
package mcp

import (
	"context"
	"crypto/rand"
	"fmt"
)

// mcpInterviewer implements handlers.FreeformInterviewer by blocking on the
// run's answer channel. It uses a stored context for cancellation so the
// signature matches tracker's Interviewer interface (no context parameter).
type mcpInterviewer struct {
	run *ActiveRun
	ctx context.Context
}

// Ask sets a pending question on the run, pauses the pipeline, and blocks
// until an answer arrives on the answer channel or the context is cancelled.
// This signature matches tracker's handlers.Interviewer interface.
func (iv *mcpInterviewer) Ask(prompt string, choices []string, defaultChoice string) (string, error) {
	if err := iv.ctx.Err(); err != nil {
		return "", err
	}

	// Drain any stale answer from a previous question.
	select {
	case <-iv.run.answerCh:
	default:
	}

	qid := randomHex(8)
	iv.run.mu.Lock()
	iv.run.Status = StatusPaused
	iv.run.PendingQuestion = &PendingQuestion{
		ID:      qid,
		Text:    prompt,
		Options: choices,
		NodeID:  iv.run.CurrentNode,
	}
	iv.run.mu.Unlock()
	select {
	case <-iv.ctx.Done():
		// Clean up pending question and restore running state on cancellation
		// to avoid leaving the run stuck as paused with a stale question.
		iv.run.mu.Lock()
		if iv.run.PendingQuestion != nil && iv.run.PendingQuestion.ID == qid {
			iv.run.PendingQuestion = nil
			iv.run.Status = StatusRunning
		}
		iv.run.mu.Unlock()
		// Drain any answer that arrived between cancellation and cleanup.
		select {
		case <-iv.run.answerCh:
		default:
		}
		return "", iv.ctx.Err()
	case answer := <-iv.run.answerCh:
		iv.run.mu.Lock()
		iv.run.Status = StatusRunning
		iv.run.PendingQuestion = nil
		iv.run.mu.Unlock()
		return answer, nil
	}
}

// AskFreeform presents a freeform prompt without fixed choices.
// This satisfies the handlers.FreeformInterviewer interface.
func (iv *mcpInterviewer) AskFreeform(prompt string) (string, error) {
	return iv.Ask(prompt, nil, "")
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fall back to timestamp-based uniqueness if crypto/rand fails.
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return fmt.Sprintf("%x", b)
}
