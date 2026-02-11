// ABOUTME: Tests for the EventQuery interface and FSEventQuery implementation.
// ABOUTME: Covers filtering by type, node, time range, pagination, counting, tail, and summarization.
package attractor

import (
	"testing"
	"time"
)

// --- Helpers ---

// setupEventQuery creates a FSRunStateStore with a run pre-populated with events and returns the query, run ID, and the events.
func setupEventQuery(t *testing.T) (*FSEventQuery, string, []EngineEvent) {
	t.Helper()
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)

	events := []EngineEvent{
		{Type: EventPipelineStarted, NodeID: "", Data: map[string]any{"pipeline": "test"}, Timestamp: baseTime},
		{Type: EventStageStarted, NodeID: "node_a", Data: map[string]any{"step": 1}, Timestamp: baseTime.Add(1 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_a", Data: map[string]any{"step": 1}, Timestamp: baseTime.Add(2 * time.Minute)},
		{Type: EventStageStarted, NodeID: "node_b", Data: map[string]any{"step": 2}, Timestamp: baseTime.Add(3 * time.Minute)},
		{Type: EventStageRetrying, NodeID: "node_b", Data: map[string]any{"attempt": 2}, Timestamp: baseTime.Add(4 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_b", Data: map[string]any{"step": 2}, Timestamp: baseTime.Add(5 * time.Minute)},
		{Type: EventCheckpointSaved, NodeID: "node_b", Data: nil, Timestamp: baseTime.Add(6 * time.Minute)},
		{Type: EventPipelineCompleted, NodeID: "", Data: nil, Timestamp: baseTime.Add(7 * time.Minute)},
	}

	for _, evt := range events {
		if err := store.AddEvent(state.ID, evt); err != nil {
			t.Fatalf("AddEvent failed: %v", err)
		}
	}

	query := NewFSEventQuery(store)
	return query, state.ID, events
}

// --- EngineEvent Timestamp field ---

func TestEngineEventHasTimestamp(t *testing.T) {
	now := time.Now()
	evt := EngineEvent{
		Type:      EventPipelineStarted,
		NodeID:    "test",
		Data:      nil,
		Timestamp: now,
	}
	if evt.Timestamp.IsZero() {
		t.Error("expected Timestamp to be set, got zero value")
	}
	if !evt.Timestamp.Equal(now) {
		t.Errorf("Timestamp mismatch: got %v, want %v", evt.Timestamp, now)
	}
}

// --- QueryEvents tests ---

func TestQueryEventsNoFilter(t *testing.T) {
	query, runID, events := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != len(events) {
		t.Errorf("expected %d events, got %d", len(events), len(results))
	}
}

func TestQueryEventsFilterByType(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{
		Types: []EngineEventType{EventStageStarted},
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 stage.started events, got %d", len(results))
	}
	for _, r := range results {
		if r.Type != EventStageStarted {
			t.Errorf("expected type %q, got %q", EventStageStarted, r.Type)
		}
	}
}

func TestQueryEventsFilterByMultipleTypes(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{
		Types: []EngineEventType{EventPipelineStarted, EventPipelineCompleted},
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 pipeline events, got %d", len(results))
	}
}

func TestQueryEventsFilterByNodeID(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{
		NodeID: "node_b",
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("expected 4 events for node_b, got %d", len(results))
	}
	for _, r := range results {
		if r.NodeID != "node_b" {
			t.Errorf("expected NodeID 'node_b', got %q", r.NodeID)
		}
	}
}

func TestQueryEventsFilterBySince(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	since := time.Date(2025, 6, 15, 10, 5, 0, 0, time.UTC)
	results, err := query.QueryEvents(runID, EventFilter{
		Since: &since,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	// Events at 5min, 6min, 7min = 3 events
	if len(results) != 3 {
		t.Errorf("expected 3 events since %v, got %d", since, len(results))
	}
	for _, r := range results {
		if r.Timestamp.Before(since) {
			t.Errorf("event timestamp %v is before since %v", r.Timestamp, since)
		}
	}
}

func TestQueryEventsFilterByUntil(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	until := time.Date(2025, 6, 15, 10, 2, 0, 0, time.UTC)
	results, err := query.QueryEvents(runID, EventFilter{
		Until: &until,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	// Events at 0min, 1min, 2min = 3 events
	if len(results) != 3 {
		t.Errorf("expected 3 events until %v, got %d", until, len(results))
	}
	for _, r := range results {
		if r.Timestamp.After(until) {
			t.Errorf("event timestamp %v is after until %v", r.Timestamp, until)
		}
	}
}

func TestQueryEventsFilterByTimeRange(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	since := time.Date(2025, 6, 15, 10, 2, 0, 0, time.UTC)
	until := time.Date(2025, 6, 15, 10, 5, 0, 0, time.UTC)
	results, err := query.QueryEvents(runID, EventFilter{
		Since: &since,
		Until: &until,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	// Events at 2min, 3min, 4min, 5min = 4 events
	if len(results) != 4 {
		t.Errorf("expected 4 events in range, got %d", len(results))
	}
}

func TestQueryEventsFilterByLimit(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{
		Limit: 3,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 events with limit, got %d", len(results))
	}
	// Should return the first 3 events
	if results[0].Type != EventPipelineStarted {
		t.Errorf("expected first event type %q, got %q", EventPipelineStarted, results[0].Type)
	}
}

func TestQueryEventsFilterByOffset(t *testing.T) {
	query, runID, events := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{
		Offset: 5,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != len(events)-5 {
		t.Errorf("expected %d events with offset 5, got %d", len(events)-5, len(results))
	}
}

func TestQueryEventsFilterByLimitAndOffset(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{
		Limit:  2,
		Offset: 2,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 events with limit=2 offset=2, got %d", len(results))
	}
	// Should be events at index 2 and 3 (stage.completed for node_a, stage.started for node_b)
	if results[0].Type != EventStageCompleted {
		t.Errorf("expected first result type %q, got %q", EventStageCompleted, results[0].Type)
	}
	if results[0].NodeID != "node_a" {
		t.Errorf("expected first result NodeID 'node_a', got %q", results[0].NodeID)
	}
	if results[1].Type != EventStageStarted {
		t.Errorf("expected second result type %q, got %q", EventStageStarted, results[1].Type)
	}
	if results[1].NodeID != "node_b" {
		t.Errorf("expected second result NodeID 'node_b', got %q", results[1].NodeID)
	}
}

func TestQueryEventsCombinedFilters(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{
		Types:  []EngineEventType{EventStageStarted, EventStageCompleted},
		NodeID: "node_b",
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	// node_b has: stage.started, stage.retrying, stage.completed
	// Filtered to started+completed = 2
	if len(results) != 2 {
		t.Errorf("expected 2 events for node_b with type filter, got %d", len(results))
	}
}

func TestQueryEventsOffsetBeyondLength(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	results, err := query.QueryEvents(runID, EventFilter{
		Offset: 100,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 events with offset beyond length, got %d", len(results))
	}
}

func TestQueryEventsNonexistentRun(t *testing.T) {
	store := newTestStore(t)
	query := NewFSEventQuery(store)

	_, err := query.QueryEvents("nonexistent", EventFilter{})
	if err == nil {
		t.Fatal("expected error for nonexistent run, got nil")
	}
}

// --- CountEvents tests ---

func TestCountEventsNoFilter(t *testing.T) {
	query, runID, events := setupEventQuery(t)

	count, err := query.CountEvents(runID, EventFilter{})
	if err != nil {
		t.Fatalf("CountEvents failed: %v", err)
	}
	if count != len(events) {
		t.Errorf("expected count %d, got %d", len(events), count)
	}
}

func TestCountEventsWithFilter(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	count, err := query.CountEvents(runID, EventFilter{
		Types: []EngineEventType{EventStageStarted},
	})
	if err != nil {
		t.Fatalf("CountEvents failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestCountEventsMatchesQueryEventsLength(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	filter := EventFilter{
		Types:  []EngineEventType{EventStageStarted, EventStageCompleted},
		NodeID: "node_a",
	}

	results, err := query.QueryEvents(runID, filter)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	count, err := query.CountEvents(runID, filter)
	if err != nil {
		t.Fatalf("CountEvents failed: %v", err)
	}

	if count != len(results) {
		t.Errorf("CountEvents (%d) does not match QueryEvents length (%d)", count, len(results))
	}
}

// --- TailEvents tests ---

func TestTailEvents(t *testing.T) {
	query, runID, events := setupEventQuery(t)

	results, err := query.TailEvents(runID, 3)
	if err != nil {
		t.Fatalf("TailEvents failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 tail events, got %d", len(results))
	}
	// Should be the last 3 events
	if results[0].Type != events[5].Type {
		t.Errorf("expected tail[0] type %q, got %q", events[5].Type, results[0].Type)
	}
	if results[1].Type != events[6].Type {
		t.Errorf("expected tail[1] type %q, got %q", events[6].Type, results[1].Type)
	}
	if results[2].Type != events[7].Type {
		t.Errorf("expected tail[2] type %q, got %q", events[7].Type, results[2].Type)
	}
}

func TestTailEventsMoreThanAvailable(t *testing.T) {
	query, runID, events := setupEventQuery(t)

	results, err := query.TailEvents(runID, 100)
	if err != nil {
		t.Fatalf("TailEvents failed: %v", err)
	}
	if len(results) != len(events) {
		t.Errorf("expected %d events (all), got %d", len(events), len(results))
	}
}

func TestTailEventsZero(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	results, err := query.TailEvents(runID, 0)
	if err != nil {
		t.Fatalf("TailEvents failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 tail events, got %d", len(results))
	}
}

// --- SummarizeEvents tests ---

func TestSummarizeEvents(t *testing.T) {
	query, runID, _ := setupEventQuery(t)

	summary, err := query.SummarizeEvents(runID)
	if err != nil {
		t.Fatalf("SummarizeEvents failed: %v", err)
	}

	if summary.TotalEvents != 8 {
		t.Errorf("expected TotalEvents=8, got %d", summary.TotalEvents)
	}

	// Check ByType counts
	expectedByType := map[EngineEventType]int{
		EventPipelineStarted:   1,
		EventPipelineCompleted: 1,
		EventStageStarted:      2,
		EventStageCompleted:    2,
		EventStageRetrying:     1,
		EventCheckpointSaved:   1,
	}
	for evtType, expectedCount := range expectedByType {
		if summary.ByType[evtType] != expectedCount {
			t.Errorf("ByType[%q]: expected %d, got %d", evtType, expectedCount, summary.ByType[evtType])
		}
	}

	// Check ByNode counts (empty NodeID events should be counted under "")
	if summary.ByNode["node_a"] != 2 {
		t.Errorf("ByNode[node_a]: expected 2, got %d", summary.ByNode["node_a"])
	}
	if summary.ByNode["node_b"] != 4 {
		t.Errorf("ByNode[node_b]: expected 4, got %d", summary.ByNode["node_b"])
	}
	if summary.ByNode[""] != 2 {
		t.Errorf("ByNode[\"\"]: expected 2, got %d", summary.ByNode[""])
	}

	// Check first/last event times
	if summary.FirstEvent == nil {
		t.Fatal("expected non-nil FirstEvent")
	}
	if summary.LastEvent == nil {
		t.Fatal("expected non-nil LastEvent")
	}

	expectedFirst := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	expectedLast := time.Date(2025, 6, 15, 10, 7, 0, 0, time.UTC)

	if !summary.FirstEvent.Equal(expectedFirst) {
		t.Errorf("FirstEvent: expected %v, got %v", expectedFirst, *summary.FirstEvent)
	}
	if !summary.LastEvent.Equal(expectedLast) {
		t.Errorf("LastEvent: expected %v, got %v", expectedLast, *summary.LastEvent)
	}
}

func TestSummarizeEventsEmptyRun(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	query := NewFSEventQuery(store)
	summary, err := query.SummarizeEvents(state.ID)
	if err != nil {
		t.Fatalf("SummarizeEvents failed: %v", err)
	}

	if summary.TotalEvents != 0 {
		t.Errorf("expected TotalEvents=0, got %d", summary.TotalEvents)
	}
	if summary.FirstEvent != nil {
		t.Errorf("expected nil FirstEvent, got %v", summary.FirstEvent)
	}
	if summary.LastEvent != nil {
		t.Errorf("expected nil LastEvent, got %v", summary.LastEvent)
	}
	if len(summary.ByType) != 0 {
		t.Errorf("expected empty ByType, got %v", summary.ByType)
	}
	if len(summary.ByNode) != 0 {
		t.Errorf("expected empty ByNode, got %v", summary.ByNode)
	}
}

// --- Empty run tests ---

func TestQueryEventsEmptyRun(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	query := NewFSEventQuery(store)
	results, err := query.QueryEvents(state.ID, EventFilter{})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 events for empty run, got %d", len(results))
	}
}

func TestCountEventsEmptyRun(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	query := NewFSEventQuery(store)
	count, err := query.CountEvents(state.ID, EventFilter{})
	if err != nil {
		t.Fatalf("CountEvents failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0 for empty run, got %d", count)
	}
}

func TestTailEventsEmptyRun(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	query := NewFSEventQuery(store)
	results, err := query.TailEvents(state.ID, 5)
	if err != nil {
		t.Fatalf("TailEvents failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 tail events for empty run, got %d", len(results))
	}
}
