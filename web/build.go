// ABOUTME: Build run types and SSE event formatting for the attractor pipeline runner.
// ABOUTME: Provides BuildRun for tracking active builds and RunState for pipeline lifecycle.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

// RunState tracks the lifecycle state of a pipeline run within the web layer.
// It mirrors attractor.RunState fields relevant to the UI.
type RunState struct {
	ID             string    `json:"id"`
	Status         string    `json:"status"` // "running", "completed", "failed", "cancelled"
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CurrentNode    string    `json:"current_node"`
	CompletedNodes []string  `json:"completed_nodes"`
	Error          string    `json:"error,omitempty"`
}

// BuildRun holds all state for an active build, including the cancellation
// context, SSE event channel, and current RunState.
type BuildRun struct {
	State  *RunState
	Events chan SSEEvent
	Cancel context.CancelFunc
	Ctx    context.Context
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
