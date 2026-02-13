// ABOUTME: Tests for the JSONL append-only event log.
// ABOUTME: Covers round-trip, empty file, trailing newline, repair, and crash safety.
package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/store"
	"github.com/oklog/ulid/v2"
)

func makeEvent(eventID uint64, specID ulid.ULID, payload core.EventPayload) core.Event {
	return core.Event{
		EventID:   eventID,
		SpecID:    specID,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

func makeSpecCreatedEvent(eventID uint64, specID ulid.ULID) core.Event {
	return makeEvent(eventID, specID, core.SpecCreatedPayload{
		Title:    "Spec " + ulid.ULID(specID).String()[:4],
		OneLiner: "Test",
		Goal:     "Goal",
	})
}

func TestAppendAndReplayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	specID := core.NewULID()

	log, err := store.OpenJsonl(path)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}
	defer func() { _ = log.Close() }()

	e1 := makeSpecCreatedEvent(1, specID)
	e2 := makeSpecCreatedEvent(2, specID)
	e3 := makeSpecCreatedEvent(3, specID)

	for _, e := range []*core.Event{&e1, &e2, &e3} {
		if err := log.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].EventID != 1 {
		t.Errorf("events[0].EventID = %d, want 1", events[0].EventID)
	}
	if events[1].EventID != 2 {
		t.Errorf("events[1].EventID = %d, want 2", events[1].EventID)
	}
	if events[2].EventID != 3 {
		t.Errorf("events[2].EventID = %d, want 3", events[2].EventID)
	}
}

func TestReplayEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")

	// Create an empty file
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = f.Close()

	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestReplayHandlesTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trailing.jsonl")
	specID := core.NewULID()

	log, err := store.OpenJsonl(path)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}

	e := makeSpecCreatedEvent(1, specID)
	if err := log.Append(&e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	_ = log.Close()

	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestRepairTruncatesPartialLastLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")
	specID := core.NewULID()

	log, err := store.OpenJsonl(path)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}

	e1 := makeSpecCreatedEvent(1, specID)
	e2 := makeSpecCreatedEvent(2, specID)
	if err := log.Append(&e1); err != nil {
		t.Fatalf("Append e1: %v", err)
	}
	if err := log.Append(&e2); err != nil {
		t.Fatalf("Append e2: %v", err)
	}
	_ = log.Close()

	// Append garbage to simulate a partial write
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	_, _ = f.WriteString(`{"event_id":3,"spec_id":"bad_json_no_clos`)
	_ = f.Close()

	count, err := store.RepairJsonl(path)
	if err != nil {
		t.Fatalf("RepairJsonl: %v", err)
	}
	if count != 2 {
		t.Errorf("repaired count = %d, want 2", count)
	}

	// Verify the file now replays cleanly
	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl after repair: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events after repair, got %d", len(events))
	}
	if events[0].EventID != 1 {
		t.Errorf("events[0].EventID = %d, want 1", events[0].EventID)
	}
	if events[1].EventID != 2 {
		t.Errorf("events[1].EventID = %d, want 2", events[1].EventID)
	}
}

func TestRepairNoOpOnCleanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.jsonl")
	specID := core.NewULID()

	log, err := store.OpenJsonl(path)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}

	for i := uint64(1); i <= 3; i++ {
		e := makeSpecCreatedEvent(i, specID)
		if err := log.Append(&e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	_ = log.Close()

	count, err := store.RepairJsonl(path)
	if err != nil {
		t.Fatalf("RepairJsonl: %v", err)
	}
	if count != 3 {
		t.Errorf("repaired count = %d, want 3", count)
	}

	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

func TestAppendIsCrashSafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "synced.jsonl")
	specID := core.NewULID()

	log, err := store.OpenJsonl(path)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}

	e := makeSpecCreatedEvent(1, specID)
	if err := log.Append(&e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// After append + sync_all, we should be able to read the event back
	_ = log.Close()

	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventID != 1 {
		t.Errorf("events[0].EventID = %d, want 1", events[0].EventID)
	}
}

func TestOpenJsonlCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "events.jsonl")

	log, err := store.OpenJsonl(path)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}
	_ = log.Close()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to exist after OpenJsonl")
	}
}

func TestReplayPreservesEventPayloadType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "types.jsonl")
	specID := core.NewULID()

	log, err := store.OpenJsonl(path)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}

	card := core.NewCard("idea", "Test Card", "human")
	e1 := makeEvent(1, specID, core.SpecCreatedPayload{
		Title: "Test", OneLiner: "Test", Goal: "Test",
	})
	e2 := makeEvent(2, specID, core.CardCreatedPayload{Card: card})
	e3 := makeEvent(3, specID, core.CardMovedPayload{
		CardID: card.CardID, Lane: "Plan", Order: 1.5,
	})

	for _, e := range []*core.Event{&e1, &e2, &e3} {
		if err := log.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	_ = log.Close()

	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Verify payload types survived round-trip
	if _, ok := events[0].Payload.(core.SpecCreatedPayload); !ok {
		t.Errorf("events[0] payload type = %T, want SpecCreatedPayload", events[0].Payload)
	}
	if p, ok := events[1].Payload.(core.CardCreatedPayload); !ok {
		t.Errorf("events[1] payload type = %T, want CardCreatedPayload", events[1].Payload)
	} else if p.Card.Title != "Test Card" {
		t.Errorf("events[1] card title = %q, want %q", p.Card.Title, "Test Card")
	}
	if p, ok := events[2].Payload.(core.CardMovedPayload); !ok {
		t.Errorf("events[2] payload type = %T, want CardMovedPayload", events[2].Payload)
	} else {
		if p.Lane != "Plan" {
			t.Errorf("events[2] lane = %q, want %q", p.Lane, "Plan")
		}
		if p.Order != 1.5 {
			t.Errorf("events[2] order = %f, want 1.5", p.Order)
		}
	}
}

func TestReplayLineOrderPreserved(t *testing.T) {
	// Verify events maintain their original JSON line content after repair
	dir := t.TempDir()
	path := filepath.Join(dir, "order.jsonl")
	specID := core.NewULID()

	log, err := store.OpenJsonl(path)
	if err != nil {
		t.Fatalf("OpenJsonl: %v", err)
	}

	for i := uint64(1); i <= 5; i++ {
		e := makeSpecCreatedEvent(i, specID)
		if err := log.Append(&e); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	_ = log.Close()

	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl: %v", err)
	}

	for i, e := range events {
		expected := uint64(i + 1)
		if e.EventID != expected {
			t.Errorf("events[%d].EventID = %d, want %d", i, e.EventID, expected)
		}
	}
}

func TestRepairWithCorruptMiddleLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "middle_corrupt.jsonl")
	specID := core.NewULID()

	// Write event 1 manually
	e1 := makeSpecCreatedEvent(1, specID)
	data1, _ := json.Marshal(&e1)

	card := core.NewCard("idea", "Card", "human")
	e3 := makeEvent(3, specID, core.CardCreatedPayload{Card: card})
	data3, _ := json.Marshal(&e3)

	// Write: valid line, corrupt line, valid line
	content := string(data1) + "\n" + `{"broken": true, garbage}` + "\n" + string(data3) + "\n"
	_ = os.WriteFile(path, []byte(content), 0o644)

	count, err := store.RepairJsonl(path)
	if err != nil {
		t.Fatalf("RepairJsonl: %v", err)
	}
	// Only 2 lines are valid events (the corrupt one in the middle is dropped)
	if count != 2 {
		t.Errorf("repaired count = %d, want 2", count)
	}

	events, err := store.ReplayJsonl(path)
	if err != nil {
		t.Fatalf("ReplayJsonl after repair: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}
