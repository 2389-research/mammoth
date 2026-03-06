// ABOUTME: Checkpoint serialization for persisting execution state to disk.
// ABOUTME: Supports JSON save/load for resuming pipeline runs from a known point.
package attractor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Checkpoint is a serializable snapshot of execution state.
type Checkpoint struct {
	Timestamp      time.Time      `json:"timestamp"`
	CurrentNode    string         `json:"current_node"`
	CompletedNodes []string       `json:"completed_nodes"`
	NodeRetries    map[string]int `json:"node_retries"`
	ContextValues  map[string]any `json:"context_values"`
	Logs           []string       `json:"logs"`
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

// Save serializes the checkpoint to JSON and writes it to the given path
// using an atomic write: data is written to a temp file in the same directory,
// then renamed into place. This prevents a partial file if the process crashes
// mid-write.
func (cp *Checkpoint) Save(path string) error {
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".checkpoint-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Restore 0644 permissions (CreateTemp uses 0600)
	if err := os.Chmod(tmpPath, 0644); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
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
