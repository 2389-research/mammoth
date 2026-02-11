// ABOUTME: Tests for the LogSink interface and FSLogSink filesystem-backed implementation.
// ABOUTME: Covers append, query, tail, summarize, retention pruning, index consistency, and Close.
package attractor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Helpers ---

// newTestLogSink creates an FSLogSink backed by a temp directory for testing.
func newTestLogSink(t *testing.T) *FSLogSink {
	t.Helper()
	dir := t.TempDir()
	sink, err := NewFSLogSink(dir)
	if err != nil {
		t.Fatalf("NewFSLogSink failed: %v", err)
	}
	return sink
}

// createTestRun creates a run in the sink's backing store and returns the run ID.
func createTestRun(t *testing.T, sink *FSLogSink, status string, startedAt time.Time) string {
	t.Helper()
	id, err := GenerateRunID()
	if err != nil {
		t.Fatalf("GenerateRunID failed: %v", err)
	}
	state := &RunState{
		ID:             id,
		PipelineFile:   "test-pipeline.dot",
		Status:         status,
		StartedAt:      startedAt,
		CurrentNode:    "start",
		CompletedNodes: []string{},
		Context:        map[string]any{"model": "test"},
		Events:         []EngineEvent{},
	}
	if err := sink.store.Create(state); err != nil {
		t.Fatalf("Create run failed: %v", err)
	}
	return id
}

// --- LogSink interface compliance ---

func TestFSLogSinkImplementsLogSink(t *testing.T) {
	var _ LogSink = (*FSLogSink)(nil)
}

// --- Append tests ---

func TestLogSinkAppend(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	runID := createTestRun(t, sink, "running", time.Now())

	evt := EngineEvent{
		Type:      EventPipelineStarted,
		NodeID:    "",
		Data:      map[string]any{"pipeline": "test"},
		Timestamp: time.Now(),
	}

	err := sink.Append(runID, evt)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Verify event was stored
	events, total, err := sink.Query(runID, EventFilter{})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventPipelineStarted {
		t.Errorf("event type mismatch: got %q, want %q", events[0].Type, EventPipelineStarted)
	}
}

func TestLogSinkAppendMultipleEvents(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	runID := createTestRun(t, sink, "running", time.Now())

	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	events := []EngineEvent{
		{Type: EventPipelineStarted, NodeID: "", Timestamp: baseTime},
		{Type: EventStageStarted, NodeID: "node_a", Timestamp: baseTime.Add(1 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_a", Timestamp: baseTime.Add(2 * time.Minute)},
		{Type: EventPipelineCompleted, NodeID: "", Timestamp: baseTime.Add(3 * time.Minute)},
	}

	for _, evt := range events {
		if err := sink.Append(runID, evt); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	results, total, err := sink.Query(runID, EventFilter{})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if total != 4 {
		t.Errorf("expected total 4, got %d", total)
	}
	if len(results) != 4 {
		t.Errorf("expected 4 events, got %d", len(results))
	}
}

func TestLogSinkAppendNonexistentRun(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	err := sink.Append("nonexistent-run", EngineEvent{Type: EventPipelineStarted, Timestamp: time.Now()})
	if err == nil {
		t.Fatal("expected error for appending to nonexistent run, got nil")
	}
}

func TestLogSinkAppendUpdatesIndex(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	startTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	runID := createTestRun(t, sink, "running", startTime)

	evt := EngineEvent{
		Type:      EventPipelineStarted,
		NodeID:    "",
		Timestamp: startTime,
	}
	if err := sink.Append(runID, evt); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Read the index file and verify the run entry was updated
	index, err := sink.loadIndex()
	if err != nil {
		t.Fatalf("loadIndex failed: %v", err)
	}

	entry, ok := index.Runs[runID]
	if !ok {
		t.Fatalf("run %q not found in index", runID)
	}
	if entry.EventCount != 1 {
		t.Errorf("expected EventCount 1, got %d", entry.EventCount)
	}
	if entry.Status != "running" {
		t.Errorf("expected Status 'running', got %q", entry.Status)
	}
}

// --- Query tests ---

func TestLogSinkQueryWithFilter(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	runID := createTestRun(t, sink, "running", time.Now())

	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	events := []EngineEvent{
		{Type: EventPipelineStarted, NodeID: "", Timestamp: baseTime},
		{Type: EventStageStarted, NodeID: "node_a", Timestamp: baseTime.Add(1 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_a", Timestamp: baseTime.Add(2 * time.Minute)},
		{Type: EventStageStarted, NodeID: "node_b", Timestamp: baseTime.Add(3 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_b", Timestamp: baseTime.Add(4 * time.Minute)},
		{Type: EventPipelineCompleted, NodeID: "", Timestamp: baseTime.Add(5 * time.Minute)},
	}
	for _, evt := range events {
		if err := sink.Append(runID, evt); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	tests := []struct {
		name      string
		filter    EventFilter
		wantCount int
		wantTotal int
	}{
		{
			name:      "no filter returns all",
			filter:    EventFilter{},
			wantCount: 6,
			wantTotal: 6,
		},
		{
			name:      "filter by type",
			filter:    EventFilter{Types: []EngineEventType{EventStageStarted}},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name:      "filter by node",
			filter:    EventFilter{NodeID: "node_a"},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name:      "filter with limit",
			filter:    EventFilter{Limit: 3},
			wantCount: 3,
			wantTotal: 6,
		},
		{
			name:      "filter with offset",
			filter:    EventFilter{Offset: 4},
			wantCount: 2,
			wantTotal: 6,
		},
		{
			name:      "filter with limit and offset",
			filter:    EventFilter{Limit: 2, Offset: 2},
			wantCount: 2,
			wantTotal: 6,
		},
		{
			name: "filter by time range",
			filter: EventFilter{
				Since: timePtr(baseTime.Add(1 * time.Minute)),
				Until: timePtr(baseTime.Add(3 * time.Minute)),
			},
			wantCount: 3,
			wantTotal: 3,
		},
		{
			name:      "combined filter",
			filter:    EventFilter{Types: []EngineEventType{EventStageStarted, EventStageCompleted}, NodeID: "node_b"},
			wantCount: 2,
			wantTotal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, total, err := sink.Query(runID, tt.filter)
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}
			if total != tt.wantTotal {
				t.Errorf("total: got %d, want %d", total, tt.wantTotal)
			}
			if len(results) != tt.wantCount {
				t.Errorf("count: got %d, want %d", len(results), tt.wantCount)
			}
		})
	}
}

func TestLogSinkQueryNonexistentRun(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	_, _, err := sink.Query("nonexistent-run", EventFilter{})
	if err == nil {
		t.Fatal("expected error for querying nonexistent run, got nil")
	}
}

// --- Tail tests ---

func TestLogSinkTail(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	runID := createTestRun(t, sink, "running", time.Now())

	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		evt := EngineEvent{
			Type:      EventStageStarted,
			NodeID:    "node",
			Data:      map[string]any{"index": i},
			Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
		}
		if err := sink.Append(runID, evt); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	tests := []struct {
		name      string
		n         int
		wantCount int
	}{
		{"last 3", 3, 3},
		{"last 10 (more than available)", 10, 5},
		{"last 0", 0, 0},
		{"last 1", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := sink.Tail(runID, tt.n)
			if err != nil {
				t.Fatalf("Tail failed: %v", err)
			}
			if len(results) != tt.wantCount {
				t.Errorf("got %d events, want %d", len(results), tt.wantCount)
			}
		})
	}
}

func TestLogSinkTailReturnsLastN(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	runID := createTestRun(t, sink, "running", time.Now())

	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		evt := EngineEvent{
			Type:      EventStageStarted,
			NodeID:    "node",
			Data:      map[string]any{"index": i},
			Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
		}
		if err := sink.Append(runID, evt); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	results, err := sink.Tail(runID, 2)
	if err != nil {
		t.Fatalf("Tail failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 events, got %d", len(results))
	}

	// Last 2 events should have indices 3 and 4
	idx3, ok := results[0].Data["index"]
	if !ok {
		t.Fatal("expected Data[index] to exist")
	}
	// JSON unmarshals numbers as float64
	if idx3.(float64) != 3 {
		t.Errorf("expected first tail event index 3, got %v", idx3)
	}
	idx4, ok := results[1].Data["index"]
	if !ok {
		t.Fatal("expected Data[index] to exist")
	}
	if idx4.(float64) != 4 {
		t.Errorf("expected second tail event index 4, got %v", idx4)
	}
}

func TestLogSinkTailNonexistentRun(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	_, err := sink.Tail("nonexistent-run", 5)
	if err == nil {
		t.Fatal("expected error for tailing nonexistent run, got nil")
	}
}

// --- Summarize tests ---

func TestLogSinkSummarize(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	runID := createTestRun(t, sink, "running", time.Now())

	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	events := []EngineEvent{
		{Type: EventPipelineStarted, NodeID: "", Timestamp: baseTime},
		{Type: EventStageStarted, NodeID: "node_a", Timestamp: baseTime.Add(1 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_a", Timestamp: baseTime.Add(2 * time.Minute)},
		{Type: EventStageStarted, NodeID: "node_b", Timestamp: baseTime.Add(3 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_b", Timestamp: baseTime.Add(4 * time.Minute)},
		{Type: EventPipelineCompleted, NodeID: "", Timestamp: baseTime.Add(5 * time.Minute)},
	}
	for _, evt := range events {
		if err := sink.Append(runID, evt); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	summary, err := sink.Summarize(runID)
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}

	if summary.TotalEvents != 6 {
		t.Errorf("TotalEvents: got %d, want 6", summary.TotalEvents)
	}
	if summary.ByType[EventPipelineStarted] != 1 {
		t.Errorf("ByType[pipeline.started]: got %d, want 1", summary.ByType[EventPipelineStarted])
	}
	if summary.ByType[EventStageStarted] != 2 {
		t.Errorf("ByType[stage.started]: got %d, want 2", summary.ByType[EventStageStarted])
	}
	if summary.ByNode["node_a"] != 2 {
		t.Errorf("ByNode[node_a]: got %d, want 2", summary.ByNode["node_a"])
	}
	if summary.FirstEvent == nil || !summary.FirstEvent.Equal(baseTime) {
		t.Errorf("FirstEvent: got %v, want %v", summary.FirstEvent, baseTime)
	}
	expectedLast := baseTime.Add(5 * time.Minute)
	if summary.LastEvent == nil || !summary.LastEvent.Equal(expectedLast) {
		t.Errorf("LastEvent: got %v, want %v", summary.LastEvent, expectedLast)
	}
}

func TestLogSinkSummarizeEmptyRun(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	runID := createTestRun(t, sink, "running", time.Now())

	summary, err := sink.Summarize(runID)
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if summary.TotalEvents != 0 {
		t.Errorf("TotalEvents: got %d, want 0", summary.TotalEvents)
	}
	if summary.FirstEvent != nil {
		t.Errorf("expected nil FirstEvent, got %v", summary.FirstEvent)
	}
	if summary.LastEvent != nil {
		t.Errorf("expected nil LastEvent, got %v", summary.LastEvent)
	}
}

func TestLogSinkSummarizeNonexistentRun(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	_, err := sink.Summarize("nonexistent-run")
	if err == nil {
		t.Fatal("expected error for summarizing nonexistent run, got nil")
	}
}

// --- Prune tests ---

func TestLogSinkPruneByAge(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	now := time.Now()
	oldTime := now.Add(-48 * time.Hour)
	recentTime := now.Add(-1 * time.Hour)

	// Create an old run and a recent run
	oldRunID := createTestRun(t, sink, "completed", oldTime)
	recentRunID := createTestRun(t, sink, "running", recentTime)

	// Append events so both runs are indexed
	if err := sink.Append(oldRunID, EngineEvent{Type: EventPipelineStarted, Timestamp: oldTime}); err != nil {
		t.Fatalf("Append to old run failed: %v", err)
	}
	if err := sink.Append(recentRunID, EngineEvent{Type: EventPipelineStarted, Timestamp: recentTime}); err != nil {
		t.Fatalf("Append to recent run failed: %v", err)
	}

	// Prune runs older than 24 hours
	pruned, err := sink.Prune(24 * time.Hour)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned run, got %d", pruned)
	}

	// Old run should be gone
	_, _, err = sink.Query(oldRunID, EventFilter{})
	if err == nil {
		t.Error("expected error querying pruned run, got nil")
	}

	// Recent run should still exist
	events, total, err := sink.Query(recentRunID, EventFilter{})
	if err != nil {
		t.Fatalf("Query recent run failed: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 event in recent run, got %d", total)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}

	// Index should not contain the old run
	index, err := sink.loadIndex()
	if err != nil {
		t.Fatalf("loadIndex failed: %v", err)
	}
	if _, ok := index.Runs[oldRunID]; ok {
		t.Error("expected old run to be removed from index")
	}
	if _, ok := index.Runs[recentRunID]; !ok {
		t.Error("expected recent run to remain in index")
	}
}

func TestLogSinkPruneNoMatchingRuns(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	recentTime := time.Now().Add(-1 * time.Hour)
	runID := createTestRun(t, sink, "running", recentTime)
	if err := sink.Append(runID, EngineEvent{Type: EventPipelineStarted, Timestamp: recentTime}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	pruned, err := sink.Prune(24 * time.Hour)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned runs, got %d", pruned)
	}
}

func TestLogSinkPruneEmptyStore(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	pruned, err := sink.Prune(24 * time.Hour)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned runs, got %d", pruned)
	}
}

// --- RetentionConfig tests ---

func TestRetentionConfigPruneLoop(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	now := time.Now()
	oldTime := now.Add(-48 * time.Hour)

	oldRunID := createTestRun(t, sink, "completed", oldTime)
	if err := sink.Append(oldRunID, EngineEvent{Type: EventPipelineStarted, Timestamp: oldTime}); err != nil {
		t.Fatalf("Append to old run failed: %v", err)
	}

	rc := RetentionConfig{
		MaxAge: 24 * time.Hour,
	}

	// Use a short-lived context to run exactly one prune cycle
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// The PruneLoop should run at least once with a short interval
	rc.PruneLoop(ctx, sink, 50*time.Millisecond)

	// After PruneLoop completes, the old run should be pruned
	_, _, err := sink.Query(oldRunID, EventFilter{})
	if err == nil {
		t.Error("expected error querying pruned run after PruneLoop, got nil")
	}
}

func TestRetentionConfigMaxRuns(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	now := time.Now()
	// Create 5 runs with staggered start times
	runIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		startTime := now.Add(-time.Duration(5-i) * time.Hour)
		runIDs[i] = createTestRun(t, sink, "completed", startTime)
		if err := sink.Append(runIDs[i], EngineEvent{Type: EventPipelineStarted, Timestamp: startTime}); err != nil {
			t.Fatalf("Append failed for run %d: %v", i, err)
		}
	}

	rc := RetentionConfig{
		MaxRuns: 3,
	}

	pruned, err := rc.PruneByMaxRuns(sink)
	if err != nil {
		t.Fatalf("PruneByMaxRuns failed: %v", err)
	}
	if pruned != 2 {
		t.Errorf("expected 2 pruned runs, got %d", pruned)
	}

	// The 2 oldest runs should be gone
	for _, id := range runIDs[:2] {
		_, _, err := sink.Query(id, EventFilter{})
		if err == nil {
			t.Errorf("expected error querying pruned run %q, got nil", id)
		}
	}

	// The 3 most recent runs should remain
	for _, id := range runIDs[2:] {
		_, _, err := sink.Query(id, EventFilter{})
		if err != nil {
			t.Errorf("expected recent run %q to still exist, got error: %v", id, err)
		}
	}
}

// --- Index tests ---

func TestLogSinkIndexCreatedOnInit(t *testing.T) {
	dir := t.TempDir()
	sink, err := NewFSLogSink(dir)
	if err != nil {
		t.Fatalf("NewFSLogSink failed: %v", err)
	}
	defer sink.Close()

	indexPath := filepath.Join(dir, "index.json")
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("index.json not created on init: %v", err)
	}

	// Verify it's valid JSON
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("failed to read index.json: %v", err)
	}
	var index RunIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatalf("index.json is not valid JSON: %v", err)
	}
}

func TestLogSinkIndexConsistencyAfterMultipleAppends(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	startTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	runID := createTestRun(t, sink, "running", startTime)

	// Append multiple events
	for i := 0; i < 10; i++ {
		evt := EngineEvent{
			Type:      EventStageStarted,
			NodeID:    "node",
			Timestamp: startTime.Add(time.Duration(i) * time.Minute),
		}
		if err := sink.Append(runID, evt); err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
	}

	index, err := sink.loadIndex()
	if err != nil {
		t.Fatalf("loadIndex failed: %v", err)
	}

	entry, ok := index.Runs[runID]
	if !ok {
		t.Fatalf("run %q not in index", runID)
	}
	if entry.EventCount != 10 {
		t.Errorf("EventCount: got %d, want 10", entry.EventCount)
	}
}

func TestLogSinkIndexRemovedAfterPrune(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	now := time.Now()
	oldTime := now.Add(-48 * time.Hour)

	oldRunID := createTestRun(t, sink, "completed", oldTime)
	if err := sink.Append(oldRunID, EngineEvent{Type: EventPipelineStarted, Timestamp: oldTime}); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Verify run is in the index
	index, err := sink.loadIndex()
	if err != nil {
		t.Fatalf("loadIndex failed: %v", err)
	}
	if _, ok := index.Runs[oldRunID]; !ok {
		t.Fatal("expected old run to be in index before prune")
	}

	// Prune
	_, err = sink.Prune(24 * time.Hour)
	if err != nil {
		t.Fatalf("Prune failed: %v", err)
	}

	// Verify run is removed from the index
	index, err = sink.loadIndex()
	if err != nil {
		t.Fatalf("loadIndex after prune failed: %v", err)
	}
	if _, ok := index.Runs[oldRunID]; ok {
		t.Error("expected old run to be removed from index after prune")
	}
}

func TestLogSinkIndexListRuns(t *testing.T) {
	sink := newTestLogSink(t)
	defer sink.Close()

	now := time.Now()
	runIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		startTime := now.Add(-time.Duration(3-i) * time.Hour)
		runIDs[i] = createTestRun(t, sink, "completed", startTime)
		if err := sink.Append(runIDs[i], EngineEvent{Type: EventPipelineStarted, Timestamp: startTime}); err != nil {
			t.Fatalf("Append failed for run %d: %v", i, err)
		}
	}

	entries, err := sink.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 runs, got %d", len(entries))
	}

	// Verify all runs are present
	found := make(map[string]bool)
	for _, entry := range entries {
		found[entry.ID] = true
	}
	for _, id := range runIDs {
		if !found[id] {
			t.Errorf("run %q not found in ListRuns results", id)
		}
	}
}

// --- Close tests ---

func TestLogSinkClose(t *testing.T) {
	sink := newTestLogSink(t)

	err := sink.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Calling Close again should be safe
	err = sink.Close()
	if err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

// --- helper ---

func timePtr(t time.Time) *time.Time {
	return &t
}
