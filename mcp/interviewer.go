// ABOUTME: Channel-based interviewer that bridges MCP tool calls to attractor human gates.
// ABOUTME: Blocks the pipeline goroutine until an answer arrives via the answer_question tool.
package mcp

import (
	"context"
	"crypto/rand"
	"fmt"
)

// mcpInterviewer implements attractor.Interviewer by blocking on the run's
// answer channel.
type mcpInterviewer struct {
	run *ActiveRun
}

// Ask sets a pending question on the run, pauses the pipeline, and blocks
// until an answer arrives on the answer channel or the context is cancelled.
func (iv *mcpInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	qid := randomHex(8)
	iv.run.mu.Lock()
	iv.run.Status = StatusPaused
	iv.run.PendingQuestion = &PendingQuestion{
		ID:      qid,
		Text:    question,
		Options: options,
		NodeID:  iv.run.CurrentNode,
	}
	iv.run.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case answer := <-iv.run.answerCh:
		iv.run.mu.Lock()
		iv.run.Status = StatusRunning
		iv.run.PendingQuestion = nil
		iv.run.mu.Unlock()
		return answer, nil
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
