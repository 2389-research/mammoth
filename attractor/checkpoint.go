// ABOUTME: Checkpoint serialization for persisting execution state to disk.
// ABOUTME: Supports JSON save/load for resuming pipeline runs from a known point.
package attractor

import (
	"encoding/json"
	"os"
	"time"
)

// Checkpoint is a serializable snapshot of execution state.
type Checkpoint struct {
	Timestamp      time.Time         `json:"timestamp"`
	CurrentNode    string            `json:"current_node"`
	CompletedNodes []string          `json:"completed_nodes"`
	NodeRetries    map[string]int    `json:"node_retries"`
	ContextValues  map[string]any    `json:"context_values"`
	Logs           []string          `json:"logs"`
}

// NewCheckpoint creates a checkpoint from the current execution state.
func NewCheckpoint(ctx *Context, currentNode string, completedNodes []string, nodeRetries map[string]int) *Checkpoint {
	return &Checkpoint{
		Timestamp:      time.Now(),
		CurrentNode:    currentNode,
		CompletedNodes: completedNodes,
		NodeRetries:    nodeRetries,
		ContextValues:  ctx.Snapshot(),
		Logs:           ctx.Logs(),
	}
}

// Save serializes the checkpoint to JSON and writes it to the given path.
func (cp *Checkpoint) Save(path string) error {
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadCheckpoint deserializes a checkpoint from JSON at the given path.
func LoadCheckpoint(path string) (*Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}
