// ABOUTME: Tests for ChannelInterviewer that bridges human gates to SSE events.
// ABOUTME: Validates blocking Ask/AskFreeform, Respond, concurrent gates, and unknown gate errors.
package web

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestChannelInterviewer_AskAndRespond(t *testing.T) {
	var received []BuildEvent
	var mu sync.Mutex
	broadcast := func(evt BuildEvent) {
		mu.Lock()
		received = append(received, evt)
		mu.Unlock()
	}

	iv := NewChannelInterviewer(context.Background(), broadcast)

	var answer string
	var askErr error
	done := make(chan struct{})
	go func() {
		answer, askErr = iv.Ask("approve?", []string{"yes", "no"}, "yes")
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(received) == 0 {
		t.Fatal("expected gate event to be broadcast")
	}
	evt := received[0]
	mu.Unlock()

	if evt.Type != BuildEventHumanGateChoice {
		t.Fatalf("expected %q, got %q", BuildEventHumanGateChoice, evt.Type)
	}
	gateID, ok := evt.Data["gate_id"].(string)
	if !ok || gateID == "" {
		t.Fatal("expected gate_id in event data")
	}

	if err := iv.Respond(gateID, "no"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	<-done
	if askErr != nil {
		t.Fatalf("ask error: %v", askErr)
	}
	if answer != "no" {
		t.Errorf("expected answer=no, got %q", answer)
	}
}

func TestChannelInterviewer_AskFreeformAndRespond(t *testing.T) {
	var received []BuildEvent
	var mu sync.Mutex
	broadcast := func(evt BuildEvent) {
		mu.Lock()
		received = append(received, evt)
		mu.Unlock()
	}

	iv := NewChannelInterviewer(context.Background(), broadcast)

	var answer string
	var askErr error
	done := make(chan struct{})
	go func() {
		answer, askErr = iv.AskFreeform("describe the feature:")
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(received) == 0 {
		t.Fatal("expected gate event")
	}
	evt := received[0]
	mu.Unlock()

	if evt.Type != BuildEventHumanGateFreeform {
		t.Fatalf("expected %q, got %q", BuildEventHumanGateFreeform, evt.Type)
	}
	gateID := evt.Data["gate_id"].(string)

	if err := iv.Respond(gateID, "a login page"); err != nil {
		t.Fatalf("respond: %v", err)
	}

	<-done
	if askErr != nil {
		t.Fatalf("ask error: %v", askErr)
	}
	if answer != "a login page" {
		t.Errorf("expected 'a login page', got %q", answer)
	}
}

func TestChannelInterviewer_RespondUnknownGate(t *testing.T) {
	iv := NewChannelInterviewer(context.Background(), func(BuildEvent) {})
	err := iv.Respond("nonexistent", "answer")
	if err == nil {
		t.Fatal("expected error for unknown gate")
	}
}

func TestChannelInterviewer_ConcurrentGates(t *testing.T) {
	iv := NewChannelInterviewer(context.Background(), func(BuildEvent) {})

	var wg sync.WaitGroup
	results := make([]string, 3)
	gateIDs := make([]string, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		idx := i
		go func() {
			defer wg.Done()
			answer, err := iv.AskFreeform("gate " + string(rune('A'+idx)))
			if err != nil {
				t.Errorf("gate %d: %v", idx, err)
				return
			}
			results[idx] = answer
		}()
	}

	time.Sleep(100 * time.Millisecond)

	iv.mu.Lock()
	i := 0
	for id := range iv.pending {
		gateIDs[i] = id
		i++
	}
	iv.mu.Unlock()

	for j, id := range gateIDs {
		if err := iv.Respond(id, "answer-"+string(rune('A'+j))); err != nil {
			t.Fatalf("respond gate %d: %v", j, err)
		}
	}

	wg.Wait()

	for j, r := range results {
		if r == "" {
			t.Errorf("gate %d got empty result", j)
		}
	}
}
