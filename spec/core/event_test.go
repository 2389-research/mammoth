// ABOUTME: Tests for Event and EventPayload tagged union JSON serialization.
// ABOUTME: Covers round-trips for all 13 payload variants including nested types.
package core_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

func TestEventEnvelope_RoundTrip(t *testing.T) {
	specID := core.NewULID()
	ts := time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)
	evt := core.Event{
		EventID:   42,
		SpecID:    specID,
		Timestamp: ts,
		Payload: core.SpecCreatedPayload{
			Title:    "Test",
			OneLiner: "One line",
			Goal:     "Goal",
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got core.Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.EventID != 42 {
		t.Errorf("EventID: got %d, want 42", got.EventID)
	}
	if got.SpecID != specID {
		t.Errorf("SpecID: got %s, want %s", got.SpecID, specID)
	}
	if !got.Timestamp.Equal(ts) {
		t.Errorf("Timestamp: got %v, want %v", got.Timestamp, ts)
	}
}

func TestMarshalEventPayload_NilReturnsError(t *testing.T) {
	_, err := core.MarshalEventPayload(nil)
	if err == nil {
		t.Fatal("expected error for nil payload, got nil")
	}
}

func TestUnmarshalEventPayload_UnknownTypeReturnsError(t *testing.T) {
	data := []byte(`{"type":"BogusPayload"}`)
	_, err := core.UnmarshalEventPayload(data)
	if err == nil {
		t.Fatal("expected error for unknown event payload type, got nil")
	}
}

func TestUnmarshalEventPayload_InvalidJSONReturnsError(t *testing.T) {
	_, err := core.UnmarshalEventPayload([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSpecCreatedPayload_RoundTrip(t *testing.T) {
	p := core.SpecCreatedPayload{
		Title:    "My Spec",
		OneLiner: "Quick summary",
		Goal:     "Build great things",
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "SpecCreated")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.SpecCreatedPayload)
	if !ok {
		t.Fatalf("expected SpecCreatedPayload, got %T", got)
	}
	if result.Title != p.Title {
		t.Errorf("Title: got %q, want %q", result.Title, p.Title)
	}
	if result.OneLiner != p.OneLiner {
		t.Errorf("OneLiner: got %q, want %q", result.OneLiner, p.OneLiner)
	}
	if result.Goal != p.Goal {
		t.Errorf("Goal: got %q, want %q", result.Goal, p.Goal)
	}
}

func TestSpecCoreUpdatedPayload_RoundTrip(t *testing.T) {
	title := "Updated"
	risks := "Some risks"
	p := core.SpecCoreUpdatedPayload{
		Title: &title,
		Risks: &risks,
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "SpecCoreUpdated")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.SpecCoreUpdatedPayload)
	if !ok {
		t.Fatalf("expected SpecCoreUpdatedPayload, got %T", got)
	}
	if result.Title == nil || *result.Title != title {
		t.Errorf("Title: got %v, want %q", result.Title, title)
	}
	if result.Risks == nil || *result.Risks != risks {
		t.Errorf("Risks: got %v, want %q", result.Risks, risks)
	}
	if result.Goal != nil {
		t.Errorf("Goal: expected nil, got %q", *result.Goal)
	}
}

func TestCardCreatedPayload_RoundTrip(t *testing.T) {
	card := core.NewCard("task", "Test card", "agent-1")
	p := core.CardCreatedPayload{Card: card}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "CardCreated")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.CardCreatedPayload)
	if !ok {
		t.Fatalf("expected CardCreatedPayload, got %T", got)
	}
	if result.Card.CardID != card.CardID {
		t.Errorf("Card.CardID: got %s, want %s", result.Card.CardID, card.CardID)
	}
	if result.Card.Title != card.Title {
		t.Errorf("Card.Title: got %q, want %q", result.Card.Title, card.Title)
	}
	if result.Card.CardType != card.CardType {
		t.Errorf("Card.CardType: got %q, want %q", result.Card.CardType, card.CardType)
	}
	if result.Card.CreatedBy != card.CreatedBy {
		t.Errorf("Card.CreatedBy: got %q, want %q", result.Card.CreatedBy, card.CreatedBy)
	}
}

func TestCardUpdatedPayload_BodyPresent_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	title := "New title"
	p := core.CardUpdatedPayload{
		CardID: cardID,
		Title:  &title,
		Body:   core.Present("updated body text"),
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "CardUpdated")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.CardUpdatedPayload)
	if !ok {
		t.Fatalf("expected CardUpdatedPayload, got %T", got)
	}
	if result.CardID != cardID {
		t.Errorf("CardID: got %s, want %s", result.CardID, cardID)
	}
	if result.Title == nil || *result.Title != title {
		t.Errorf("Title: got %v, want %q", result.Title, title)
	}
	if !result.Body.Set || !result.Body.Valid {
		t.Fatal("Body: expected Present")
	}
	if result.Body.Value != "updated body text" {
		t.Errorf("Body.Value: got %q, want %q", result.Body.Value, "updated body text")
	}
}

func TestCardUpdatedPayload_BodyNull_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	p := core.CardUpdatedPayload{
		CardID: cardID,
		Body:   core.Null[string](),
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify body is present and null in JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	bodyRaw, ok := raw["body"]
	if !ok {
		t.Fatal("expected body field to be present in JSON for Null")
	}
	if string(bodyRaw) != "null" {
		t.Errorf("body JSON: got %s, want null", bodyRaw)
	}

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.CardUpdatedPayload)
	if !ok {
		t.Fatalf("expected CardUpdatedPayload, got %T", got)
	}
	if !result.Body.Set {
		t.Fatal("Body.Set: expected true")
	}
	if result.Body.Valid {
		t.Fatal("Body.Valid: expected false for Null body")
	}
}

func TestCardUpdatedPayload_BodyAbsent_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	p := core.CardUpdatedPayload{
		CardID: cardID,
		Body:   core.Absent[string](),
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify body is absent from JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, present := raw["body"]; present {
		t.Fatal("expected body field to be absent from JSON for Absent")
	}

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.CardUpdatedPayload)
	if !ok {
		t.Fatalf("expected CardUpdatedPayload, got %T", got)
	}
	if result.Body.Set {
		t.Fatal("Body.Set: expected false for Absent body")
	}
}

func TestCardMovedPayload_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	p := core.CardMovedPayload{
		CardID: cardID,
		Lane:   "Done",
		Order:  3.14,
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "CardMoved")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.CardMovedPayload)
	if !ok {
		t.Fatalf("expected CardMovedPayload, got %T", got)
	}
	if result.CardID != cardID {
		t.Errorf("CardID: got %s, want %s", result.CardID, cardID)
	}
	if result.Lane != p.Lane {
		t.Errorf("Lane: got %q, want %q", result.Lane, p.Lane)
	}
	if result.Order != p.Order {
		t.Errorf("Order: got %f, want %f", result.Order, p.Order)
	}
}

func TestCardDeletedPayload_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	p := core.CardDeletedPayload{CardID: cardID}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "CardDeleted")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.CardDeletedPayload)
	if !ok {
		t.Fatalf("expected CardDeletedPayload, got %T", got)
	}
	if result.CardID != cardID {
		t.Errorf("CardID: got %s, want %s", result.CardID, cardID)
	}
}

func TestTranscriptAppendedPayload_RoundTrip(t *testing.T) {
	msg := core.NewTranscriptMessage("human", "Hello there")
	p := core.TranscriptAppendedPayload{Message: msg}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "TranscriptAppended")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.TranscriptAppendedPayload)
	if !ok {
		t.Fatalf("expected TranscriptAppendedPayload, got %T", got)
	}
	if result.Message.MessageID != msg.MessageID {
		t.Errorf("MessageID: got %s, want %s", result.Message.MessageID, msg.MessageID)
	}
	if result.Message.Sender != msg.Sender {
		t.Errorf("Sender: got %q, want %q", result.Message.Sender, msg.Sender)
	}
	if result.Message.Content != msg.Content {
		t.Errorf("Content: got %q, want %q", result.Message.Content, msg.Content)
	}
	if result.Message.Kind != core.MessageKindChat {
		t.Errorf("Kind: got %q, want %q", result.Message.Kind, core.MessageKindChat)
	}
}

func TestQuestionAskedPayload_Boolean_RoundTrip(t *testing.T) {
	qid := core.NewULID()
	defaultVal := false
	p := core.QuestionAskedPayload{
		Question: core.BooleanQuestion{
			QID:      qid,
			Question: "Continue?",
			Default:  &defaultVal,
		},
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "QuestionAsked")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.QuestionAskedPayload)
	if !ok {
		t.Fatalf("expected QuestionAskedPayload, got %T", got)
	}
	bq, ok := result.Question.(core.BooleanQuestion)
	if !ok {
		t.Fatalf("expected BooleanQuestion, got %T", result.Question)
	}
	if bq.QID != qid {
		t.Errorf("QID: got %s, want %s", bq.QID, qid)
	}
	if bq.Question != "Continue?" {
		t.Errorf("Question: got %q, want %q", bq.Question, "Continue?")
	}
	if bq.Default == nil || *bq.Default != false {
		t.Errorf("Default: got %v, want false", bq.Default)
	}
}

func TestQuestionAskedPayload_MultipleChoice_RoundTrip(t *testing.T) {
	qid := core.NewULID()
	p := core.QuestionAskedPayload{
		Question: core.MultipleChoiceQuestion{
			QID:        qid,
			Question:   "Which approach?",
			Choices:    []string{"A", "B", "C"},
			AllowMulti: false,
		},
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.QuestionAskedPayload)
	if !ok {
		t.Fatalf("expected QuestionAskedPayload, got %T", got)
	}
	mc, ok := result.Question.(core.MultipleChoiceQuestion)
	if !ok {
		t.Fatalf("expected MultipleChoiceQuestion, got %T", result.Question)
	}
	if mc.QID != qid {
		t.Errorf("QID: got %s, want %s", mc.QID, qid)
	}
	if len(mc.Choices) != 3 {
		t.Fatalf("Choices: got %d items, want 3", len(mc.Choices))
	}
	if mc.AllowMulti {
		t.Error("AllowMulti: got true, want false")
	}
}

func TestQuestionAskedPayload_Freeform_RoundTrip(t *testing.T) {
	qid := core.NewULID()
	p := core.QuestionAskedPayload{
		Question: core.FreeformQuestion{
			QID:      qid,
			Question: "What do you think?",
		},
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.QuestionAskedPayload)
	if !ok {
		t.Fatalf("expected QuestionAskedPayload, got %T", got)
	}
	fq, ok := result.Question.(core.FreeformQuestion)
	if !ok {
		t.Fatalf("expected FreeformQuestion, got %T", result.Question)
	}
	if fq.QID != qid {
		t.Errorf("QID: got %s, want %s", fq.QID, qid)
	}
	if fq.Question != "What do you think?" {
		t.Errorf("Question: got %q, want %q", fq.Question, "What do you think?")
	}
	if fq.Placeholder != nil {
		t.Errorf("Placeholder: expected nil, got %q", *fq.Placeholder)
	}
	if fq.ValidationHint != nil {
		t.Errorf("ValidationHint: expected nil, got %q", *fq.ValidationHint)
	}
}

func TestQuestionAnsweredPayload_RoundTrip(t *testing.T) {
	qid := core.NewULID()
	p := core.QuestionAnsweredPayload{
		QuestionID: qid,
		Answer:     "Go with option B",
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "QuestionAnswered")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.QuestionAnsweredPayload)
	if !ok {
		t.Fatalf("expected QuestionAnsweredPayload, got %T", got)
	}
	if result.QuestionID != qid {
		t.Errorf("QuestionID: got %s, want %s", result.QuestionID, qid)
	}
	if result.Answer != p.Answer {
		t.Errorf("Answer: got %q, want %q", result.Answer, p.Answer)
	}
}

func TestAgentStepStartedPayload_RoundTrip(t *testing.T) {
	p := core.AgentStepStartedPayload{
		AgentID:     "agent-99",
		Description: "Running analysis",
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "AgentStepStarted")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.AgentStepStartedPayload)
	if !ok {
		t.Fatalf("expected AgentStepStartedPayload, got %T", got)
	}
	if result.AgentID != p.AgentID {
		t.Errorf("AgentID: got %q, want %q", result.AgentID, p.AgentID)
	}
	if result.Description != p.Description {
		t.Errorf("Description: got %q, want %q", result.Description, p.Description)
	}
}

func TestAgentStepFinishedPayload_RoundTrip(t *testing.T) {
	p := core.AgentStepFinishedPayload{
		AgentID:     "agent-99",
		DiffSummary: "+20 -5 in 2 files",
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "AgentStepFinished")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.AgentStepFinishedPayload)
	if !ok {
		t.Fatalf("expected AgentStepFinishedPayload, got %T", got)
	}
	if result.AgentID != p.AgentID {
		t.Errorf("AgentID: got %q, want %q", result.AgentID, p.AgentID)
	}
	if result.DiffSummary != p.DiffSummary {
		t.Errorf("DiffSummary: got %q, want %q", result.DiffSummary, p.DiffSummary)
	}
}

func TestUndoAppliedPayload_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	p := core.UndoAppliedPayload{
		TargetEventID: 7,
		InverseEvents: []core.EventPayload{
			core.CardDeletedPayload{CardID: cardID},
			core.SpecCoreUpdatedPayload{
				Title: strPtr("Reverted title"),
			},
		},
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "UndoApplied")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.UndoAppliedPayload)
	if !ok {
		t.Fatalf("expected UndoAppliedPayload, got %T", got)
	}
	if result.TargetEventID != 7 {
		t.Errorf("TargetEventID: got %d, want 7", result.TargetEventID)
	}
	if len(result.InverseEvents) != 2 {
		t.Fatalf("InverseEvents: got %d items, want 2", len(result.InverseEvents))
	}

	inv0, ok := result.InverseEvents[0].(core.CardDeletedPayload)
	if !ok {
		t.Fatalf("InverseEvents[0]: expected CardDeletedPayload, got %T", result.InverseEvents[0])
	}
	if inv0.CardID != cardID {
		t.Errorf("InverseEvents[0].CardID: got %s, want %s", inv0.CardID, cardID)
	}

	inv1, ok := result.InverseEvents[1].(core.SpecCoreUpdatedPayload)
	if !ok {
		t.Fatalf("InverseEvents[1]: expected SpecCoreUpdatedPayload, got %T", result.InverseEvents[1])
	}
	if inv1.Title == nil || *inv1.Title != "Reverted title" {
		t.Errorf("InverseEvents[1].Title: got %v, want %q", inv1.Title, "Reverted title")
	}
}

func TestUndoAppliedPayload_EmptyInverseEvents_RoundTrip(t *testing.T) {
	p := core.UndoAppliedPayload{
		TargetEventID: 99,
		InverseEvents: []core.EventPayload{},
	}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.UndoAppliedPayload)
	if !ok {
		t.Fatalf("expected UndoAppliedPayload, got %T", got)
	}
	if result.TargetEventID != 99 {
		t.Errorf("TargetEventID: got %d, want 99", result.TargetEventID)
	}
	if len(result.InverseEvents) != 0 {
		t.Errorf("InverseEvents: got %d items, want 0", len(result.InverseEvents))
	}
}

func TestSnapshotWrittenPayload_RoundTrip(t *testing.T) {
	p := core.SnapshotWrittenPayload{SnapshotID: 123}

	data, err := core.MarshalEventPayload(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertPayloadType(t, data, "SnapshotWritten")

	got, err := core.UnmarshalEventPayload(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.SnapshotWrittenPayload)
	if !ok {
		t.Fatalf("expected SnapshotWrittenPayload, got %T", got)
	}
	if result.SnapshotID != 123 {
		t.Errorf("SnapshotID: got %d, want 123", result.SnapshotID)
	}
}

func TestEventFullRoundTrip_WithComplexPayload(t *testing.T) {
	specID := core.NewULID()
	cardID := core.NewULID()
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	evt := core.Event{
		EventID:   100,
		SpecID:    specID,
		Timestamp: ts,
		Payload: core.UndoAppliedPayload{
			TargetEventID: 50,
			InverseEvents: []core.EventPayload{
				core.CardUpdatedPayload{
					CardID: cardID,
					Body:   core.Present("restored body"),
				},
			},
		},
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var got core.Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if got.EventID != 100 {
		t.Errorf("EventID: got %d, want 100", got.EventID)
	}
	if got.SpecID != specID {
		t.Errorf("SpecID: got %s, want %s", got.SpecID, specID)
	}

	undo, ok := got.Payload.(core.UndoAppliedPayload)
	if !ok {
		t.Fatalf("expected UndoAppliedPayload, got %T", got.Payload)
	}
	if undo.TargetEventID != 50 {
		t.Errorf("TargetEventID: got %d, want 50", undo.TargetEventID)
	}
	if len(undo.InverseEvents) != 1 {
		t.Fatalf("InverseEvents: got %d items, want 1", len(undo.InverseEvents))
	}

	cu, ok := undo.InverseEvents[0].(core.CardUpdatedPayload)
	if !ok {
		t.Fatalf("InverseEvents[0]: expected CardUpdatedPayload, got %T", undo.InverseEvents[0])
	}
	if cu.CardID != cardID {
		t.Errorf("CardID: got %s, want %s", cu.CardID, cardID)
	}
	if !cu.Body.Set || !cu.Body.Valid || cu.Body.Value != "restored body" {
		t.Errorf("Body: got {Set:%v Valid:%v Value:%q}, want Present(\"restored body\")",
			cu.Body.Set, cu.Body.Valid, cu.Body.Value)
	}
}

// assertPayloadType checks the "type" discriminator field in the serialized JSON.
func assertPayloadType(t *testing.T, data []byte, expected string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal raw JSON: %v", err)
	}
	typeRaw, ok := m["type"]
	if !ok {
		t.Fatal("JSON missing 'type' field")
	}
	var typeStr string
	if err := json.Unmarshal(typeRaw, &typeStr); err != nil {
		t.Fatalf("unmarshal type field: %v", err)
	}
	if typeStr != expected {
		t.Errorf("type field: got %q, want %q", typeStr, expected)
	}
}

// strPtr is a helper that returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}

// Ensure ulid import is used.
var _ ulid.ULID
