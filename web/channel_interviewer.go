// ABOUTME: ChannelInterviewer bridges pipeline human gates to SSE events.
// ABOUTME: Blocks the pipeline handler, broadcasts gate events, and waits for user responses.
package web

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// ChannelInterviewer implements handlers.Interviewer and handlers.FreeformInterviewer.
// When the pipeline hits a human gate, it broadcasts a BuildEvent and blocks
// until Respond() is called with the user's answer.
type ChannelInterviewer struct {
	broadcast func(BuildEvent)
	pending   map[string]chan string
	mu        sync.Mutex
}

// NewChannelInterviewer creates a ChannelInterviewer that broadcasts gate events
// via the given function.
func NewChannelInterviewer(broadcast func(BuildEvent)) *ChannelInterviewer {
	return &ChannelInterviewer{
		broadcast: broadcast,
		pending:   make(map[string]chan string),
	}
}

// Ask presents a multiple-choice gate. Blocks until Respond() is called.
func (iv *ChannelInterviewer) Ask(prompt string, choices []string, defaultChoice string) (string, error) {
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

	answer := <-ch
	return answer, nil
}

// AskFreeform presents an open-ended text input gate. Blocks until Respond() is called.
func (iv *ChannelInterviewer) AskFreeform(prompt string) (string, error) {
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

	answer := <-ch
	return answer, nil
}

// Respond delivers the user's answer to a pending gate.
func (iv *ChannelInterviewer) Respond(gateID, answer string) error {
	iv.mu.Lock()
	ch, ok := iv.pending[gateID]
	iv.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending gate %q", gateID)
	}
	ch <- answer
	return nil
}

func generateGateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
