// ABOUTME: Query API for the append-only event log stored in RunStateStore.
// ABOUTME: Provides filtering, pagination, counting, tailing, and summarization of EngineEvents.
package attractor

import (
	"time"
)

// EventFilter specifies criteria for filtering engine events from a run's event log.
type EventFilter struct {
	Types  []EngineEventType // filter by event type(s); empty means all types
	NodeID string            // filter by specific node; empty means all nodes
	Since  *time.Time        // events at or after this time; nil means no lower bound
	Until  *time.Time        // events at or before this time; nil means no upper bound
	Limit  int               // max results; 0 means unlimited
	Offset int               // skip first N results after filtering
}

// EventQuery defines the interface for querying engine events from a run.
type EventQuery interface {
	QueryEvents(runID string, filter EventFilter) ([]EngineEvent, error)
	CountEvents(runID string, filter EventFilter) (int, error)
	TailEvents(runID string, n int) ([]EngineEvent, error)
	SummarizeEvents(runID string) (*EventSummary, error)
}

// EventSummary holds aggregate statistics about a run's events.
type EventSummary struct {
	TotalEvents int
	ByType      map[EngineEventType]int
	ByNode      map[string]int
	FirstEvent  *time.Time
	LastEvent   *time.Time
}

// FSEventQuery implements EventQuery using an FSRunStateStore as its backing store.
type FSEventQuery struct {
	store *FSRunStateStore
}

// Compile-time check that FSEventQuery implements EventQuery.
var _ EventQuery = (*FSEventQuery)(nil)

// NewFSEventQuery creates a new FSEventQuery backed by the given FSRunStateStore.
func NewFSEventQuery(store *FSRunStateStore) *FSEventQuery {
	return &FSEventQuery{store: store}
}

// QueryEvents returns events from the given run matching the filter criteria.
// Events are loaded from the JSONL file and filtered in memory.
func (q *FSEventQuery) QueryEvents(runID string, filter EventFilter) ([]EngineEvent, error) {
	allEvents, err := q.loadEvents(runID)
	if err != nil {
		return nil, err
	}

	filtered := applyFilter(allEvents, filter)
	paginated := applyPagination(filtered, filter.Offset, filter.Limit)

	return paginated, nil
}

// CountEvents returns the count of events matching the filter criteria.
// Pagination (Limit/Offset) is ignored for counting purposes.
func (q *FSEventQuery) CountEvents(runID string, filter EventFilter) (int, error) {
	allEvents, err := q.loadEvents(runID)
	if err != nil {
		return 0, err
	}

	filtered := applyFilter(allEvents, filter)
	return len(filtered), nil
}

// TailEvents returns the last n events from the run. If there are fewer than n
// events, all events are returned.
func (q *FSEventQuery) TailEvents(runID string, n int) ([]EngineEvent, error) {
	allEvents, err := q.loadEvents(runID)
	if err != nil {
		return nil, err
	}

	if n <= 0 {
		return []EngineEvent{}, nil
	}

	if n >= len(allEvents) {
		return allEvents, nil
	}

	return allEvents[len(allEvents)-n:], nil
}

// SummarizeEvents produces aggregate statistics about a run's event log.
func (q *FSEventQuery) SummarizeEvents(runID string) (*EventSummary, error) {
	allEvents, err := q.loadEvents(runID)
	if err != nil {
		return nil, err
	}

	summary := &EventSummary{
		TotalEvents: len(allEvents),
		ByType:      make(map[EngineEventType]int),
		ByNode:      make(map[string]int),
	}

	if len(allEvents) == 0 {
		return summary, nil
	}

	for i, evt := range allEvents {
		summary.ByType[evt.Type]++
		summary.ByNode[evt.NodeID]++

		ts := evt.Timestamp
		if i == 0 || ts.Before(*summary.FirstEvent) {
			t := ts
			summary.FirstEvent = &t
		}
		if i == 0 || ts.After(*summary.LastEvent) {
			t := ts
			summary.LastEvent = &t
		}
	}

	return summary, nil
}

// loadEvents reads all events for a run from the backing store.
func (q *FSEventQuery) loadEvents(runID string) ([]EngineEvent, error) {
	state, err := q.store.Get(runID)
	if err != nil {
		return nil, err
	}
	return state.Events, nil
}

// applyFilter returns only the events that match all specified filter criteria.
// An empty filter matches all events.
func applyFilter(events []EngineEvent, filter EventFilter) []EngineEvent {
	result := make([]EngineEvent, 0, len(events))

	for _, evt := range events {
		if !matchesFilter(evt, filter) {
			continue
		}
		result = append(result, evt)
	}

	return result
}

// matchesFilter checks whether a single event matches all filter criteria.
func matchesFilter(evt EngineEvent, filter EventFilter) bool {
	// Filter by type
	if len(filter.Types) > 0 {
		found := false
		for _, t := range filter.Types {
			if evt.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Filter by node ID
	if filter.NodeID != "" && evt.NodeID != filter.NodeID {
		return false
	}

	// Filter by time range
	if filter.Since != nil && evt.Timestamp.Before(*filter.Since) {
		return false
	}
	if filter.Until != nil && evt.Timestamp.After(*filter.Until) {
		return false
	}

	return true
}

// applyPagination applies offset and limit to a slice of events.
func applyPagination(events []EngineEvent, offset, limit int) []EngineEvent {
	if offset > 0 {
		if offset >= len(events) {
			return []EngineEvent{}
		}
		events = events[offset:]
	}

	if limit > 0 && limit < len(events) {
		events = events[:limit]
	}

	return events
}
