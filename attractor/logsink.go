// ABOUTME: Defines the LogSink interface for structured event storage with query, retention, and indexing.
// ABOUTME: Provides FSLogSink, a filesystem-backed implementation wrapping FSRunStateStore and FSEventQuery.
package attractor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// LogSink defines the interface for structured event log storage with query,
// retention, and lifecycle management capabilities.
type LogSink interface {
	// Append writes an event to the log for the given run.
	Append(runID string, event EngineEvent) error

	// Query returns events matching the filter, along with the total count of
	// matching events (before pagination). This allows callers to paginate while
	// knowing the full result set size.
	Query(runID string, filter EventFilter) ([]EngineEvent, int, error)

	// Tail returns the last n events from the run's event log.
	Tail(runID string, n int) ([]EngineEvent, error)

	// Summarize returns aggregate statistics for a run's event log.
	Summarize(runID string) (*EventSummary, error)

	// Prune deletes all runs whose start time is older than the given duration ago.
	// Returns the number of runs pruned.
	Prune(olderThan time.Duration) (int, error)

	// Close releases any resources held by the sink.
	Close() error
}

// RunIndexEntry holds metadata about a single run for fast lookup without
// scanning the full directory tree.
type RunIndexEntry struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	StartTime  time.Time `json:"start_time"`
	EventCount int       `json:"event_count"`
}

// RunIndex is the top-level structure persisted as index.json in the store root.
// It maps run IDs to their metadata for fast enumeration and filtering.
type RunIndex struct {
	Runs    map[string]RunIndexEntry `json:"runs"`
	Updated time.Time                `json:"updated"`
}

// RetentionConfig specifies how long and how many runs to retain.
type RetentionConfig struct {
	MaxAge  time.Duration // prune runs older than this; 0 means no age limit
	MaxRuns int           // keep at most this many runs; 0 means unlimited
}

// PruneLoop runs periodic retention cleanup until ctx is cancelled. It prunes
// by MaxAge each interval. This blocks until the context is done.
func (rc RetentionConfig) PruneLoop(ctx context.Context, sink LogSink, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once immediately before entering the tick loop
	if rc.MaxAge > 0 {
		_, _ = sink.Prune(rc.MaxAge)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if rc.MaxAge > 0 {
				_, _ = sink.Prune(rc.MaxAge)
			}
		}
	}
}

// PruneByMaxRuns removes the oldest runs that exceed the MaxRuns limit.
// Runs are sorted by start time; the oldest beyond the limit are deleted.
// Returns the number of runs pruned.
func (rc RetentionConfig) PruneByMaxRuns(sink LogSink) (int, error) {
	fsSink, ok := sink.(*FSLogSink)
	if !ok {
		return 0, fmt.Errorf("PruneByMaxRuns requires an *FSLogSink")
	}

	if rc.MaxRuns <= 0 {
		return 0, nil
	}

	index, err := fsSink.loadIndex()
	if err != nil {
		return 0, fmt.Errorf("load index: %w", err)
	}

	if len(index.Runs) <= rc.MaxRuns {
		return 0, nil
	}

	// Sort runs by start time ascending (oldest first)
	entries := make([]RunIndexEntry, 0, len(index.Runs))
	for _, entry := range index.Runs {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].StartTime.Before(entries[j].StartTime)
	})

	// Determine how many to prune
	toPrune := len(entries) - rc.MaxRuns
	if toPrune <= 0 {
		return 0, nil
	}

	pruned := 0
	for _, entry := range entries[:toPrune] {
		if err := fsSink.deleteRun(entry.ID); err != nil {
			continue
		}
		pruned++
	}

	return pruned, nil
}

// FSLogSink is a filesystem-backed LogSink that wraps FSRunStateStore for storage
// and FSEventQuery for querying. It maintains a JSON index file for fast run enumeration.
type FSLogSink struct {
	store   *FSRunStateStore
	query   *FSEventQuery
	baseDir string
	mu      sync.Mutex
	closed  bool
}

// Compile-time check that FSLogSink implements LogSink.
var _ LogSink = (*FSLogSink)(nil)

// NewFSLogSink creates a new filesystem-backed LogSink rooted at baseDir.
// The index file is created if it does not already exist.
func NewFSLogSink(baseDir string) (*FSLogSink, error) {
	store, err := NewFSRunStateStore(baseDir)
	if err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}

	sink := &FSLogSink{
		store:   store,
		query:   NewFSEventQuery(store),
		baseDir: baseDir,
	}

	// Ensure index.json exists
	if err := sink.ensureIndex(); err != nil {
		return nil, fmt.Errorf("ensure index: %w", err)
	}

	return sink, nil
}

// Append writes an event to the run's event log and updates the index.
func (s *FSLogSink) Append(runID string, event EngineEvent) error {
	if err := s.store.AddEvent(runID, event); err != nil {
		return fmt.Errorf("append event: %w", err)
	}

	// Update the index entry for this run
	if err := s.updateIndexEntry(runID); err != nil {
		return fmt.Errorf("update index: %w", err)
	}

	return nil
}

// Query returns events matching the filter and the total count of matching events
// (before pagination). The total reflects the full filtered result set, while the
// returned slice respects Limit and Offset.
func (s *FSLogSink) Query(runID string, filter EventFilter) ([]EngineEvent, int, error) {
	// Get total count using a filter without pagination
	countFilter := EventFilter{
		Types:  filter.Types,
		NodeID: filter.NodeID,
		Since:  filter.Since,
		Until:  filter.Until,
	}
	total, err := s.query.CountEvents(runID, countFilter)
	if err != nil {
		return nil, 0, fmt.Errorf("count events: %w", err)
	}

	events, err := s.query.QueryEvents(runID, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("query events: %w", err)
	}

	return events, total, nil
}

// Tail returns the last n events from the run's event log.
func (s *FSLogSink) Tail(runID string, n int) ([]EngineEvent, error) {
	return s.query.TailEvents(runID, n)
}

// Summarize returns aggregate statistics for a run's event log.
func (s *FSLogSink) Summarize(runID string) (*EventSummary, error) {
	return s.query.SummarizeEvents(runID)
}

// Prune deletes all runs whose start time is older than the given duration ago.
// It removes both the run directory and its entry from the index.
// Returns the number of runs pruned.
func (s *FSLogSink) Prune(olderThan time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexUnlocked()
	if err != nil {
		return 0, fmt.Errorf("load index: %w", err)
	}

	cutoff := time.Now().Add(-olderThan)
	pruned := 0

	for runID, entry := range index.Runs {
		if entry.StartTime.Before(cutoff) {
			if err := s.deleteRunUnlocked(runID); err != nil {
				continue
			}
			delete(index.Runs, runID)
			pruned++
		}
	}

	if pruned > 0 {
		index.Updated = time.Now()
		if err := s.saveIndexUnlocked(index); err != nil {
			return pruned, fmt.Errorf("save index after prune: %w", err)
		}
	}

	return pruned, nil
}

// Close releases any resources held by the sink. Calling Close multiple times is safe.
func (s *FSLogSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ListRuns returns all run index entries from the index file.
func (s *FSLogSink) ListRuns() ([]RunIndexEntry, error) {
	index, err := s.loadIndex()
	if err != nil {
		return nil, fmt.Errorf("load index: %w", err)
	}

	entries := make([]RunIndexEntry, 0, len(index.Runs))
	for _, entry := range index.Runs {
		entries = append(entries, entry)
	}

	return entries, nil
}

// ensureIndex creates the index.json file if it does not already exist.
func (s *FSLogSink) ensureIndex() error {
	indexPath := filepath.Join(s.baseDir, "index.json")
	if _, err := os.Stat(indexPath); err == nil {
		return nil
	}

	index := &RunIndex{
		Runs:    make(map[string]RunIndexEntry),
		Updated: time.Now(),
	}

	return s.saveIndex(index)
}

// loadIndex reads and parses the index.json file. Thread-safe.
func (s *FSLogSink) loadIndex() (*RunIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadIndexUnlocked()
}

// loadIndexUnlocked reads and parses the index.json file. Caller must hold the mutex.
func (s *FSLogSink) loadIndexUnlocked() (*RunIndex, error) {
	indexPath := filepath.Join(s.baseDir, "index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &RunIndex{Runs: make(map[string]RunIndexEntry)}, nil
		}
		return nil, fmt.Errorf("read index: %w", err)
	}

	var index RunIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}

	if index.Runs == nil {
		index.Runs = make(map[string]RunIndexEntry)
	}

	return &index, nil
}

// saveIndex writes the index to index.json atomically. Thread-safe.
func (s *FSLogSink) saveIndex(index *RunIndex) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveIndexUnlocked(index)
}

// saveIndexUnlocked writes the index to index.json atomically. Caller must hold the mutex.
func (s *FSLogSink) saveIndexUnlocked(index *RunIndex) error {
	return writeJSONAtomic(filepath.Join(s.baseDir, "index.json"), index)
}

// updateIndexEntry refreshes the index entry for a specific run based on current state.
func (s *FSLogSink) updateIndexEntry(runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	index, err := s.loadIndexUnlocked()
	if err != nil {
		return err
	}

	state, err := s.store.Get(runID)
	if err != nil {
		return fmt.Errorf("get run state: %w", err)
	}

	index.Runs[runID] = RunIndexEntry{
		ID:         runID,
		Status:     state.Status,
		StartTime:  state.StartedAt,
		EventCount: len(state.Events),
	}
	index.Updated = time.Now()

	return s.saveIndexUnlocked(index)
}

// deleteRun removes a run's directory from the filesystem. Thread-safe.
func (s *FSLogSink) deleteRun(runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteRunUnlocked(runID)
}

// deleteRunUnlocked removes a run's directory from the filesystem. Caller must hold the mutex.
func (s *FSLogSink) deleteRunUnlocked(runID string) error {
	runDir := filepath.Join(s.baseDir, runID)
	return os.RemoveAll(runDir)
}
