// ABOUTME: Event system for the coding agent session, enabling real-time observation of agent actions.
// ABOUTME: Provides EventEmitter with subscribe/emit/unsubscribe pattern and typed SessionEvent delivery.

package agent

import (
	"sync"
	"time"
)

// EventKind discriminates the type of session event.
type EventKind string

const (
	EventSessionStart        EventKind = "session_start"
	EventSessionEnd          EventKind = "session_end"
	EventUserInput           EventKind = "user_input"
	EventAssistantTextStart  EventKind = "assistant_text_start"
	EventAssistantTextDelta  EventKind = "assistant_text_delta"
	EventAssistantTextEnd    EventKind = "assistant_text_end"
	EventToolCallStart       EventKind = "tool_call_start"
	EventToolCallOutputDelta EventKind = "tool_call_output_delta"
	EventToolCallEnd         EventKind = "tool_call_end"
	EventSteeringInjected    EventKind = "steering_injected"
	EventTurnLimit           EventKind = "turn_limit"
	EventLoopDetection       EventKind = "loop_detection"
	EventError               EventKind = "error"
)

// SessionEvent represents a typed event emitted by the agent loop.
type SessionEvent struct {
	Kind      EventKind      `json:"kind"`
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id"`
	Data      map[string]any `json:"data,omitempty"`
}

// EventEmitter delivers session events to subscribed channels.
type EventEmitter struct {
	mu          sync.RWMutex
	subscribers []chan SessionEvent
	closed      bool
}

// NewEventEmitter creates a new EventEmitter.
func NewEventEmitter() *EventEmitter {
	return &EventEmitter{
		subscribers: make([]chan SessionEvent, 0),
	}
}

// Subscribe registers a new subscriber channel and returns it.
// The channel has a buffer of 64 to reduce the likelihood of blocking.
func (e *EventEmitter) Subscribe() <-chan SessionEvent {
	e.mu.Lock()
	defer e.mu.Unlock()

	ch := make(chan SessionEvent, 64)
	e.subscribers = append(e.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (e *EventEmitter) Unsubscribe(ch <-chan SessionEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, sub := range e.subscribers {
		// Cast the bidirectional channel to receive-only for comparison
		if (<-chan SessionEvent)(sub) == ch {
			close(sub)
			e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)
			return
		}
	}
}

// Emit sends an event to all subscribers. Non-blocking: if a subscriber's
// channel buffer is full, the event is dropped for that subscriber.
func (e *EventEmitter) Emit(event SessionEvent) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		return
	}

	for _, ch := range e.subscribers {
		select {
		case ch <- event:
		default:
			// Drop event for slow subscribers rather than blocking
		}
	}
}

// Close closes the emitter and all subscriber channels.
func (e *EventEmitter) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return
	}
	e.closed = true

	for _, ch := range e.subscribers {
		close(ch)
	}
	e.subscribers = nil
}
