// ABOUTME: Tests for the event system used by the coding agent session.
// ABOUTME: Covers EventEmitter subscribe/emit/unsubscribe/close and Session.Emit helper.

package agent

import (
	"testing"
	"time"
)

func TestEventEmitter(t *testing.T) {
	emitter := NewEventEmitter()
	defer emitter.Close()

	ch := emitter.Subscribe()

	event := SessionEvent{
		Kind:      EventSessionStart,
		Timestamp: time.Now(),
		SessionID: "test-session",
		Data:      map[string]any{"key": "value"},
	}

	emitter.Emit(event)

	select {
	case received := <-ch:
		if received.Kind != EventSessionStart {
			t.Errorf("expected kind %s, got %s", EventSessionStart, received.Kind)
		}
		if received.SessionID != "test-session" {
			t.Errorf("expected session ID 'test-session', got %s", received.SessionID)
		}
		if received.Data["key"] != "value" {
			t.Errorf("expected data key 'value', got %v", received.Data["key"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventEmitterMultipleSubscribers(t *testing.T) {
	emitter := NewEventEmitter()
	defer emitter.Close()

	ch1 := emitter.Subscribe()
	ch2 := emitter.Subscribe()
	ch3 := emitter.Subscribe()

	event := SessionEvent{
		Kind:      EventUserInput,
		Timestamp: time.Now(),
		SessionID: "test-session",
		Data:      map[string]any{"input": "hello"},
	}

	emitter.Emit(event)

	for i, ch := range []<-chan SessionEvent{ch1, ch2, ch3} {
		select {
		case received := <-ch:
			if received.Kind != EventUserInput {
				t.Errorf("subscriber %d: expected kind %s, got %s", i, EventUserInput, received.Kind)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}

func TestEventEmitterUnsubscribe(t *testing.T) {
	emitter := NewEventEmitter()
	defer emitter.Close()

	ch1 := emitter.Subscribe()
	ch2 := emitter.Subscribe()

	emitter.Unsubscribe(ch1)

	event := SessionEvent{
		Kind:      EventError,
		Timestamp: time.Now(),
		SessionID: "test-session",
		Data:      map[string]any{},
	}

	emitter.Emit(event)

	// ch2 should still receive the event
	select {
	case received := <-ch2:
		if received.Kind != EventError {
			t.Errorf("expected kind %s, got %s", EventError, received.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("ch2 timed out waiting for event")
	}

	// ch1 should not receive anything (it was unsubscribed and closed)
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("ch1 should have been closed after unsubscribe, but received an event")
		}
		// ok == false means channel is closed, which is expected
	case <-time.After(100 * time.Millisecond):
		// Also acceptable: channel was closed and drained
	}
}

func TestEventEmitterClose(t *testing.T) {
	emitter := NewEventEmitter()

	ch1 := emitter.Subscribe()
	ch2 := emitter.Subscribe()

	emitter.Close()

	// Both channels should be closed
	for i, ch := range []<-chan SessionEvent{ch1, ch2} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("subscriber %d: channel should be closed", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for channel close", i)
		}
	}

	// Emitting after close should not panic
	emitter.Emit(SessionEvent{
		Kind:      EventSessionEnd,
		Timestamp: time.Now(),
		SessionID: "test-session",
	})
}

func TestSessionEmit(t *testing.T) {
	config := DefaultSessionConfig()
	session := NewSession(config)
	defer session.Close()

	ch := session.EventEmitter.Subscribe()

	session.Emit(EventSessionStart, map[string]any{"test": true})

	select {
	case received := <-ch:
		if received.Kind != EventSessionStart {
			t.Errorf("expected kind %s, got %s", EventSessionStart, received.Kind)
		}
		if received.SessionID != session.ID {
			t.Errorf("expected session ID %s, got %s", session.ID, received.SessionID)
		}
		if received.Data["test"] != true {
			t.Errorf("expected data test=true, got %v", received.Data["test"])
		}
		if received.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventKindConstants(t *testing.T) {
	// Verify all event kind constants are defined and have unique non-empty values
	kinds := []EventKind{
		EventSessionStart,
		EventSessionEnd,
		EventUserInput,
		EventAssistantTextStart,
		EventAssistantTextDelta,
		EventAssistantTextEnd,
		EventToolCallStart,
		EventToolCallOutputDelta,
		EventToolCallEnd,
		EventSteeringInjected,
		EventTurnLimit,
		EventLoopDetection,
		EventError,
	}

	seen := make(map[EventKind]bool)
	for _, kind := range kinds {
		if kind == "" {
			t.Error("found empty event kind constant")
		}
		if seen[kind] {
			t.Errorf("duplicate event kind: %s", kind)
		}
		seen[kind] = true
	}

	if len(kinds) != 13 {
		t.Errorf("expected 13 event kinds, got %d", len(kinds))
	}
}
