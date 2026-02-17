// ABOUTME: Tests for the NDJSON progress logger that records engine events for observability.
// ABOUTME: Validates event appending, live state tracking, atomic file writes, and concurrent safety.
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

func TestNewProgressLogger(t *testing.T) {
	dir := t.TempDir()

	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	// Verify progress.ndjson file was created
	ndjsonPath := filepath.Join(dir, "progress.ndjson")
	if _, err := os.Stat(ndjsonPath); os.IsNotExist(err) {
		t.Errorf("expected progress.ndjson to exist at %s", ndjsonPath)
	}

	// Verify live.json file was created with initial state
	liveJSONPath := filepath.Join(dir, "live.json")
	if _, err := os.Stat(liveJSONPath); os.IsNotExist(err) {
		t.Errorf("expected live.json to exist at %s", liveJSONPath)
	}

	data, err := os.ReadFile(liveJSONPath)
	if err != nil {
		t.Fatalf("failed to read live.json: %v", err)
	}
	var state LiveState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse live.json: %v", err)
	}
	if state.Status != "pending" {
		t.Errorf("initial status = %q, want %q", state.Status, "pending")
	}
	if state.EventCount != 0 {
		t.Errorf("initial event count = %d, want 0", state.EventCount)
	}
}

func TestProgressLoggerHandleEventAppends(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	// Send three events
	events := []EngineEvent{
		{Type: EventPipelineStarted, Timestamp: time.Now()},
		{Type: EventStageStarted, NodeID: "node_a", Timestamp: time.Now()},
		{Type: EventStageCompleted, NodeID: "node_a", Timestamp: time.Now()},
	}
	for _, evt := range events {
		pl.HandleEvent(evt)
	}

	// Read the ndjson file and verify we have 3 lines
	data, err := os.ReadFile(filepath.Join(dir, "progress.ndjson"))
	if err != nil {
		t.Fatalf("failed to read progress.ndjson: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines in ndjson, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var entry ProgressEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}
}

func TestProgressEntryJSON(t *testing.T) {
	entry := ProgressEntry{
		Timestamp: "2025-01-15T10:30:00Z",
		Type:      "stage.started",
		NodeID:    "build_step",
		Data:      map[string]any{"attempt": float64(1)},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	// Unmarshal back and verify
	var decoded ProgressEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	if decoded.Timestamp != entry.Timestamp {
		t.Errorf("Timestamp = %q, want %q", decoded.Timestamp, entry.Timestamp)
	}
	if decoded.Type != entry.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, entry.Type)
	}
	if decoded.NodeID != entry.NodeID {
		t.Errorf("NodeID = %q, want %q", decoded.NodeID, entry.NodeID)
	}

	// Verify omitempty for NodeID and Data
	empty := ProgressEntry{Timestamp: "2025-01-15T10:30:00Z", Type: "pipeline.started"}
	data, err = json.Marshal(empty)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}
	s := string(data)
	if strings.Contains(s, "node_id") {
		t.Errorf("expected node_id to be omitted when empty, got: %s", s)
	}
	if strings.Contains(s, "data") {
		t.Errorf("expected data to be omitted when nil, got: %s", s)
	}
}

func TestProgressLoggerPipelineStarted(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	now := time.Now().UTC()
	pl.HandleEvent(EngineEvent{Type: EventPipelineStarted, Timestamp: now})

	state := pl.State()
	if state.Status != "running" {
		t.Errorf("Status = %q, want %q", state.Status, "running")
	}
	if state.StartedAt == "" {
		t.Error("StartedAt should be set after pipeline.started event")
	}
	if state.EventCount != 1 {
		t.Errorf("EventCount = %d, want 1", state.EventCount)
	}
}

func TestProgressLoggerStageStarted(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	pl.HandleEvent(EngineEvent{Type: EventPipelineStarted, Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "compile", Timestamp: time.Now()})

	state := pl.State()
	if state.ActiveNode != "compile" {
		t.Errorf("ActiveNode = %q, want %q", state.ActiveNode, "compile")
	}
}

func TestProgressLoggerStageCompleted(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	pl.HandleEvent(EngineEvent{Type: EventPipelineStarted, Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "compile", Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageCompleted, NodeID: "compile", Timestamp: time.Now()})

	state := pl.State()
	if state.ActiveNode != "" {
		t.Errorf("ActiveNode = %q, want empty after completion", state.ActiveNode)
	}
	if len(state.Completed) != 1 || state.Completed[0] != "compile" {
		t.Errorf("Completed = %v, want [compile]", state.Completed)
	}
}

func TestProgressLoggerStageFailed(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	pl.HandleEvent(EngineEvent{Type: EventPipelineStarted, Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "deploy", Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageFailed, NodeID: "deploy", Timestamp: time.Now()})

	state := pl.State()
	if state.ActiveNode != "" {
		t.Errorf("ActiveNode = %q, want empty after failure", state.ActiveNode)
	}
	if len(state.Failed) != 1 || state.Failed[0] != "deploy" {
		t.Errorf("Failed = %v, want [deploy]", state.Failed)
	}
}

func TestProgressLoggerPipelineCompleted(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	pl.HandleEvent(EngineEvent{Type: EventPipelineStarted, Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "build", Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageCompleted, NodeID: "build", Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventPipelineCompleted, Timestamp: time.Now()})

	state := pl.State()
	if state.Status != "completed" {
		t.Errorf("Status = %q, want %q", state.Status, "completed")
	}
}

func TestProgressLoggerPipelineFailed(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	pl.HandleEvent(EngineEvent{Type: EventPipelineStarted, Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageFailed, NodeID: "test", Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventPipelineFailed, Timestamp: time.Now(), Data: map[string]any{"error": "tests failed"}})

	state := pl.State()
	if state.Status != "failed" {
		t.Errorf("Status = %q, want %q", state.Status, "failed")
	}
}

func TestProgressLoggerLiveJSON(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	// Send a sequence of events
	pl.HandleEvent(EngineEvent{Type: EventPipelineStarted, Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "step1", Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageCompleted, NodeID: "step1", Timestamp: time.Now()})
	pl.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "step2", Timestamp: time.Now()})

	// Read live.json directly from disk
	data, err := os.ReadFile(filepath.Join(dir, "live.json"))
	if err != nil {
		t.Fatalf("failed to read live.json: %v", err)
	}

	var state LiveState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse live.json: %v", err)
	}

	if state.Status != "running" {
		t.Errorf("Status = %q, want %q", state.Status, "running")
	}
	if state.ActiveNode != "step2" {
		t.Errorf("ActiveNode = %q, want %q", state.ActiveNode, "step2")
	}
	if len(state.Completed) != 1 || state.Completed[0] != "step1" {
		t.Errorf("Completed = %v, want [step1]", state.Completed)
	}
	if state.EventCount != 4 {
		t.Errorf("EventCount = %d, want 4", state.EventCount)
	}
	if state.StartedAt == "" {
		t.Error("StartedAt should be set")
	}
	if state.UpdatedAt == "" {
		t.Error("UpdatedAt should be set")
	}
}

func TestProgressLoggerClose(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}

	// Write one event before close
	pl.HandleEvent(EngineEvent{Type: EventPipelineStarted, Timestamp: time.Now()})

	if err := pl.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// After close, HandleEvent should not panic (graceful no-op)
	pl.HandleEvent(EngineEvent{Type: EventStageStarted, NodeID: "x", Timestamp: time.Now()})

	// Verify the ndjson file still has exactly 1 line (no writes after close)
	data, err := os.ReadFile(filepath.Join(dir, "progress.ndjson"))
	if err != nil {
		t.Fatalf("failed to read progress.ndjson: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line after close, got %d", len(lines))
	}
}

func TestProgressLoggerConcurrent(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	const goroutines = 20
	const eventsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				pl.HandleEvent(EngineEvent{
					Type:      EventStageStarted,
					NodeID:    "node",
					Timestamp: time.Now(),
				})
			}
		}(g)
	}

	wg.Wait()

	// Verify the ndjson file has exactly goroutines * eventsPerGoroutine lines
	data, err := os.ReadFile(filepath.Join(dir, "progress.ndjson"))
	if err != nil {
		t.Fatalf("failed to read progress.ndjson: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	expected := goroutines * eventsPerGoroutine
	if len(lines) != expected {
		t.Errorf("expected %d lines, got %d", expected, len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var entry ProgressEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Errorf("line %d is not valid JSON: %v (content: %q)", i, err, line)
		}
	}

	// Event count should match
	state := pl.State()
	if state.EventCount != expected {
		t.Errorf("EventCount = %d, want %d", state.EventCount, expected)
	}
}

func TestProgressLoggerSkipsTextDeltaEvents(t *testing.T) {
	dir := t.TempDir()
	pl, err := NewProgressLogger(dir)
	if err != nil {
		t.Fatalf("NewProgressLogger() error = %v", err)
	}
	defer pl.Close()

	// Send a normal event first so we know the logger is working
	pl.HandleEvent(EngineEvent{
		Type:      EventStageStarted,
		NodeID:    "build",
		Timestamp: time.Now(),
	})

	// Send several agent.text.delta events (these are high-frequency ephemeral events)
	for i := 0; i < 5; i++ {
		pl.HandleEvent(EngineEvent{
			Type:      EventAgentTextDelta,
			NodeID:    "build",
			Timestamp: time.Now(),
			Data:      map[string]any{"text": "hello "},
		})
	}

	// Send another normal event after the deltas
	pl.HandleEvent(EngineEvent{
		Type:      EventStageCompleted,
		NodeID:    "build",
		Timestamp: time.Now(),
	})

	// Read the NDJSON file - should only contain the 2 normal events, not the 5 deltas
	data, err := os.ReadFile(filepath.Join(dir, "progress.ndjson"))
	if err != nil {
		t.Fatalf("failed to read progress.ndjson: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines in ndjson (delta events should be skipped), got %d", len(lines))
	}

	// Verify the persisted events are the stage.started and stage.completed, not deltas
	for _, line := range lines {
		var entry ProgressEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid JSON line: %v", err)
		}
		if entry.Type == string(EventAgentTextDelta) {
			t.Errorf("EventAgentTextDelta event should not be persisted, but found one in ndjson")
		}
	}

	// Verify live state still tracks event count for all events including deltas
	state := pl.State()
	if state.EventCount != 2 {
		t.Errorf("EventCount = %d, want 2 (deltas should not increment count)", state.EventCount)
	}
}
