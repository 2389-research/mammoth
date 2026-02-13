// ABOUTME: Wire compatibility tests proving Go can read barnstormer (Rust) JSONL data.
// ABOUTME: Loads real barnstormer fixtures, replays all events, and asserts state correctness.
package core_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/core"
)

// testdataDir returns the absolute path to the testdata directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
}

// loadFixtureEvents reads a JSONL file and returns all parsed events.
func loadFixtureEvents(t *testing.T, filename string) []core.Event {
	t.Helper()
	path := filepath.Join(testdataDir(t), filename)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture %s: %v", filename, err)
	}
	defer func() { _ = f.Close() }()

	var events []core.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for long lines
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var evt core.Event
		if err := json.Unmarshal(line, &evt); err != nil {
			t.Fatalf("line %d: unmarshal event: %v\nJSON: %s", lineNum, err, string(line[:min(len(line), 200)]))
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	return events
}

func TestWireCompat_ParseAllBarnstormerEvents(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_01.jsonl")

	if len(events) != 178 {
		t.Fatalf("expected 178 events, got %d", len(events))
	}

	// Verify event IDs are sequential
	for i, evt := range events {
		expected := uint64(i + 1)
		if evt.EventID != expected {
			t.Errorf("event %d: got EventID %d, want %d", i, evt.EventID, expected)
		}
	}

	// Verify all events have the same spec ID
	specID := events[0].SpecID
	for i, evt := range events {
		if evt.SpecID != specID {
			t.Errorf("event %d: spec_id %s != %s", i, evt.SpecID, specID)
		}
	}
}

func TestWireCompat_ReplayProducesCorrectState(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_01.jsonl")

	state := core.NewSpecState()
	for i := range events {
		state.Apply(&events[i])
	}

	// Spec core must exist
	if state.Core == nil {
		t.Fatal("expected non-nil core after replay")
	}

	// Title was set by SpecCoreUpdated at event 6
	if state.Core.Title != "Team Knowledge Base Discord Bot" {
		t.Errorf("title = %q, want %q", state.Core.Title, "Team Knowledge Base Discord Bot")
	}

	// OneLiner was also set
	if state.Core.OneLiner == "" {
		t.Error("expected non-empty OneLiner")
	}

	// Goal was set
	if state.Core.Goal == "" {
		t.Error("expected non-empty Goal")
	}

	// Description was set
	if state.Core.Description == nil || *state.Core.Description == "" {
		t.Error("expected non-empty Description")
	}

	// Cards: 33 created - 3 deleted = 30 remaining
	if state.Cards.Len() != 30 {
		t.Errorf("cards count = %d, want 30", state.Cards.Len())
	}

	// Transcript should have entries (TranscriptAppended + AgentStepStarted/Finished + QuestionAnswered)
	if len(state.Transcript) == 0 {
		t.Error("expected non-empty transcript")
	}

	// Last event ID should match the last event
	if state.LastEventID != 178 {
		t.Errorf("LastEventID = %d, want 178", state.LastEventID)
	}
}

func TestWireCompat_EventTypesCovered(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_01.jsonl")

	typesSeen := map[string]int{}
	for _, evt := range events {
		typesSeen[evt.Payload.EventPayloadType()]++
	}

	// The fixture contains all these event types
	expectedTypes := []string{
		"SpecCreated",
		"SpecCoreUpdated",
		"CardCreated",
		"CardUpdated",
		"CardMoved",
		"CardDeleted",
		"TranscriptAppended",
		"QuestionAsked",
		"QuestionAnswered",
		"AgentStepStarted",
		"AgentStepFinished",
	}

	for _, et := range expectedTypes {
		count, ok := typesSeen[et]
		if !ok || count == 0 {
			t.Errorf("expected event type %q in fixture, not found", et)
		} else {
			t.Logf("%s: %d events", et, count)
		}
	}
}

func TestWireCompat_CardFieldsPreserved(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_01.jsonl")

	// Find the first CardCreated event and check its fields
	for _, evt := range events {
		p, ok := evt.Payload.(core.CardCreatedPayload)
		if !ok {
			continue
		}

		card := p.Card
		if card.CardID.String() == "" {
			t.Error("CardID is empty")
		}
		if card.CardType == "" {
			t.Error("CardType is empty")
		}
		if card.Title == "" {
			t.Error("Title is empty")
		}
		if card.Lane == "" {
			t.Error("Lane is empty")
		}
		if card.CreatedBy == "" {
			t.Error("CreatedBy is empty")
		}
		if card.UpdatedBy == "" {
			t.Error("UpdatedBy is empty")
		}
		if card.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero")
		}
		if card.UpdatedAt.IsZero() {
			t.Error("UpdatedAt is zero")
		}
		if card.Refs == nil {
			t.Error("Refs should be non-nil (empty slice, not nil)")
		}

		// Found and validated one, that's enough
		return
	}
	t.Fatal("no CardCreated events found")
}

func TestWireCompat_QuestionFreeformFieldsPreserved(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_01.jsonl")

	for _, evt := range events {
		p, ok := evt.Payload.(core.QuestionAskedPayload)
		if !ok {
			continue
		}

		fq, ok := p.Question.(core.FreeformQuestion)
		if !ok {
			continue
		}

		if fq.QID.String() == "" {
			t.Error("Question ID is empty")
		}
		if fq.Question == "" {
			t.Error("Question text is empty")
		}

		// Found and validated one freeform question
		return
	}
	t.Fatal("no Freeform QuestionAsked events found")
}

func TestWireCompat_RoundTripPreservesSemantics(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_01.jsonl")

	// Marshal each event back to JSON and unmarshal again
	// The semantic content must be preserved even if byte representation differs
	for i, evt := range events {
		data, err := json.Marshal(evt)
		if err != nil {
			t.Fatalf("event %d: marshal: %v", i+1, err)
		}

		var roundTripped core.Event
		if err := json.Unmarshal(data, &roundTripped); err != nil {
			t.Fatalf("event %d: unmarshal round-tripped: %v", i+1, err)
		}

		if roundTripped.EventID != evt.EventID {
			t.Errorf("event %d: EventID mismatch after round-trip: %d != %d",
				i+1, roundTripped.EventID, evt.EventID)
		}
		if roundTripped.SpecID != evt.SpecID {
			t.Errorf("event %d: SpecID mismatch after round-trip", i+1)
		}
		if roundTripped.Payload.EventPayloadType() != evt.Payload.EventPayloadType() {
			t.Errorf("event %d: payload type mismatch: %q != %q",
				i+1, roundTripped.Payload.EventPayloadType(), evt.Payload.EventPayloadType())
		}
	}
}

func TestWireCompat_SpecStateJSONRoundTrip(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_01.jsonl")

	state := core.NewSpecState()
	for i := range events {
		state.Apply(&events[i])
	}

	// Marshal state to JSON
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	// Unmarshal back into a new SpecState
	var roundTripped core.SpecState
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}

	// Verify round-tripped state matches
	if roundTripped.Core == nil {
		t.Fatal("round-tripped core is nil")
	}
	if roundTripped.Core.Title != state.Core.Title {
		t.Errorf("title: got %q, want %q", roundTripped.Core.Title, state.Core.Title)
	}
	if roundTripped.Cards.Len() != state.Cards.Len() {
		t.Errorf("cards: got %d, want %d", roundTripped.Cards.Len(), state.Cards.Len())
	}
	if len(roundTripped.Transcript) != len(state.Transcript) {
		t.Errorf("transcript: got %d, want %d", len(roundTripped.Transcript), len(state.Transcript))
	}
	if roundTripped.LastEventID != state.LastEventID {
		t.Errorf("last_event_id: got %d, want %d", roundTripped.LastEventID, state.LastEventID)
	}
	if len(roundTripped.Lanes) != len(state.Lanes) {
		t.Errorf("lanes: got %d, want %d", len(roundTripped.Lanes), len(state.Lanes))
	}

	// Verify individual cards exist and match
	state.Cards.Range(func(id ulid.ULID, card core.Card) bool {
		rtCard, ok := roundTripped.Cards.Get(id)
		if !ok {
			t.Errorf("card %s missing after round-trip", id)
			return true
		}
		if rtCard.Title != card.Title {
			t.Errorf("card %s title: got %q, want %q", id, rtCard.Title, card.Title)
		}
		if rtCard.Lane != card.Lane {
			t.Errorf("card %s lane: got %q, want %q", id, rtCard.Lane, card.Lane)
		}
		return true
	})
}

func TestWireCompat_LargerFixture_ParseAndReplay(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_02.jsonl")

	if len(events) != 394 {
		t.Fatalf("expected 394 events in fixture 02, got %d", len(events))
	}

	state := core.NewSpecState()
	for i := range events {
		state.Apply(&events[i])
	}

	if state.Core == nil {
		t.Fatal("expected non-nil core")
	}
	if state.Core.Title == "" {
		t.Error("expected non-empty title")
	}
	if state.Cards.Len() == 0 {
		t.Error("expected cards after replay")
	}
	if len(state.Transcript) == 0 {
		t.Error("expected transcript entries")
	}

	// State JSON round-trip should also work
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rt core.SpecState
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rt.Cards.Len() != state.Cards.Len() {
		t.Errorf("round-trip cards: got %d, want %d", rt.Cards.Len(), state.Cards.Len())
	}
}

func TestWireCompat_ReplayThenExportDoesNotPanic(t *testing.T) {
	events := loadFixtureEvents(t, "barnstormer_fixture_01.jsonl")

	state := core.NewSpecState()
	for i := range events {
		state.Apply(&events[i])
	}

	// Serialize the entire state to JSON (as the API would)
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("state JSON is empty")
	}

	// Verify it's valid JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("state JSON is invalid: %v", err)
	}

	// Verify expected top-level fields exist
	for _, field := range []string{"core", "cards", "transcript", "undo_stack", "last_event_id", "lanes"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("state JSON missing field %q", field)
		}
	}
}
