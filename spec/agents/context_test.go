// ABOUTME: Tests for AgentContext creation, event updates, snapshot round-trips, and compaction.
// ABOUTME: Validates rolling summary bounds, decision list bounds, and event cursor behavior.
package agents

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

func TestAgentContextCreation(t *testing.T) {
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "brainstormer-1", RoleBrainstormer)

	if ctx.SpecID != specID {
		t.Errorf("expected spec_id %s, got %s", specID, ctx.SpecID)
	}
	if ctx.AgentID != "brainstormer-1" {
		t.Errorf("expected agent_id 'brainstormer-1', got '%s'", ctx.AgentID)
	}
	if ctx.AgentRole != RoleBrainstormer {
		t.Errorf("expected role Brainstormer, got %v", ctx.AgentRole)
	}
	if ctx.StateSummary != "" {
		t.Error("expected empty state_summary")
	}
	if len(ctx.RecentEvents) != 0 {
		t.Error("expected empty recent_events")
	}
	if len(ctx.RecentTranscript) != 0 {
		t.Error("expected empty recent_transcript")
	}
	if ctx.RollingSummary != "" {
		t.Error("expected empty rolling_summary")
	}
	if len(ctx.KeyDecisions) != 0 {
		t.Error("expected empty key_decisions")
	}
	if ctx.LastEventSeen != 0 {
		t.Errorf("expected last_event_seen 0, got %d", ctx.LastEventSeen)
	}
}

func TestContextSnapshotRoundTrip(t *testing.T) {
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "planner-1", RolePlanner)
	ctx.RollingSummary = "Some accumulated context about the spec"
	ctx.KeyDecisions = append(ctx.KeyDecisions, "Decided to use microservices")
	ctx.KeyDecisions = append(ctx.KeyDecisions, "Chose PostgreSQL over SQLite")
	ctx.LastEventSeen = 42

	snapshot := ctx.ToSnapshotValue()

	restored, err := FromSnapshotValue(snapshot)
	if err != nil {
		t.Fatalf("failed to deserialize snapshot: %v", err)
	}
	if restored.SpecID != specID {
		t.Errorf("expected spec_id %s, got %s", specID, restored.SpecID)
	}
	if restored.AgentID != "planner-1" {
		t.Errorf("expected agent_id 'planner-1', got '%s'", restored.AgentID)
	}
	if restored.AgentRole != RolePlanner {
		t.Errorf("expected role Planner, got %v", restored.AgentRole)
	}
	if restored.RollingSummary != ctx.RollingSummary {
		t.Errorf("rolling_summary mismatch")
	}
	if len(restored.KeyDecisions) != 2 {
		t.Errorf("expected 2 key_decisions, got %d", len(restored.KeyDecisions))
	}
	if restored.LastEventSeen != 42 {
		t.Errorf("expected last_event_seen 42, got %d", restored.LastEventSeen)
	}
}

func TestContextCompactsWhenTooLarge(t *testing.T) {
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "manager-1", RoleManager)

	// Build a rolling summary that exceeds the 2000-char cap.
	longEntry := "Event #999: SomeVariant"
	for i := 0; i < 200; i++ {
		if ctx.RollingSummary == "" {
			ctx.RollingSummary = longEntry
		} else {
			ctx.RollingSummary += "; " + longEntry
		}
	}

	if len(ctx.RollingSummary) <= RollingSummaryCap {
		t.Fatal("expected rolling_summary to exceed cap before compaction")
	}

	ctx.CompactSummary()

	if utf8.RuneCountInString(ctx.RollingSummary) > RollingSummaryCap {
		t.Errorf("rolling_summary should be within cap after compaction, got %d chars",
			utf8.RuneCountInString(ctx.RollingSummary))
	}
	if !strings.HasPrefix(ctx.RollingSummary, "[earlier context compacted]") {
		t.Error("compacted summary should start with '[earlier context compacted]'")
	}
}

func TestContextUpdatesFromEvents(t *testing.T) {
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "critic-1", RoleCritic)

	events := []core.Event{
		{
			EventID:   1,
			SpecID:    specID,
			Timestamp: time.Now().UTC(),
			Payload: core.SpecCreatedPayload{
				Title:    "Test",
				OneLiner: "A test spec",
				Goal:     "Verify updates",
			},
		},
		{
			EventID:   2,
			SpecID:    specID,
			Timestamp: time.Now().UTC(),
			Payload: core.TranscriptAppendedPayload{
				Message: core.NewTranscriptMessage("system", "Spec created"),
			},
		},
	}

	ctx.UpdateFromEvents(events)

	if ctx.LastEventSeen != 2 {
		t.Errorf("expected last_event_seen 2, got %d", ctx.LastEventSeen)
	}
	if ctx.RollingSummary == "" {
		t.Error("expected non-empty rolling_summary")
	}
	if !strings.Contains(ctx.RollingSummary, "Event #1") {
		t.Error("expected rolling_summary to contain 'Event #1'")
	}
	if !strings.Contains(ctx.RollingSummary, "spec created: 'Test'") {
		t.Error("expected rolling_summary to contain spec created description")
	}
	if !strings.Contains(ctx.RollingSummary, "Event #2") {
		t.Error("expected rolling_summary to contain 'Event #2'")
	}
	if !strings.Contains(ctx.RollingSummary, "system said:") {
		t.Error("expected rolling_summary to contain 'system said:'")
	}
}

func TestContextSkipsAlreadySeenEvents(t *testing.T) {
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "critic-1", RoleCritic)
	ctx.LastEventSeen = 5

	events := []core.Event{
		{
			EventID:   3,
			SpecID:    specID,
			Timestamp: time.Now().UTC(),
			Payload: core.SpecCreatedPayload{
				Title:    "Old",
				OneLiner: "Should skip",
				Goal:     "Skip",
			},
		},
		{
			EventID:   6,
			SpecID:    specID,
			Timestamp: time.Now().UTC(),
			Payload: core.TranscriptAppendedPayload{
				Message: core.NewTranscriptMessage("system", "Should process"),
			},
		},
	}

	ctx.UpdateFromEvents(events)

	if ctx.LastEventSeen != 6 {
		t.Errorf("expected last_event_seen 6, got %d", ctx.LastEventSeen)
	}
	// Only event #6 should appear in summary
	if strings.Contains(ctx.RollingSummary, "Event #3") {
		t.Error("event #3 should have been skipped")
	}
	if !strings.Contains(ctx.RollingSummary, "Event #6") {
		t.Error("event #6 should appear in summary")
	}
}

func TestAddDecisionBoundsList(t *testing.T) {
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "manager-1", RoleManager)

	for i := 0; i < 60; i++ {
		ctx.AddDecision(strings.Repeat("Decision ", 1) + string(rune('A'+i%26)))
	}

	if len(ctx.KeyDecisions) != MaxKeyDecisions {
		t.Errorf("expected %d key_decisions, got %d", MaxKeyDecisions, len(ctx.KeyDecisions))
	}
}

func TestAgentRoleLabel(t *testing.T) {
	tests := []struct {
		role  AgentRole
		label string
	}{
		{RoleManager, "manager"},
		{RoleBrainstormer, "brainstormer"},
		{RolePlanner, "planner"},
		{RoleDotGenerator, "dot_generator"},
		{RoleCritic, "critic"},
	}

	for _, tt := range tests {
		if got := tt.role.Label(); got != tt.label {
			t.Errorf("AgentRole(%d).Label() = %q, want %q", tt.role, got, tt.label)
		}
	}
}

func TestMultiContextSnapshotRoundTrip(t *testing.T) {
	specID := core.NewULID()

	ctxA := NewAgentContext(specID, "manager-1", RoleManager)
	ctxA.RollingSummary = "Manager saw 5 events"
	ctxA.LastEventSeen = 5
	ctxA.AddDecision("Use REST API")

	ctxB := NewAgentContext(specID, "brainstormer-1", RoleBrainstormer)
	ctxB.RollingSummary = "Brainstormer explored ideas"
	ctxB.LastEventSeen = 3

	m := ContextsToSnapshotMap([]*AgentContext{ctxA, ctxB})
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if _, ok := m["manager-1"]; !ok {
		t.Error("expected 'manager-1' key in map")
	}
	if _, ok := m["brainstormer-1"]; !ok {
		t.Error("expected 'brainstormer-1' key in map")
	}

	restored := ContextsFromSnapshotMap(m)
	if len(restored) != 2 {
		t.Fatalf("expected 2 restored contexts, got %d", len(restored))
	}

	var restoredManager, restoredBrainstormer *AgentContext
	for _, ctx := range restored {
		switch ctx.AgentID {
		case "manager-1":
			restoredManager = ctx
		case "brainstormer-1":
			restoredBrainstormer = ctx
		}
	}

	if restoredManager == nil {
		t.Fatal("expected to find manager-1 in restored contexts")
	}
	if restoredManager.RollingSummary != "Manager saw 5 events" {
		t.Errorf("expected 'Manager saw 5 events', got '%s'", restoredManager.RollingSummary)
	}
	if restoredManager.LastEventSeen != 5 {
		t.Errorf("expected last_event_seen 5, got %d", restoredManager.LastEventSeen)
	}
	if len(restoredManager.KeyDecisions) != 1 || restoredManager.KeyDecisions[0] != "Use REST API" {
		t.Errorf("key_decisions mismatch: %v", restoredManager.KeyDecisions)
	}

	if restoredBrainstormer == nil {
		t.Fatal("expected to find brainstormer-1 in restored contexts")
	}
	if restoredBrainstormer.RollingSummary != "Brainstormer explored ideas" {
		t.Errorf("expected 'Brainstormer explored ideas', got '%s'", restoredBrainstormer.RollingSummary)
	}
}

func TestCompactSummaryHandlesNonASCII(t *testing.T) {
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "manager-1", RoleManager)

	// Build a summary with multi-byte characters that exceeds the cap.
	emojiEntry := "Event #1: \U0001F680\U0001F525\u2728 launched \u4e16\u754c"
	for i := 0; i < 200; i++ {
		if ctx.RollingSummary == "" {
			ctx.RollingSummary = emojiEntry
		} else {
			ctx.RollingSummary += "; " + emojiEntry
		}
	}

	if len(ctx.RollingSummary) <= RollingSummaryCap {
		t.Fatal("expected rolling_summary to exceed cap before compaction")
	}

	// This must not panic on non-ASCII char boundaries
	ctx.CompactSummary()

	if utf8.RuneCountInString(ctx.RollingSummary) > RollingSummaryCap {
		t.Errorf("compacted summary should be within cap, got %d chars",
			utf8.RuneCountInString(ctx.RollingSummary))
	}
	if !strings.HasPrefix(ctx.RollingSummary, "[earlier context compacted]") {
		t.Error("compacted summary should start with '[earlier context compacted]'")
	}
}

func TestContextsFromSnapshotMapSkipsInvalid(t *testing.T) {
	m := make(map[string]json.RawMessage)

	// Valid context
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "valid-1", RolePlanner)
	m["valid-1"] = ctx.ToSnapshotValue()

	// Invalid entry (not valid JSON at all)
	m["invalid-1"] = json.RawMessage(`this is not valid json`)

	restored := ContextsFromSnapshotMap(m)
	if len(restored) != 1 {
		t.Fatalf("expected 1 restored context, got %d", len(restored))
	}
	if restored[0].AgentID != "valid-1" {
		t.Errorf("expected agent_id 'valid-1', got '%s'", restored[0].AgentID)
	}
}

func TestDescribeEventPayload(t *testing.T) {
	card := core.NewCard("idea", "Cache Layer", "brainstormer-1")

	payloadsAndExpected := []struct {
		payload  core.EventPayload
		expected string
	}{
		{
			core.SpecCreatedPayload{Title: "My App", OneLiner: "An app", Goal: "Build it"},
			"spec created: 'My App'",
		},
		{
			core.SpecCoreUpdatedPayload{Title: strPtr("Renamed")},
			"spec updated (title -> 'Renamed')",
		},
		{
			core.CardCreatedPayload{Card: card},
			"card created: 'Cache Layer' (idea)",
		},
		{
			core.CardMovedPayload{CardID: card.CardID, Lane: "Spec", Order: 1.0},
			"moved to 'Spec'",
		},
		{
			core.CardDeletedPayload{CardID: card.CardID},
			"deleted",
		},
		{
			core.QuestionAskedPayload{Question: core.BooleanQuestion{QID: core.NewULID(), Question: "Proceed?"}},
			"question asked to user",
		},
		{
			core.AgentStepStartedPayload{AgentID: "planner-1", Description: "Planning phase"},
			"agent planner-1 started: Planning phase",
		},
		{
			core.UndoAppliedPayload{TargetEventID: 7, InverseEvents: []core.EventPayload{}},
			"undo applied to event #7",
		},
		{
			core.SnapshotWrittenPayload{SnapshotID: 42},
			"snapshot #42 written",
		},
	}

	for _, tc := range payloadsAndExpected {
		desc := describeEventPayload(tc.payload)
		if !strings.Contains(desc, tc.expected) {
			t.Errorf("expected '%s' to contain '%s', got '%s'", desc, tc.expected, desc)
		}
	}
}

func TestCompactionPreservesRecentEntriesAfterEvents(t *testing.T) {
	specID := core.NewULID()
	ctx := NewAgentContext(specID, "manager-1", RoleManager)

	// Feed many events to trigger compaction
	events := make([]core.Event, 100)
	for i := 0; i < 100; i++ {
		events[i] = core.Event{
			EventID:   uint64(i + 1),
			SpecID:    specID,
			Timestamp: time.Now().UTC(),
			Payload: core.TranscriptAppendedPayload{
				Message: core.NewTranscriptMessage(
					"agent-"+string(rune('0'+i%5)),
					"Message number "+string(rune('0'+i%10))+" with some extra padding to fill space",
				),
			},
		}
	}

	ctx.UpdateFromEvents(events)

	if ctx.LastEventSeen != 100 {
		t.Errorf("expected last_event_seen 100, got %d", ctx.LastEventSeen)
	}
	if utf8.RuneCountInString(ctx.RollingSummary) > RollingSummaryCap {
		t.Errorf("rolling_summary should be within cap, got %d chars",
			utf8.RuneCountInString(ctx.RollingSummary))
	}
	if !strings.Contains(ctx.RollingSummary, "Event #100") {
		t.Error("expected rolling_summary to contain 'Event #100'")
	}
}

func TestTruncateChars(t *testing.T) {
	// Short string is not truncated
	if got := truncateChars("hello", 10); got != "hello" {
		t.Errorf("expected 'hello', got '%s'", got)
	}
	// Long string is truncated with ...
	if got := truncateChars("hello world", 5); got != "hello..." {
		t.Errorf("expected 'hello...', got '%s'", got)
	}
	// Multi-byte: 60 emoji characters (each 4 bytes), truncated to 50
	emojiContent := strings.Repeat("\U0001F600", 60)
	got := truncateChars(emojiContent, 50)
	if !strings.HasSuffix(got, "...") {
		t.Error("truncated emoji string should end with '...'")
	}
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}

// Needed for tests to resolve ulid in imports
var _ ulid.ULID
