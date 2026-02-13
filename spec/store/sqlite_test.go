// ABOUTME: Tests for the SQLite-backed index for spec and card queries.
// ABOUTME: Covers upsert, delete, list, rebuild, incremental apply, and event ID tracking.
package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/store"
	"github.com/oklog/ulid/v2"
)

func makeSpecCore() core.SpecCore {
	return core.NewSpecCore("Test Spec", "A test", "Build it")
}

func makeCard(createdBy string) core.Card {
	return core.NewCard("idea", "Test Card", createdBy)
}

func makeSqliteEvent(eventID uint64, specID ulid.ULID, payload core.EventPayload) core.Event {
	return core.Event{
		EventID:   eventID,
		SpecID:    specID,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

func TestSqliteSpecUpsertAndList(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	idx, err := store.OpenSqlite(dbPath)
	if err != nil {
		t.Fatalf("OpenSqlite: %v", err)
	}
	defer func() { _ = idx.Close() }()

	spec := makeSpecCore()
	if err := idx.UpdateSpec(&spec); err != nil {
		t.Fatalf("UpdateSpec: %v", err)
	}

	specs, err := idx.ListSpecs()
	if err != nil {
		t.Fatalf("ListSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Title != "Test Spec" {
		t.Errorf("title = %q, want %q", specs[0].Title, "Test Spec")
	}
	if specs[0].OneLiner != "A test" {
		t.Errorf("one_liner = %q, want %q", specs[0].OneLiner, "A test")
	}
	if specs[0].Goal != "Build it" {
		t.Errorf("goal = %q, want %q", specs[0].Goal, "Build it")
	}
	if specs[0].SpecID != spec.SpecID.String() {
		t.Errorf("spec_id = %q, want %q", specs[0].SpecID, spec.SpecID.String())
	}

	// Upsert with changed title
	spec.Title = "Updated Spec"
	spec.UpdatedAt = time.Now().UTC()
	if err := idx.UpdateSpec(&spec); err != nil {
		t.Fatalf("UpdateSpec (upsert): %v", err)
	}

	specs, err = idx.ListSpecs()
	if err != nil {
		t.Fatalf("ListSpecs after upsert: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec after upsert, got %d", len(specs))
	}
	if specs[0].Title != "Updated Spec" {
		t.Errorf("title after upsert = %q, want %q", specs[0].Title, "Updated Spec")
	}
}

func TestSqliteCardCRUD(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	idx, err := store.OpenSqlite(dbPath)
	if err != nil {
		t.Fatalf("OpenSqlite: %v", err)
	}
	defer func() { _ = idx.Close() }()

	spec := makeSpecCore()
	if err := idx.UpdateSpec(&spec); err != nil {
		t.Fatalf("UpdateSpec: %v", err)
	}

	// Create card
	card := makeCard("human")
	if err := idx.UpdateCard(spec.SpecID, &card); err != nil {
		t.Fatalf("UpdateCard: %v", err)
	}

	cards, err := idx.ListCards(spec.SpecID)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	if cards[0].Title != "Test Card" {
		t.Errorf("title = %q, want %q", cards[0].Title, "Test Card")
	}
	if cards[0].CardType != "idea" {
		t.Errorf("card_type = %q, want %q", cards[0].CardType, "idea")
	}
	if cards[0].CreatedBy != "human" {
		t.Errorf("created_by = %q, want %q", cards[0].CreatedBy, "human")
	}

	// Update card (upsert)
	card.Title = "Updated Card"
	card.UpdatedAt = time.Now().UTC()
	if err := idx.UpdateCard(spec.SpecID, &card); err != nil {
		t.Fatalf("UpdateCard (upsert): %v", err)
	}

	cards, err = idx.ListCards(spec.SpecID)
	if err != nil {
		t.Fatalf("ListCards after upsert: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card after upsert, got %d", len(cards))
	}
	if cards[0].Title != "Updated Card" {
		t.Errorf("title after upsert = %q, want %q", cards[0].Title, "Updated Card")
	}

	// Delete card
	if err := idx.DeleteCard(card.CardID); err != nil {
		t.Fatalf("DeleteCard: %v", err)
	}

	cards, err = idx.ListCards(spec.SpecID)
	if err != nil {
		t.Fatalf("ListCards after delete: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("expected 0 cards after delete, got %d", len(cards))
	}
}

func TestSqliteRebuildFromEvents(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	idx, err := store.OpenSqlite(dbPath)
	if err != nil {
		t.Fatalf("OpenSqlite: %v", err)
	}
	defer func() { _ = idx.Close() }()

	specID := core.NewULID()
	card := core.NewCard("idea", "Event Card", "agent")
	cardID := card.CardID

	events := []core.Event{
		makeSqliteEvent(1, specID, core.SpecCreatedPayload{
			Title:    "Rebuilt Spec",
			OneLiner: "From events",
			Goal:     "Test rebuild",
		}),
		makeSqliteEvent(2, specID, core.CardCreatedPayload{Card: card}),
		makeSqliteEvent(3, specID, core.CardMovedPayload{
			CardID: cardID,
			Lane:   "Plan",
			Order:  2.0,
		}),
	}

	if err := idx.RebuildFromEvents(events); err != nil {
		t.Fatalf("RebuildFromEvents: %v", err)
	}

	specs, err := idx.ListSpecs()
	if err != nil {
		t.Fatalf("ListSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Title != "Rebuilt Spec" {
		t.Errorf("title = %q, want %q", specs[0].Title, "Rebuilt Spec")
	}

	cards, err := idx.ListCards(specID)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	if cards[0].Title != "Event Card" {
		t.Errorf("card title = %q, want %q", cards[0].Title, "Event Card")
	}
	if cards[0].Lane != "Plan" {
		t.Errorf("card lane = %q, want %q", cards[0].Lane, "Plan")
	}
	if cards[0].SortOrder != 2.0 {
		t.Errorf("card sort_order = %f, want 2.0", cards[0].SortOrder)
	}

	lastID, found, err := idx.GetLastEventID()
	if err != nil {
		t.Fatalf("GetLastEventID: %v", err)
	}
	if !found {
		t.Error("expected last_event_id to be found")
	}
	if lastID != 3 {
		t.Errorf("last_event_id = %d, want 3", lastID)
	}
}

func TestSqliteApplyEventIncrementally(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	idx, err := store.OpenSqlite(dbPath)
	if err != nil {
		t.Fatalf("OpenSqlite: %v", err)
	}
	defer func() { _ = idx.Close() }()

	specID := core.NewULID()

	// Apply SpecCreated
	e1 := makeSqliteEvent(1, specID, core.SpecCreatedPayload{
		Title:    "Incremental",
		OneLiner: "Step by step",
		Goal:     "Test incremental",
	})
	if err := idx.ApplyEvent(&e1); err != nil {
		t.Fatalf("ApplyEvent(SpecCreated): %v", err)
	}

	specs, err := idx.ListSpecs()
	if err != nil {
		t.Fatalf("ListSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Title != "Incremental" {
		t.Errorf("title = %q, want %q", specs[0].Title, "Incremental")
	}

	// Apply CardCreated
	card := core.NewCard("task", "Do Thing", "human")
	cardID := card.CardID
	e2 := makeSqliteEvent(2, specID, core.CardCreatedPayload{Card: card})
	if err := idx.ApplyEvent(&e2); err != nil {
		t.Fatalf("ApplyEvent(CardCreated): %v", err)
	}

	cards, err := idx.ListCards(specID)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	if cards[0].Title != "Do Thing" {
		t.Errorf("card title = %q, want %q", cards[0].Title, "Do Thing")
	}

	// Apply CardUpdated
	newTitle := "Done Thing"
	newBody := "With a body"
	e3 := makeSqliteEvent(3, specID, core.CardUpdatedPayload{
		CardID:   cardID,
		Title:    &newTitle,
		Body:     core.Present(newBody),
		CardType: nil,
	})
	if err := idx.ApplyEvent(&e3); err != nil {
		t.Fatalf("ApplyEvent(CardUpdated): %v", err)
	}

	cards, err = idx.ListCards(specID)
	if err != nil {
		t.Fatalf("ListCards after update: %v", err)
	}
	if cards[0].Title != "Done Thing" {
		t.Errorf("card title = %q, want %q", cards[0].Title, "Done Thing")
	}
	if cards[0].Body == nil || *cards[0].Body != "With a body" {
		t.Errorf("card body = %v, want %q", cards[0].Body, "With a body")
	}

	// Apply CardDeleted
	e4 := makeSqliteEvent(4, specID, core.CardDeletedPayload{CardID: cardID})
	if err := idx.ApplyEvent(&e4); err != nil {
		t.Fatalf("ApplyEvent(CardDeleted): %v", err)
	}

	cards, err = idx.ListCards(specID)
	if err != nil {
		t.Fatalf("ListCards after delete: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("expected 0 cards after delete, got %d", len(cards))
	}

	// Verify last event id
	lastID, found, err := idx.GetLastEventID()
	if err != nil {
		t.Fatalf("GetLastEventID: %v", err)
	}
	if !found || lastID != 4 {
		t.Errorf("last_event_id = %d (found=%v), want 4", lastID, found)
	}
}

func TestSqliteLastEventIDTracking(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	idx, err := store.OpenSqlite(dbPath)
	if err != nil {
		t.Fatalf("OpenSqlite: %v", err)
	}
	defer func() { _ = idx.Close() }()

	// Initially no last event id
	_, found, err := idx.GetLastEventID()
	if err != nil {
		t.Fatalf("GetLastEventID: %v", err)
	}
	if found {
		t.Error("expected no last_event_id initially")
	}

	// Set it
	if err := idx.SetLastEventID(42); err != nil {
		t.Fatalf("SetLastEventID(42): %v", err)
	}
	id, found, err := idx.GetLastEventID()
	if err != nil {
		t.Fatalf("GetLastEventID: %v", err)
	}
	if !found || id != 42 {
		t.Errorf("last_event_id = %d (found=%v), want 42", id, found)
	}

	// Update it
	if err := idx.SetLastEventID(100); err != nil {
		t.Fatalf("SetLastEventID(100): %v", err)
	}
	id, found, err = idx.GetLastEventID()
	if err != nil {
		t.Fatalf("GetLastEventID: %v", err)
	}
	if !found || id != 100 {
		t.Errorf("last_event_id = %d (found=%v), want 100", id, found)
	}
}

func TestSqliteSpecCoreUpdated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	idx, err := store.OpenSqlite(dbPath)
	if err != nil {
		t.Fatalf("OpenSqlite: %v", err)
	}
	defer func() { _ = idx.Close() }()

	specID := core.NewULID()

	// Create spec first
	e1 := makeSqliteEvent(1, specID, core.SpecCreatedPayload{
		Title:    "Original",
		OneLiner: "Original liner",
		Goal:     "Original goal",
	})
	if err := idx.ApplyEvent(&e1); err != nil {
		t.Fatalf("ApplyEvent(SpecCreated): %v", err)
	}

	// Update title only
	newTitle := "Updated Title"
	e2 := makeSqliteEvent(2, specID, core.SpecCoreUpdatedPayload{
		Title: &newTitle,
	})
	if err := idx.ApplyEvent(&e2); err != nil {
		t.Fatalf("ApplyEvent(SpecCoreUpdated): %v", err)
	}

	specs, err := idx.ListSpecs()
	if err != nil {
		t.Fatalf("ListSpecs: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Title != "Updated Title" {
		t.Errorf("title = %q, want %q", specs[0].Title, "Updated Title")
	}
	if specs[0].OneLiner != "Original liner" {
		t.Errorf("one_liner = %q, want %q (should be unchanged)", specs[0].OneLiner, "Original liner")
	}
}

func TestSqliteCardMovedUpdatesLaneAndOrder(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	idx, err := store.OpenSqlite(dbPath)
	if err != nil {
		t.Fatalf("OpenSqlite: %v", err)
	}
	defer func() { _ = idx.Close() }()

	specID := core.NewULID()
	card := core.NewCard("idea", "Movable Card", "human")

	e1 := makeSqliteEvent(1, specID, core.SpecCreatedPayload{
		Title: "Move Test", OneLiner: "Test", Goal: "Test",
	})
	e2 := makeSqliteEvent(2, specID, core.CardCreatedPayload{Card: card})
	e3 := makeSqliteEvent(3, specID, core.CardMovedPayload{
		CardID: card.CardID,
		Lane:   "Spec",
		Order:  3.14,
	})

	for _, e := range []*core.Event{&e1, &e2, &e3} {
		if err := idx.ApplyEvent(e); err != nil {
			t.Fatalf("ApplyEvent: %v", err)
		}
	}

	cards, err := idx.ListCards(specID)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	if cards[0].Lane != "Spec" {
		t.Errorf("lane = %q, want %q", cards[0].Lane, "Spec")
	}
	if cards[0].SortOrder != 3.14 {
		t.Errorf("sort_order = %f, want 3.14", cards[0].SortOrder)
	}
}
