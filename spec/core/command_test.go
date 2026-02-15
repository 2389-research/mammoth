// ABOUTME: Tests for Command tagged union JSON serialization.
// ABOUTME: Covers round-trips for all 12 variants including OptionalField body.
package core_test

import (
	"encoding/json"
	"testing"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/oklog/ulid/v2"
)

func TestMarshalCommand_NilReturnsError(t *testing.T) {
	_, err := core.MarshalCommand(nil)
	if err == nil {
		t.Fatal("expected error for nil command, got nil")
	}
}

func TestUnmarshalCommand_UnknownTypeReturnsError(t *testing.T) {
	data := []byte(`{"type":"BogusType"}`)
	_, err := core.UnmarshalCommand(data)
	if err == nil {
		t.Fatal("expected error for unknown command type, got nil")
	}
}

func TestUnmarshalCommand_InvalidJSONReturnsError(t *testing.T) {
	_, err := core.UnmarshalCommand([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestCreateSpecCommand_RoundTrip(t *testing.T) {
	cmd := core.CreateSpecCommand{
		Title:    "My Spec",
		OneLiner: "A short summary",
		Goal:     "Build something great",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "CreateSpec")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.CreateSpecCommand)
	if !ok {
		t.Fatalf("expected CreateSpecCommand, got %T", got)
	}
	if result.Title != cmd.Title {
		t.Errorf("Title: got %q, want %q", result.Title, cmd.Title)
	}
	if result.OneLiner != cmd.OneLiner {
		t.Errorf("OneLiner: got %q, want %q", result.OneLiner, cmd.OneLiner)
	}
	if result.Goal != cmd.Goal {
		t.Errorf("Goal: got %q, want %q", result.Goal, cmd.Goal)
	}
}

func TestUpdateSpecCoreCommand_RoundTrip(t *testing.T) {
	title := "Updated Title"
	desc := "New description"
	cmd := core.UpdateSpecCoreCommand{
		Title:       &title,
		Description: &desc,
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "UpdateSpecCore")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.UpdateSpecCoreCommand)
	if !ok {
		t.Fatalf("expected UpdateSpecCoreCommand, got %T", got)
	}
	if result.Title == nil || *result.Title != title {
		t.Errorf("Title: got %v, want %q", result.Title, title)
	}
	if result.Description == nil || *result.Description != desc {
		t.Errorf("Description: got %v, want %q", result.Description, desc)
	}
	if result.OneLiner != nil {
		t.Errorf("OneLiner: expected nil, got %q", *result.OneLiner)
	}
}

func TestUpdateSpecCoreCommand_UnmarshalArrayFields(t *testing.T) {
	data := []byte(`{
		"type":"UpdateSpecCore",
		"constraints":["Must run offline","No external SaaS"],
		"success_criteria":["Works end-to-end","Latency under 200ms"]
	}`)

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cmd, ok := got.(core.UpdateSpecCoreCommand)
	if !ok {
		t.Fatalf("expected UpdateSpecCoreCommand, got %T", got)
	}
	if cmd.Constraints == nil || *cmd.Constraints != "Must run offline\nNo external SaaS" {
		t.Fatalf("constraints: got %v", cmd.Constraints)
	}
	if cmd.SuccessCriteria == nil || *cmd.SuccessCriteria != "Works end-to-end\nLatency under 200ms" {
		t.Fatalf("success_criteria: got %v", cmd.SuccessCriteria)
	}
}

func TestUpdateSpecCoreCommand_UnmarshalRejectsInvalidFieldType(t *testing.T) {
	data := []byte(`{
		"type":"UpdateSpecCore",
		"constraints":{"k":"v"}
	}`)

	_, err := core.UnmarshalCommand(data)
	if err == nil {
		t.Fatal("expected error for invalid constraints type")
	}
}

func TestCreateCardCommand_RoundTrip(t *testing.T) {
	body := "Card body content"
	lane := "Doing"
	cmd := core.CreateCardCommand{
		CardType:  "task",
		Title:     "Implement feature",
		Body:      &body,
		Lane:      &lane,
		CreatedBy: "agent-1",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "CreateCard")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.CreateCardCommand)
	if !ok {
		t.Fatalf("expected CreateCardCommand, got %T", got)
	}
	if result.CardType != cmd.CardType {
		t.Errorf("CardType: got %q, want %q", result.CardType, cmd.CardType)
	}
	if result.Title != cmd.Title {
		t.Errorf("Title: got %q, want %q", result.Title, cmd.Title)
	}
	if result.Body == nil || *result.Body != body {
		t.Errorf("Body: got %v, want %q", result.Body, body)
	}
	if result.Lane == nil || *result.Lane != lane {
		t.Errorf("Lane: got %v, want %q", result.Lane, lane)
	}
	if result.CreatedBy != cmd.CreatedBy {
		t.Errorf("CreatedBy: got %q, want %q", result.CreatedBy, cmd.CreatedBy)
	}
}

func TestUpdateCardCommand_BodyPresent_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	title := "New title"
	cmd := core.UpdateCardCommand{
		CardID:    cardID,
		Title:     &title,
		Body:      core.Present("updated body"),
		UpdatedBy: "agent-1",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "UpdateCard")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.UpdateCardCommand)
	if !ok {
		t.Fatalf("expected UpdateCardCommand, got %T", got)
	}
	if result.CardID != cardID {
		t.Errorf("CardID: got %s, want %s", result.CardID, cardID)
	}
	if result.Title == nil || *result.Title != title {
		t.Errorf("Title: got %v, want %q", result.Title, title)
	}
	if !result.Body.Set || !result.Body.Valid {
		t.Fatal("Body: expected Present, got something else")
	}
	if result.Body.Value != "updated body" {
		t.Errorf("Body.Value: got %q, want %q", result.Body.Value, "updated body")
	}
	if result.UpdatedBy != cmd.UpdatedBy {
		t.Errorf("UpdatedBy: got %q, want %q", result.UpdatedBy, cmd.UpdatedBy)
	}
}

func TestUpdateCardCommand_BodyNull_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	cmd := core.UpdateCardCommand{
		CardID:    cardID,
		Body:      core.Null[string](),
		UpdatedBy: "agent-2",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify the body field is present and null in the JSON
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

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.UpdateCardCommand)
	if !ok {
		t.Fatalf("expected UpdateCardCommand, got %T", got)
	}
	if !result.Body.Set {
		t.Fatal("Body.Set: expected true")
	}
	if result.Body.Valid {
		t.Fatal("Body.Valid: expected false for Null body")
	}
}

func TestUpdateCardCommand_BodyAbsent_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	cmd := core.UpdateCardCommand{
		CardID:    cardID,
		Body:      core.Absent[string](),
		UpdatedBy: "agent-3",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify the body field is absent from the JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, present := raw["body"]; present {
		t.Fatal("expected body field to be absent from JSON for Absent")
	}

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.UpdateCardCommand)
	if !ok {
		t.Fatalf("expected UpdateCardCommand, got %T", got)
	}
	if result.Body.Set {
		t.Fatal("Body.Set: expected false for Absent body")
	}
}

func TestMoveCardCommand_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	cmd := core.MoveCardCommand{
		CardID:    cardID,
		Lane:      "Done",
		Order:     2.5,
		UpdatedBy: "agent-1",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "MoveCard")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.MoveCardCommand)
	if !ok {
		t.Fatalf("expected MoveCardCommand, got %T", got)
	}
	if result.CardID != cardID {
		t.Errorf("CardID: got %s, want %s", result.CardID, cardID)
	}
	if result.Lane != cmd.Lane {
		t.Errorf("Lane: got %q, want %q", result.Lane, cmd.Lane)
	}
	if result.Order != cmd.Order {
		t.Errorf("Order: got %f, want %f", result.Order, cmd.Order)
	}
	if result.UpdatedBy != cmd.UpdatedBy {
		t.Errorf("UpdatedBy: got %q, want %q", result.UpdatedBy, cmd.UpdatedBy)
	}
}

func TestDeleteCardCommand_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	cmd := core.DeleteCardCommand{
		CardID:    cardID,
		UpdatedBy: "agent-1",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "DeleteCard")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.DeleteCardCommand)
	if !ok {
		t.Fatalf("expected DeleteCardCommand, got %T", got)
	}
	if result.CardID != cardID {
		t.Errorf("CardID: got %s, want %s", result.CardID, cardID)
	}
	if result.UpdatedBy != cmd.UpdatedBy {
		t.Errorf("UpdatedBy: got %q, want %q", result.UpdatedBy, cmd.UpdatedBy)
	}
}

func TestAppendTranscriptCommand_RoundTrip(t *testing.T) {
	cmd := core.AppendTranscriptCommand{
		Sender:  "human",
		Content: "Let's discuss the architecture.",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "AppendTranscript")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.AppendTranscriptCommand)
	if !ok {
		t.Fatalf("expected AppendTranscriptCommand, got %T", got)
	}
	if result.Sender != cmd.Sender {
		t.Errorf("Sender: got %q, want %q", result.Sender, cmd.Sender)
	}
	if result.Content != cmd.Content {
		t.Errorf("Content: got %q, want %q", result.Content, cmd.Content)
	}
}

func TestAskQuestionCommand_Boolean_RoundTrip(t *testing.T) {
	qid := core.NewULID()
	defaultVal := true
	cmd := core.AskQuestionCommand{
		Question: core.BooleanQuestion{
			QID:      qid,
			Question: "Proceed?",
			Default:  &defaultVal,
		},
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "AskQuestion")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.AskQuestionCommand)
	if !ok {
		t.Fatalf("expected AskQuestionCommand, got %T", got)
	}
	bq, ok := result.Question.(core.BooleanQuestion)
	if !ok {
		t.Fatalf("expected BooleanQuestion, got %T", result.Question)
	}
	if bq.QID != qid {
		t.Errorf("QID: got %s, want %s", bq.QID, qid)
	}
	if bq.Question != "Proceed?" {
		t.Errorf("Question: got %q, want %q", bq.Question, "Proceed?")
	}
	if bq.Default == nil || *bq.Default != true {
		t.Errorf("Default: got %v, want true", bq.Default)
	}
}

func TestAskQuestionCommand_MultipleChoice_RoundTrip(t *testing.T) {
	qid := core.NewULID()
	cmd := core.AskQuestionCommand{
		Question: core.MultipleChoiceQuestion{
			QID:        qid,
			Question:   "Pick a color",
			Choices:    []string{"red", "green", "blue"},
			AllowMulti: true,
		},
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.AskQuestionCommand)
	if !ok {
		t.Fatalf("expected AskQuestionCommand, got %T", got)
	}
	mc, ok := result.Question.(core.MultipleChoiceQuestion)
	if !ok {
		t.Fatalf("expected MultipleChoiceQuestion, got %T", result.Question)
	}
	if mc.QID != qid {
		t.Errorf("QID: got %s, want %s", mc.QID, qid)
	}
	if mc.Question != "Pick a color" {
		t.Errorf("Question: got %q, want %q", mc.Question, "Pick a color")
	}
	if len(mc.Choices) != 3 {
		t.Fatalf("Choices: got %d items, want 3", len(mc.Choices))
	}
	if mc.Choices[0] != "red" || mc.Choices[1] != "green" || mc.Choices[2] != "blue" {
		t.Errorf("Choices: got %v, want [red green blue]", mc.Choices)
	}
	if !mc.AllowMulti {
		t.Error("AllowMulti: got false, want true")
	}
}

func TestAskQuestionCommand_Freeform_RoundTrip(t *testing.T) {
	qid := core.NewULID()
	placeholder := "Type here..."
	hint := "Must be at least 10 chars"
	cmd := core.AskQuestionCommand{
		Question: core.FreeformQuestion{
			QID:            qid,
			Question:       "Describe your approach",
			Placeholder:    &placeholder,
			ValidationHint: &hint,
		},
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.AskQuestionCommand)
	if !ok {
		t.Fatalf("expected AskQuestionCommand, got %T", got)
	}
	fq, ok := result.Question.(core.FreeformQuestion)
	if !ok {
		t.Fatalf("expected FreeformQuestion, got %T", result.Question)
	}
	if fq.QID != qid {
		t.Errorf("QID: got %s, want %s", fq.QID, qid)
	}
	if fq.Question != "Describe your approach" {
		t.Errorf("Question: got %q, want %q", fq.Question, "Describe your approach")
	}
	if fq.Placeholder == nil || *fq.Placeholder != placeholder {
		t.Errorf("Placeholder: got %v, want %q", fq.Placeholder, placeholder)
	}
	if fq.ValidationHint == nil || *fq.ValidationHint != hint {
		t.Errorf("ValidationHint: got %v, want %q", fq.ValidationHint, hint)
	}
}

func TestAnswerQuestionCommand_RoundTrip(t *testing.T) {
	qid := core.NewULID()
	cmd := core.AnswerQuestionCommand{
		QuestionID: qid,
		Answer:     "yes",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "AnswerQuestion")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.AnswerQuestionCommand)
	if !ok {
		t.Fatalf("expected AnswerQuestionCommand, got %T", got)
	}
	if result.QuestionID != qid {
		t.Errorf("QuestionID: got %s, want %s", result.QuestionID, qid)
	}
	if result.Answer != cmd.Answer {
		t.Errorf("Answer: got %q, want %q", result.Answer, cmd.Answer)
	}
}

func TestStartAgentStepCommand_RoundTrip(t *testing.T) {
	cmd := core.StartAgentStepCommand{
		AgentID:     "agent-42",
		Description: "Analyzing code structure",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "StartAgentStep")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.StartAgentStepCommand)
	if !ok {
		t.Fatalf("expected StartAgentStepCommand, got %T", got)
	}
	if result.AgentID != cmd.AgentID {
		t.Errorf("AgentID: got %q, want %q", result.AgentID, cmd.AgentID)
	}
	if result.Description != cmd.Description {
		t.Errorf("Description: got %q, want %q", result.Description, cmd.Description)
	}
}

func TestFinishAgentStepCommand_RoundTrip(t *testing.T) {
	cmd := core.FinishAgentStepCommand{
		AgentID:     "agent-42",
		DiffSummary: "+50 -10 in 3 files",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "FinishAgentStep")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.FinishAgentStepCommand)
	if !ok {
		t.Fatalf("expected FinishAgentStepCommand, got %T", got)
	}
	if result.AgentID != cmd.AgentID {
		t.Errorf("AgentID: got %q, want %q", result.AgentID, cmd.AgentID)
	}
	if result.DiffSummary != cmd.DiffSummary {
		t.Errorf("DiffSummary: got %q, want %q", result.DiffSummary, cmd.DiffSummary)
	}
}

func TestUndoCommand_RoundTrip(t *testing.T) {
	cmd := core.UndoCommand{}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	assertTypeField(t, data, "Undo")

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	_, ok := got.(core.UndoCommand)
	if !ok {
		t.Fatalf("expected UndoCommand, got %T", got)
	}
}

func TestUpdateCardCommand_WithRefs_RoundTrip(t *testing.T) {
	cardID := core.NewULID()
	refs := []string{"ref-a", "ref-b"}
	cardType := "requirement"
	cmd := core.UpdateCardCommand{
		CardID:    cardID,
		Body:      core.Present("body with refs"),
		CardType:  &cardType,
		Refs:      &refs,
		UpdatedBy: "agent-5",
	}

	data, err := core.MarshalCommand(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got, err := core.UnmarshalCommand(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result, ok := got.(core.UpdateCardCommand)
	if !ok {
		t.Fatalf("expected UpdateCardCommand, got %T", got)
	}
	if result.CardType == nil || *result.CardType != cardType {
		t.Errorf("CardType: got %v, want %q", result.CardType, cardType)
	}
	if result.Refs == nil || len(*result.Refs) != 2 {
		t.Fatalf("Refs: got %v, want 2 items", result.Refs)
	}
	if (*result.Refs)[0] != "ref-a" || (*result.Refs)[1] != "ref-b" {
		t.Errorf("Refs: got %v, want [ref-a ref-b]", *result.Refs)
	}
}

// assertTypeField checks the "type" discriminator is set correctly in the JSON.
func assertTypeField(t *testing.T, data []byte, expected string) {
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

// Ensure ulid import is used (compiler will catch if not, but this makes intent clear).
var _ ulid.ULID
