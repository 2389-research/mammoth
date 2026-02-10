// ABOUTME: Append-only NDJSON event logger for pipeline execution observability.
// ABOUTME: Writes engine events to a .ndjson file and maintains a live.json status snapshot.
package attractor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ProgressEntry is a JSON-serializable record written as one line in the NDJSON log.
type ProgressEntry struct {
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	NodeID    string         `json:"node_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// LiveState represents the current pipeline execution snapshot, written to live.json
// after each event so external tools can poll for status.
type LiveState struct {
	Status     string   `json:"status"`
	ActiveNode string   `json:"active_node"`
	Completed  []string `json:"completed"`
	Failed     []string `json:"failed"`
	StartedAt  string   `json:"started_at"`
	UpdatedAt  string   `json:"updated_at"`
	EventCount int      `json:"event_count"`
}

// ProgressLogger writes engine events to an append-only NDJSON file and maintains
// a live.json snapshot reflecting current pipeline state.
type ProgressLogger struct {
	dir        string
	file       *os.File
	state      LiveState
	mu         sync.Mutex
	closed     bool
	WriteErrors int // count of write errors encountered (for diagnostics)
}

// NewProgressLogger creates a progress logger that writes to the given directory.
// It opens progress.ndjson for appending and writes an initial live.json with pending status.
func NewProgressLogger(dir string) (*ProgressLogger, error) {
	ndjsonPath := filepath.Join(dir, "progress.ndjson")
	f, err := os.OpenFile(ndjsonPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	pl := &ProgressLogger{
		dir:  dir,
		file: f,
		state: LiveState{
			Status:    "pending",
			Completed: []string{},
			Failed:    []string{},
		},
	}

	// Write initial live.json
	if err := pl.writeLiveJSON(); err != nil {
		f.Close()
		return nil, err
	}

	return pl, nil
}

// HandleEvent converts an EngineEvent to a ProgressEntry, appends it to the NDJSON
// file, updates the live state, and atomically rewrites live.json. This method
// signature matches EngineConfig.EventHandler so it can be wired directly.
func (p *ProgressLogger) HandleEvent(evt EngineEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Build the NDJSON entry
	entry := ProgressEntry{
		Timestamp: evt.Timestamp.UTC().Format(time.RFC3339),
		Type:      string(evt.Type),
		NodeID:    evt.NodeID,
		Data:      evt.Data,
	}

	// Append to the NDJSON file (best-effort: state is updated even on write failure)
	line, err := json.Marshal(entry)
	if err != nil {
		p.WriteErrors++
		fmt.Fprintf(os.Stderr, "[progress] marshal error: %v\n", err)
	} else {
		line = append(line, '\n')
		if _, err := p.file.Write(line); err != nil {
			p.WriteErrors++
			fmt.Fprintf(os.Stderr, "[progress] write error: %v\n", err)
		}
	}

	// Update live state based on event type (always, even if NDJSON write failed)
	switch evt.Type {
	case EventPipelineStarted:
		p.state.Status = "running"
		p.state.StartedAt = evt.Timestamp.UTC().Format(time.RFC3339)
	case EventStageStarted:
		p.state.ActiveNode = evt.NodeID
	case EventStageCompleted:
		p.state.Completed = append(p.state.Completed, evt.NodeID)
		p.state.ActiveNode = ""
	case EventStageFailed:
		p.state.Failed = append(p.state.Failed, evt.NodeID)
		p.state.ActiveNode = ""
	case EventPipelineCompleted:
		p.state.Status = "completed"
	case EventPipelineFailed:
		p.state.Status = "failed"
	}

	p.state.EventCount++
	p.state.UpdatedAt = now

	// Atomically rewrite live.json
	if err := p.writeLiveJSON(); err != nil {
		fmt.Fprintf(os.Stderr, "[progress] live.json write error: %v\n", err)
	}
}

// Close closes the underlying NDJSON file. After Close, HandleEvent becomes a no-op.
func (p *ProgressLogger) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return p.file.Close()
}

// State returns a copy of the current live state.
func (p *ProgressLogger) State() LiveState {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return a deep copy so callers cannot mutate internal state
	cp := p.state
	cp.Completed = make([]string, len(p.state.Completed))
	copy(cp.Completed, p.state.Completed)
	cp.Failed = make([]string, len(p.state.Failed))
	copy(cp.Failed, p.state.Failed)
	return cp
}

// writeLiveJSON atomically writes the current state to live.json.
// Caller must hold p.mu.
func (p *ProgressLogger) writeLiveJSON() error {
	return writeJSONAtomic(filepath.Join(p.dir, "live.json"), p.state)
}
