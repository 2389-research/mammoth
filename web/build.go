// ABOUTME: Build run types and SSE event formatting for the attractor pipeline runner.
// ABOUTME: Provides BuildRun for tracking active builds and RunState for pipeline lifecycle.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

// RunState tracks the lifecycle state of a pipeline run within the web layer.
// It mirrors attractor.RunState fields relevant to the UI.
type RunState struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"` // "running", "completed", "failed", "cancelled"
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CurrentNode    string     `json:"current_node"`
	CompletedNodes []string   `json:"completed_nodes"`
	Error          string     `json:"error,omitempty"`
}

// BuildRun holds all state for an active build, including the cancellation
// context, SSE event channel, and current RunState.
type BuildRun struct {
	State  *RunState
	Events chan SSEEvent
	Cancel context.CancelFunc
	Ctx    context.Context

	mu          sync.Mutex
	subscribers map[int]chan SSEEvent
	nextSubID   int
	startOnce   sync.Once
	closed      bool
	history     []SSEEvent
}

// EnsureFanoutStarted starts a background broadcaster that fans Events out to
// all subscribers. Safe to call multiple times.
func (r *BuildRun) EnsureFanoutStarted() {
	r.startOnce.Do(func() {
		if r.Events == nil {
			r.Events = make(chan SSEEvent, 100)
		}
		go func() {
			for evt := range r.Events {
				r.mu.Lock()
				r.history = append(r.history, evt)
				if len(r.history) > 300 {
					r.history = r.history[len(r.history)-300:]
				}
				for _, ch := range r.subscribers {
					select {
					case ch <- evt:
					default:
					}
				}
				r.mu.Unlock()
			}

			r.mu.Lock()
			r.closed = true
			for id, ch := range r.subscribers {
				close(ch)
				delete(r.subscribers, id)
			}
			r.mu.Unlock()
		}()
	})
}

// HistorySnapshot returns a copy of buffered recent events for replay.
func (r *BuildRun) HistorySnapshot() []SSEEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SSEEvent, len(r.history))
	copy(out, r.history)
	return out
}

// Subscribe registers a subscriber channel that receives all future events.
// The returned function unsubscribes and closes the channel.
func (r *BuildRun) Subscribe() (<-chan SSEEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ch := make(chan SSEEvent, 128)
	if r.closed {
		close(ch)
		return ch, func() {}
	}
	if r.subscribers == nil {
		r.subscribers = make(map[int]chan SSEEvent)
	}
	id := r.nextSubID
	r.nextSubID++
	r.subscribers[id] = ch

	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if sub, ok := r.subscribers[id]; ok {
			close(sub)
			delete(r.subscribers, id)
		}
	}
}

// SubscribeWithHistory atomically snapshots buffered events and subscribes for
// future events to avoid replay/stream duplication races.
func (r *BuildRun) SubscribeWithHistory() ([]SSEEvent, <-chan SSEEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()

	history := make([]SSEEvent, len(r.history))
	copy(history, r.history)

	ch := make(chan SSEEvent, 128)
	if r.closed {
		close(ch)
		return history, ch, func() {}
	}
	if r.subscribers == nil {
		r.subscribers = make(map[int]chan SSEEvent)
	}
	id := r.nextSubID
	r.nextSubID++
	r.subscribers[id] = ch

	return history, ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if sub, ok := r.subscribers[id]; ok {
			close(sub)
			delete(r.subscribers, id)
		}
	}
}

// SSEEvent represents a server-sent event ready for formatting and transmission.
type SSEEvent struct {
	Event string // event type (e.g. "pipeline.started", "stage.completed")
	Data  string // JSON-encoded event data
}

// Format renders the SSEEvent as a properly formatted SSE message string.
// The format follows the SSE spec: "event: <type>\ndata: <data>\n\n".
func (e SSEEvent) Format() string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", e.Event, e.Data)
}

// engineEventToSSE converts an attractor.EngineEvent into an SSEEvent suitable
// for streaming to the browser.
func engineEventToSSE(evt attractor.EngineEvent) SSEEvent {
	data := map[string]any{
		"timestamp": evt.Timestamp.Format(time.RFC3339),
	}
	if evt.NodeID != "" {
		data["node_id"] = evt.NodeID
	}
	if evt.Data != nil {
		for k, v := range evt.Data {
			data[k] = v
		}
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		jsonData = []byte(`{"error":"failed to marshal event"}`)
	}

	return SSEEvent{
		Event: string(evt.Type),
		Data:  string(jsonData),
	}
}
