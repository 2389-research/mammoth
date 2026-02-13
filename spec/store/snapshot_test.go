// ABOUTME: Tests for atomic snapshot save and load operations.
// ABOUTME: Covers round-trip, highest-event-ID selection, empty dirs, and nested directory creation.
package store_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/store"
)

func makeSnapshot(eventID uint64) *store.SnapshotData {
	state := core.NewSpecState()
	state.LastEventID = eventID

	agentContexts := map[string]json.RawMessage{
		"explorer": json.RawMessage(`{"step":3,"notes":"found patterns"}`),
	}

	return &store.SnapshotData{
		State:         state,
		LastEventID:   eventID,
		AgentContexts: agentContexts,
		SavedAt:       time.Now().UTC(),
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	snap := makeSnapshot(42)

	if err := store.SaveSnapshot(dir, snap); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	loaded, err := store.LoadLatestSnapshot(dir)
	if err != nil {
		t.Fatalf("LoadLatestSnapshot: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil snapshot")
	}

	if loaded.LastEventID != 42 {
		t.Errorf("LastEventID = %d, want 42", loaded.LastEventID)
	}
	if loaded.State.LastEventID != 42 {
		t.Errorf("State.LastEventID = %d, want 42", loaded.State.LastEventID)
	}
	if _, ok := loaded.AgentContexts["explorer"]; !ok {
		t.Error("expected agent_contexts to contain 'explorer'")
	}

	// Verify the explorer context content
	var ctx map[string]interface{}
	if err := json.Unmarshal(loaded.AgentContexts["explorer"], &ctx); err != nil {
		t.Fatalf("unmarshal explorer context: %v", err)
	}
	if ctx["step"] != float64(3) {
		t.Errorf("explorer step = %v, want 3", ctx["step"])
	}
}

func TestLoadLatestPicksHighest(t *testing.T) {
	dir := t.TempDir()

	if err := store.SaveSnapshot(dir, makeSnapshot(10)); err != nil {
		t.Fatalf("SaveSnapshot(10): %v", err)
	}
	if err := store.SaveSnapshot(dir, makeSnapshot(20)); err != nil {
		t.Fatalf("SaveSnapshot(20): %v", err)
	}

	loaded, err := store.LoadLatestSnapshot(dir)
	if err != nil {
		t.Fatalf("LoadLatestSnapshot: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if loaded.LastEventID != 20 {
		t.Errorf("LastEventID = %d, want 20", loaded.LastEventID)
	}
}

func TestLoadReturnsNilForEmptyDir(t *testing.T) {
	dir := t.TempDir()

	loaded, err := store.LoadLatestSnapshot(dir)
	if err != nil {
		t.Fatalf("LoadLatestSnapshot: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil, got snapshot with LastEventID=%d", loaded.LastEventID)
	}
}

func TestLoadReturnsNilForNonExistentDir(t *testing.T) {
	loaded, err := store.LoadLatestSnapshot("/nonexistent/path/snapshots")
	if err != nil {
		t.Fatalf("LoadLatestSnapshot: %v", err)
	}
	if loaded != nil {
		t.Error("expected nil for non-existent directory")
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "deep", "nested", "snapshots")

	if err := store.SaveSnapshot(nested, makeSnapshot(5)); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	loaded, err := store.LoadLatestSnapshot(nested)
	if err != nil {
		t.Fatalf("LoadLatestSnapshot: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if loaded.LastEventID != 5 {
		t.Errorf("LastEventID = %d, want 5", loaded.LastEventID)
	}
}

func TestSnapshotWithCards(t *testing.T) {
	state := core.NewSpecState()
	state.Core = &core.SpecCore{
		SpecID:    core.NewULID(),
		Title:     "Snapshot With Cards",
		OneLiner:  "Test",
		Goal:      "Verify card round-trip",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	state.LastEventID = 10

	card1 := core.NewCard("idea", "Card One", "human")
	card2 := core.NewCard("task", "Card Two", "agent")
	card2.Lane = "Plan"
	card2.Order = 2.5
	state.Cards.Set(card1.CardID, card1)
	state.Cards.Set(card2.CardID, card2)

	dir := t.TempDir()
	snap := &store.SnapshotData{
		State:         state,
		LastEventID:   10,
		AgentContexts: map[string]json.RawMessage{},
		SavedAt:       time.Now().UTC(),
	}

	if err := store.SaveSnapshot(dir, snap); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	loaded, err := store.LoadLatestSnapshot(dir)
	if err != nil {
		t.Fatalf("LoadLatestSnapshot: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil snapshot")
	}

	if loaded.State.Cards.Len() != 2 {
		t.Fatalf("expected 2 cards, got %d", loaded.State.Cards.Len())
	}
	if loaded.State.Core == nil {
		t.Fatal("expected non-nil core")
	}
	if loaded.State.Core.Title != "Snapshot With Cards" {
		t.Errorf("core title = %q, want %q", loaded.State.Core.Title, "Snapshot With Cards")
	}

	// Verify card2 properties survived
	c2, ok := loaded.State.Cards.Get(card2.CardID)
	if !ok {
		t.Fatal("expected to find card2")
	}
	if c2.Lane != "Plan" {
		t.Errorf("card2 lane = %q, want %q", c2.Lane, "Plan")
	}
	if c2.Order != 2.5 {
		t.Errorf("card2 order = %f, want 2.5", c2.Order)
	}
}

func TestSnapshotWithDefaultLanes(t *testing.T) {
	state := core.NewSpecState()
	state.LastEventID = 1

	dir := t.TempDir()
	snap := &store.SnapshotData{
		State:         state,
		LastEventID:   1,
		AgentContexts: map[string]json.RawMessage{},
		SavedAt:       time.Now().UTC(),
	}

	if err := store.SaveSnapshot(dir, snap); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	loaded, err := store.LoadLatestSnapshot(dir)
	if err != nil {
		t.Fatalf("LoadLatestSnapshot: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil snapshot")
	}

	expectedLanes := []string{"Ideas", "Plan", "Spec"}
	if len(loaded.State.Lanes) != len(expectedLanes) {
		t.Fatalf("lanes length = %d, want %d", len(loaded.State.Lanes), len(expectedLanes))
	}
	for i, lane := range expectedLanes {
		if loaded.State.Lanes[i] != lane {
			t.Errorf("lanes[%d] = %q, want %q", i, loaded.State.Lanes[i], lane)
		}
	}
}
