// ABOUTME: Tests for RunState types, RunStateStore interface, and the filesystem-backed implementation.
// ABOUTME: Covers CRUD operations, event appending, concurrent access safety, and corrupt/missing file handling.
package attractor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- GenerateRunID tests ---

func TestRunStateGenerateRunID(t *testing.T) {
	id, err := GenerateRunID()
	if err != nil {
		t.Fatalf("GenerateRunID failed: %v", err)
	}
	if len(id) != 16 {
		t.Errorf("expected 16-char hex string, got %d chars: %q", len(id), id)
	}
}

func TestRunStateGenerateRunIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := GenerateRunID()
		if err != nil {
			t.Fatalf("GenerateRunID failed on iteration %d: %v", i, err)
		}
		if seen[id] {
			t.Fatalf("duplicate ID generated: %q", id)
		}
		seen[id] = true
	}
}

// --- FSRunStateStore tests ---

func newTestStore(t *testing.T) *FSRunStateStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewFSRunStateStore(dir)
	if err != nil {
		t.Fatalf("NewFSRunStateStore failed: %v", err)
	}
	return store
}

func newTestRunState(t *testing.T) *RunState {
	t.Helper()
	id, err := GenerateRunID()
	if err != nil {
		t.Fatalf("GenerateRunID failed: %v", err)
	}
	return &RunState{
		ID:             id,
		PipelineFile:   "test-pipeline.dot",
		Status:         "running",
		StartedAt:      time.Now().Truncate(time.Millisecond),
		CurrentNode:    "start",
		CompletedNodes: []string{},
		Context:        map[string]any{"model": "gpt-4"},
		Events:         []EngineEvent{},
	}
}

func TestRunStateCreate(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	err := store.Create(state)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify directory was created
	runDir := filepath.Join(store.baseDir, state.ID)
	info, err := os.Stat(runDir)
	if err != nil {
		t.Fatalf("run directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected run path to be a directory")
	}

	// Verify manifest.json exists
	manifestPath := filepath.Join(runDir, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest.json not created: %v", err)
	}

	// Verify context.json exists
	contextPath := filepath.Join(runDir, "context.json")
	if _, err := os.Stat(contextPath); err != nil {
		t.Fatalf("context.json not created: %v", err)
	}

	// Verify events.jsonl exists
	eventsPath := filepath.Join(runDir, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		t.Fatalf("events.jsonl not created: %v", err)
	}

	// Verify nodes directory exists
	nodesDir := filepath.Join(runDir, "nodes")
	if _, err := os.Stat(nodesDir); err != nil {
		t.Fatalf("nodes directory not created: %v", err)
	}
}

func TestRunStateCreateDuplicate(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("first Create failed: %v", err)
	}

	err := store.Create(state)
	if err == nil {
		t.Fatal("expected error for duplicate Create, got nil")
	}
}

func TestRunStateGet(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)
	state.Context = map[string]any{"model": "gpt-4", "temperature": 0.7}

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.Get(state.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.ID != state.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, state.ID)
	}
	if got.PipelineFile != state.PipelineFile {
		t.Errorf("PipelineFile mismatch: got %q, want %q", got.PipelineFile, state.PipelineFile)
	}
	if got.Status != state.Status {
		t.Errorf("Status mismatch: got %q, want %q", got.Status, state.Status)
	}
	if got.CurrentNode != state.CurrentNode {
		t.Errorf("CurrentNode mismatch: got %q, want %q", got.CurrentNode, state.CurrentNode)
	}
	if got.Context["model"] != "gpt-4" {
		t.Errorf("Context[model] mismatch: got %v, want 'gpt-4'", got.Context["model"])
	}
	if got.StartedAt.UnixMilli() != state.StartedAt.UnixMilli() {
		t.Errorf("StartedAt mismatch: got %v, want %v", got.StartedAt, state.StartedAt)
	}
}

func TestRunStateGetNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.Get("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent ID, got nil")
	}
}

func TestRunStateUpdate(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update the state
	now := time.Now().Truncate(time.Millisecond)
	state.Status = "completed"
	state.CompletedAt = &now
	state.CurrentNode = "end"
	state.CompletedNodes = []string{"start", "process", "end"}
	state.Context["result"] = "success"

	if err := store.Update(state); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := store.Get(state.ID)
	if err != nil {
		t.Fatalf("Get after Update failed: %v", err)
	}

	if got.Status != "completed" {
		t.Errorf("Status mismatch: got %q, want 'completed'", got.Status)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected non-nil CompletedAt")
	}
	if got.CompletedAt.UnixMilli() != now.UnixMilli() {
		t.Errorf("CompletedAt mismatch: got %v, want %v", got.CompletedAt, now)
	}
	if len(got.CompletedNodes) != 3 {
		t.Errorf("expected 3 completed nodes, got %d", len(got.CompletedNodes))
	}
	if got.Context["result"] != "success" {
		t.Errorf("Context[result] mismatch: got %v, want 'success'", got.Context["result"])
	}
}

func TestRunStateUpdateNonexistent(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	err := store.Update(state)
	if err == nil {
		t.Fatal("expected error for updating nonexistent run, got nil")
	}
}

func TestRunStateList(t *testing.T) {
	store := newTestStore(t)

	// Empty list
	list, err := store.List()
	if err != nil {
		t.Fatalf("List on empty store failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d items", len(list))
	}

	// Create multiple runs
	state1 := newTestRunState(t)
	state2 := newTestRunState(t)
	state3 := newTestRunState(t)

	for _, s := range []*RunState{state1, state2, state3} {
		if err := store.Create(s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	list, err = store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 items, got %d", len(list))
	}

	// Verify all IDs are present
	ids := make(map[string]bool)
	for _, s := range list {
		ids[s.ID] = true
	}
	for _, s := range []*RunState{state1, state2, state3} {
		if !ids[s.ID] {
			t.Errorf("missing run ID %q from list", s.ID)
		}
	}
}

func TestRunStateAddEvent(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	event1 := EngineEvent{
		Type:   EventPipelineStarted,
		NodeID: "",
		Data:   map[string]any{"pipeline": "test"},
	}
	event2 := EngineEvent{
		Type:   EventStageStarted,
		NodeID: "start",
		Data:   map[string]any{"attempt": 1},
	}

	if err := store.AddEvent(state.ID, event1); err != nil {
		t.Fatalf("AddEvent 1 failed: %v", err)
	}
	if err := store.AddEvent(state.ID, event2); err != nil {
		t.Fatalf("AddEvent 2 failed: %v", err)
	}

	// Verify events are persisted
	got, err := store.Get(state.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(got.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got.Events))
	}
	if got.Events[0].Type != EventPipelineStarted {
		t.Errorf("event 0 type mismatch: got %q, want %q", got.Events[0].Type, EventPipelineStarted)
	}
	if got.Events[1].Type != EventStageStarted {
		t.Errorf("event 1 type mismatch: got %q, want %q", got.Events[1].Type, EventStageStarted)
	}
	if got.Events[1].NodeID != "start" {
		t.Errorf("event 1 NodeID mismatch: got %q, want 'start'", got.Events[1].NodeID)
	}
}

func TestRunStateAddEventNonexistent(t *testing.T) {
	store := newTestStore(t)

	err := store.AddEvent("nonexistent-id", EngineEvent{Type: EventPipelineStarted})
	if err == nil {
		t.Fatal("expected error for adding event to nonexistent run, got nil")
	}
}

func TestRunStateAddEventAppendOnly(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Add events one at a time and verify the file grows
	for i := 0; i < 5; i++ {
		evt := EngineEvent{
			Type:   EventStageStarted,
			NodeID: "node",
			Data:   map[string]any{"index": i},
		}
		if err := store.AddEvent(state.ID, evt); err != nil {
			t.Fatalf("AddEvent %d failed: %v", i, err)
		}
	}

	// Read the raw events.jsonl and verify line count
	eventsPath := filepath.Join(store.baseDir, state.ID, "events.jsonl")
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("failed to read events.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines in events.jsonl, got %d", len(lines))
	}

	// Verify each line is valid JSON
	for i, line := range lines {
		var evt EngineEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestRunStateConcurrentAccess(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Concurrent AddEvent calls
	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			evt := EngineEvent{
				Type:   EventStageStarted,
				NodeID: "concurrent_node",
				Data:   map[string]any{"goroutine": idx},
			}
			if err := store.AddEvent(state.ID, evt); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent AddEvent error: %v", err)
	}

	// Verify all events were recorded
	got, err := store.Get(state.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(got.Events) != 20 {
		t.Errorf("expected 20 events from concurrent writes, got %d", len(got.Events))
	}
}

func TestRunStateConcurrentCreateAndList(t *testing.T) {
	store := newTestStore(t)

	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	// Create 10 runs concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := newTestRunState(t)
			if err := store.Create(s); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent Create error: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 10 {
		t.Errorf("expected 10 runs, got %d", len(list))
	}
}

func TestRunStateCorruptManifest(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Corrupt the manifest file
	manifestPath := filepath.Join(store.baseDir, state.ID, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to corrupt manifest: %v", err)
	}

	_, err := store.Get(state.ID)
	if err == nil {
		t.Fatal("expected error for corrupt manifest, got nil")
	}
}

func TestRunStateCorruptContext(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Corrupt the context file
	contextPath := filepath.Join(store.baseDir, state.ID, "context.json")
	if err := os.WriteFile(contextPath, []byte("{broken"), 0644); err != nil {
		t.Fatalf("failed to corrupt context: %v", err)
	}

	_, err := store.Get(state.ID)
	if err == nil {
		t.Fatal("expected error for corrupt context, got nil")
	}
}

func TestRunStateCorruptEventsJSONL(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Add a good event first
	if err := store.AddEvent(state.ID, EngineEvent{Type: EventPipelineStarted}); err != nil {
		t.Fatalf("AddEvent failed: %v", err)
	}

	// Append a corrupt line
	eventsPath := filepath.Join(store.baseDir, state.ID, "events.jsonl")
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open events file: %v", err)
	}
	_, _ = f.WriteString("{not valid json\n")
	f.Close()

	_, err = store.Get(state.ID)
	if err == nil {
		t.Fatal("expected error for corrupt events line, got nil")
	}
}

func TestRunStateListIgnoresNonDirectoryEntries(t *testing.T) {
	store := newTestStore(t)

	// Create a valid run
	state := newTestRunState(t)
	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Create a stray file in the base directory
	strayFile := filepath.Join(store.baseDir, "stray.txt")
	if err := os.WriteFile(strayFile, []byte("stray"), 0644); err != nil {
		t.Fatalf("failed to create stray file: %v", err)
	}

	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 run (ignoring stray file), got %d", len(list))
	}
}

func TestRunStateUpdateWithError(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	state.Status = "failed"
	state.Error = "handler panicked: nil pointer dereference"

	if err := store.Update(state); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := store.Get(state.ID)
	if err != nil {
		t.Fatalf("Get after Update failed: %v", err)
	}

	if got.Error != "handler panicked: nil pointer dereference" {
		t.Errorf("Error mismatch: got %q", got.Error)
	}
	if got.Status != "failed" {
		t.Errorf("Status mismatch: got %q, want 'failed'", got.Status)
	}
}

func TestRunStateEmptyEventsFile(t *testing.T) {
	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get immediately after create (no events added)
	got, err := store.Get(state.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(got.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(got.Events))
	}
}
