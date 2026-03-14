// ABOUTME: ChannelInterviewer bridges pipeline human gates to SSE events.
// ABOUTME: Blocks the pipeline handler, broadcasts gate events, and waits for user responses.
package web

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// ChannelInterviewer implements handlers.Interviewer and handlers.FreeformInterviewer.
// When the pipeline hits a human gate, it broadcasts a BuildEvent and blocks
// until Respond() is called with the user's answer or the context is cancelled.
type ChannelInterviewer struct {
	broadcast func(BuildEvent)
	pending   map[string]chan string
	mu        sync.Mutex
	ctx       context.Context
}

// NewChannelInterviewer creates a ChannelInterviewer that broadcasts gate events
// via the given function. The context is used for cancellation of blocking Ask calls.
func NewChannelInterviewer(ctx context.Context, broadcast func(BuildEvent)) *ChannelInterviewer {
	return &ChannelInterviewer{
		broadcast: broadcast,
		pending:   make(map[string]chan string),
		ctx:       ctx,
	}
}

// Ask presents a multiple-choice gate. Blocks until Respond() is called
// or the context is cancelled.
func (iv *ChannelInterviewer) Ask(prompt string, choices []string, defaultChoice string) (string, error) {
	if err := iv.ctx.Err(); err != nil {
		return "", err
	}

	gateID := generateGateID()
	ch := make(chan string, 1)

	iv.mu.Lock()
	iv.pending[gateID] = ch
	iv.mu.Unlock()

	defer func() {
		iv.mu.Lock()
		delete(iv.pending, gateID)
		iv.mu.Unlock()
	}()

	iv.broadcast(BuildEvent{
		Type:      BuildEventHumanGateChoice,
		Timestamp: time.Now(),
		Message:   prompt,
		Data: map[string]any{
			"gate_id": gateID,
			"choices": choices,
			"default": defaultChoice,
		},
	})

	select {
	case answer := <-ch:
		return answer, nil
	case <-iv.ctx.Done():
		return "", iv.ctx.Err()
	}
}

// AskFreeform presents an open-ended text input gate. Blocks until Respond()
// is called or the context is cancelled.
func (iv *ChannelInterviewer) AskFreeform(prompt string) (string, error) {
	if err := iv.ctx.Err(); err != nil {
		return "", err
	}

	gateID := generateGateID()
	ch := make(chan string, 1)

	iv.mu.Lock()
	iv.pending[gateID] = ch
	iv.mu.Unlock()

	defer func() {
		iv.mu.Lock()
		delete(iv.pending, gateID)
		iv.mu.Unlock()
	}()

	iv.broadcast(BuildEvent{
		Type:      BuildEventHumanGateFreeform,
		Timestamp: time.Now(),
		Message:   prompt,
		Data:      map[string]any{"gate_id": gateID},
	})

	select {
	case answer := <-ch:
		return answer, nil
	case <-iv.ctx.Done():
		return "", iv.ctx.Err()
	}
}

// Respond delivers the user's answer to a pending gate. It returns an error
// if the gate ID is unknown or the gate has already been answered.
func (iv *ChannelInterviewer) Respond(gateID, answer string) error {
	iv.mu.Lock()
	ch, ok := iv.pending[gateID]
	iv.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending gate %q", gateID)
	}
	select {
	case ch <- answer:
		return nil
	default:
		return fmt.Errorf("gate %q already answered", gateID)
	}
}

func generateGateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
