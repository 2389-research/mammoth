// ABOUTME: RunRegistry tracks active pipeline runs in memory.
// ABOUTME: Thread-safe via RWMutex. Each run gets a unique ID and an event buffer.
package mcp

import (
	"sync"
	"time"
)

// RunRegistry manages active pipeline runs in memory.
type RunRegistry struct {
	runs map[string]*ActiveRun
	mu   sync.RWMutex
}

// NewRunRegistry creates an empty registry.
func NewRunRegistry() *RunRegistry {
	return &RunRegistry{
		runs: make(map[string]*ActiveRun),
	}
}

// Create registers a new run with the given DOT source and config.
func (r *RunRegistry) Create(source string, config RunConfig) *ActiveRun {
	id := randomHex(8)
	run := &ActiveRun{
		ID:             id,
		Status:         StatusRunning,
		Source:         source,
		Config:         config,
		CompletedNodes: make([]string, 0),
		EventBuffer:    make([]RunEvent, 0, maxEventBuffer),
		CreatedAt:      time.Now(),
		answerCh:       make(chan string, 1),
	}
	r.mu.Lock()
	r.runs[id] = run
	r.mu.Unlock()
	return run
}

// Get returns the run with the given ID, or false if not found.
func (r *RunRegistry) Get(id string) (*ActiveRun, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[id]
	return run, ok
}

// List returns all runs.
func (r *RunRegistry) List() []*ActiveRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runs := make([]*ActiveRun, 0, len(r.runs))
	for _, run := range r.runs {
		runs = append(runs, run)
	}
	return runs
}
