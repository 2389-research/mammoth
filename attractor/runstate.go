// ABOUTME: Defines RunState types and the RunStateStore interface for tracking pipeline run lifecycle.
// ABOUTME: Provides run ID generation using crypto/rand and the core data model for persistent run tracking.
package attractor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// RunState represents the full state of a single pipeline run.
type RunState struct {
	ID             string         `json:"id"`
	PipelineFile   string         `json:"pipeline_file"`
	Status         string         `json:"status"` // "running", "completed", "failed", "cancelled"
	Source         string         `json:"source,omitempty"`
	StartedAt      time.Time      `json:"started_at"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
	CurrentNode    string         `json:"current_node"`
	CompletedNodes []string       `json:"completed_nodes"`
	Context        map[string]any `json:"context"`
	Events         []EngineEvent  `json:"events"`
	Error          string         `json:"error,omitempty"`
}

// RunStateStore is the interface for persisting and retrieving pipeline run state.
type RunStateStore interface {
	Create(state *RunState) error
	Get(id string) (*RunState, error)
	Update(state *RunState) error
	List() ([]*RunState, error)
	AddEvent(id string, event EngineEvent) error
}

// GenerateRunID produces a random 16-character hex string (8 bytes of entropy).
func GenerateRunID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate run ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}
