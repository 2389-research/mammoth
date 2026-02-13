// ABOUTME: Tests for crash recovery and self-healing of spec state.
// ABOUTME: Covers clean recovery, snapshot+tail, JSONL repair, and stale SQLite rebuild.
package store_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/store"
	"github.com/oklog/ulid/v2"
)

func makeRecoveryEvent(eventID uint64, specID ulid.ULID, payload core.EventPayload) core.Event {
	return core.Event{
		EventID:   eventID,
		SpecID:    specID,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

func makeRecoverySpecDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	specDir := filepath.Join(dir, "test_spec")
	_ = os.MkdirAll(filepath.Join(specDir, "snapshots"), 0o755)
	_ = os.MkdirAll(filepath.Join(specDir, "exports"), 0o755)
	return specDir
}

func writeEvents(t *testing.T, specDir string, events []core.Event) {
	t.Helper()
	eventsPath := filepath.Join(specDir, "events.jsonl")
	log, err := store.OpenJsonl(eventsPath)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}
	defer func() { _ = log.Close() }()

	for i := range events {
		if err := log.Append(&events[i]); err != nil {
			t.Fatalf("Append event %d: %v", events[i].EventID, err)
		}
	}
}

func TestRecoverFromCleanState(t *testing.T) {
	specDir := makeRecoverySpecDir(t)
	specID := core.NewULID()

	events := []core.Event{
		makeRecoveryEvent(1, specID, core.SpecCreatedPayload{
			Title:    "Recovery Test",
			OneLiner: "Test",
			Goal:     "Verify recovery",
		}),
		makeRecoveryEvent(2, specID, core.CardCreatedPayload{
			Card: core.NewCard("idea", "Test Card", "human"),
		}),
	}

	writeEvents(t, specDir, events)

	state, lastID, err := store.RecoverSpec(specDir)
	if err != nil {
		t.Fatalf("RecoverSpec: %v", err)
	}

	if lastID != 2 {
		t.Errorf("lastID = %d, want 2", lastID)
	}
	if state.Core == nil {
		t.Fatal("expected non-nil core")
	}
	if state.Core.Title != "Recovery Test" {
		t.Errorf("title = %q, want %q", state.Core.Title, "Recovery Test")
	}
	if state.Cards.Len() != 1 {
		t.Errorf("cards count = %d, want 1", state.Cards.Len())
	}
}

func TestRecoverFromSnapshotPlusTail(t *testing.T) {
	specDir := makeRecoverySpecDir(t)
	specID := core.NewULID()

	// Create 20 events
	var allEvents []core.Event
	allEvents = append(allEvents, makeRecoveryEvent(1, specID, core.SpecCreatedPayload{
		Title:    "Snapshot Test",
		OneLiner: "Test",
		Goal:     "Verify snapshot + tail",
	}))
	for i := uint64(2); i <= 20; i++ {
		allEvents = append(allEvents, makeRecoveryEvent(i, specID, core.CardCreatedPayload{
			Card: core.NewCard("idea", fmt.Sprintf("Card %d", i), "human"),
		}))
	}

	// Write all events to JSONL
	writeEvents(t, specDir, allEvents)

	// Create snapshot at event 10 by replaying first 10 events
	snapState := core.NewSpecState()
	for i := 0; i < 10; i++ {
		snapState.Apply(&allEvents[i])
	}

	snapData := &store.SnapshotData{
		State:         snapState,
		LastEventID:   10,
		AgentContexts: map[string]json.RawMessage{},
		SavedAt:       time.Now().UTC(),
	}
	if err := store.SaveSnapshot(filepath.Join(specDir, "snapshots"), snapData); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	// Recover: should load snapshot at 10, replay events 11-20
	state, lastID, err := store.RecoverSpec(specDir)
	if err != nil {
		t.Fatalf("RecoverSpec: %v", err)
	}

	if lastID != 20 {
		t.Errorf("lastID = %d, want 20", lastID)
	}
	if state.Core == nil {
		t.Fatal("expected non-nil core")
	}
	if state.Core.Title != "Snapshot Test" {
		t.Errorf("title = %q, want %q", state.Core.Title, "Snapshot Test")
	}
	// 19 cards (events 2-20)
	if state.Cards.Len() != 19 {
		t.Errorf("cards count = %d, want 19", state.Cards.Len())
	}
}

func TestRecoverRepairsPartialJsonl(t *testing.T) {
	specDir := makeRecoverySpecDir(t)
	specID := core.NewULID()

	events := []core.Event{
		makeRecoveryEvent(1, specID, core.SpecCreatedPayload{
			Title:    "Repair Test",
			OneLiner: "Test",
			Goal:     "Verify repair",
		}),
		makeRecoveryEvent(2, specID, core.CardCreatedPayload{
			Card: core.NewCard("idea", "Good Card", "human"),
		}),
	}

	writeEvents(t, specDir, events)

	// Append garbage to simulate a partial write / crash
	eventsPath := filepath.Join(specDir, "events.jsonl")
	f, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	_, _ = f.WriteString(`{"event_id":3,"corrupt_data`)
	_ = f.Close()

	// Recovery should repair and still get 2 valid events
	state, lastID, err := store.RecoverSpec(specDir)
	if err != nil {
		t.Fatalf("RecoverSpec: %v", err)
	}

	if lastID != 2 {
		t.Errorf("lastID = %d, want 2", lastID)
	}
	if state.Core == nil {
		t.Fatal("expected non-nil core")
	}
	if state.Core.Title != "Repair Test" {
		t.Errorf("title = %q, want %q", state.Core.Title, "Repair Test")
	}
	if state.Cards.Len() != 1 {
		t.Errorf("cards count = %d, want 1", state.Cards.Len())
	}
}

func TestRecoverRebuildsStaleSQL(t *testing.T) {
	specDir := makeRecoverySpecDir(t)
	specID := core.NewULID()

	card := core.NewCard("idea", "Stale Card", "human")

	events := []core.Event{
		makeRecoveryEvent(1, specID, core.SpecCreatedPayload{
			Title:    "Stale Test",
			OneLiner: "Test",
			Goal:     "Verify rebuild",
		}),
		makeRecoveryEvent(2, specID, core.CardCreatedPayload{Card: card}),
	}

	// Write only event 1 first
	writeEvents(t, specDir, events[:1])

	// Create SQLite index with only event 1 (simulate it being behind)
	indexPath := filepath.Join(specDir, "index.db")
	idx, err := store.OpenSqlite(indexPath)
	if err != nil {
		t.Fatalf("OpenSqlite: %v", err)
	}
	if err := idx.ApplyEvent(&events[0]); err != nil {
		t.Fatalf("ApplyEvent: %v", err)
	}
	_ = idx.Close()

	// Append event 2 to the JSONL
	eventsPath := filepath.Join(specDir, "events.jsonl")
	log, err := store.OpenJsonl(eventsPath)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}
	if err := log.Append(&events[1]); err != nil {
		t.Fatalf("Append: %v", err)
	}
	_ = log.Close()

	// Recovery should detect the mismatch and rebuild SQLite
	state, lastID, err := store.RecoverSpec(specDir)
	if err != nil {
		t.Fatalf("RecoverSpec: %v", err)
	}

	if lastID != 2 {
		t.Errorf("lastID = %d, want 2", lastID)
	}
	if state.Cards.Len() != 1 {
		t.Errorf("cards count = %d, want 1", state.Cards.Len())
	}

	// Verify SQLite was rebuilt
	idx, err = store.OpenSqlite(indexPath)
	if err != nil {
		t.Fatalf("OpenSqlite (verify): %v", err)
	}
	defer func() { _ = idx.Close() }()

	sqliteLastID, found, err := idx.GetLastEventID()
	if err != nil {
		t.Fatalf("GetLastEventID: %v", err)
	}
	if !found || sqliteLastID != 2 {
		t.Errorf("sqlite last_event_id = %d (found=%v), want 2", sqliteLastID, found)
	}

	cards, err := idx.ListCards(specID)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	if cards[0].Title != "Stale Card" {
		t.Errorf("card title = %q, want %q", cards[0].Title, "Stale Card")
	}
}

func TestRecoverEmptySpecDir(t *testing.T) {
	specDir := makeRecoverySpecDir(t)

	state, lastID, err := store.RecoverSpec(specDir)
	if err != nil {
		t.Fatalf("RecoverSpec: %v", err)
	}

	if lastID != 0 {
		t.Errorf("lastID = %d, want 0", lastID)
	}
	if state.Core != nil {
		t.Error("expected nil core for empty spec")
	}
	if state.Cards.Len() != 0 {
		t.Errorf("cards count = %d, want 0", state.Cards.Len())
	}
}

func TestRecoverWithRecoverAllSpecs(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "mammoth_home")

	mgr, err := store.NewStorageManager(home)
	if err != nil {
		t.Fatalf("NewStorageManager: %v", err)
	}

	specID1 := core.NewULID()
	specID2 := core.NewULID()

	specDir1, err := mgr.CreateSpecDir(specID1)
	if err != nil {
		t.Fatalf("CreateSpecDir(1): %v", err)
	}
	specDir2, err := mgr.CreateSpecDir(specID2)
	if err != nil {
		t.Fatalf("CreateSpecDir(2): %v", err)
	}

	// Write events to spec 1
	events1 := []core.Event{
		makeRecoveryEvent(1, specID1, core.SpecCreatedPayload{
			Title: "Spec One", OneLiner: "First", Goal: "Test 1",
		}),
	}
	writeEventsToDir(t, specDir1, events1)

	// Write events to spec 2
	events2 := []core.Event{
		makeRecoveryEvent(1, specID2, core.SpecCreatedPayload{
			Title: "Spec Two", OneLiner: "Second", Goal: "Test 2",
		}),
		makeRecoveryEvent(2, specID2, core.CardCreatedPayload{
			Card: core.NewCard("task", "A Task", "human"),
		}),
	}
	writeEventsToDir(t, specDir2, events2)

	// Recover all
	recovered, err := mgr.RecoverAllSpecs()
	if err != nil {
		t.Fatalf("RecoverAllSpecs: %v", err)
	}

	if len(recovered) != 2 {
		t.Fatalf("expected 2 recovered specs, got %d", len(recovered))
	}

	// Check that both specs were recovered
	foundIDs := map[string]bool{}
	for _, r := range recovered {
		foundIDs[r.SpecID.String()] = true
		if r.State.Core == nil {
			t.Errorf("spec %s has nil core", r.SpecID)
		}
	}
	if !foundIDs[specID1.String()] {
		t.Errorf("missing recovered spec %s", specID1)
	}
	if !foundIDs[specID2.String()] {
		t.Errorf("missing recovered spec %s", specID2)
	}
}

func writeEventsToDir(t *testing.T, specDir string, events []core.Event) {
	t.Helper()
	eventsPath := filepath.Join(specDir, "events.jsonl")
	log, err := store.OpenJsonl(eventsPath)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}
	defer func() { _ = log.Close() }()
	for i := range events {
		if err := log.Append(&events[i]); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
}
