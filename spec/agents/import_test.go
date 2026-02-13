// ABOUTME: Tests for LLM-based spec import: JSON extraction, command generation, and system prompts.
// ABOUTME: Validates the 3-tier JSON extraction, to_commands output, and prompt content.
package agents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/2389-research/mammoth/spec/core"
)

func sampleImportResult() *ImportResult {
	body := "Users can add tasks with a title"
	lane1 := "Ideas"
	lane2 := "Backlog"
	desc := "A todo app with persistent storage"
	constraints := "Must work offline"
	return &ImportResult{
		Spec: ImportSpec{
			Title:    "Todo App",
			OneLiner: "A simple task manager",
			Goal:     "Build a CLI todo application",
		},
		Update: &ImportUpdate{
			Description: &desc,
			Constraints: &constraints,
		},
		Cards: []ImportCard{
			{
				CardType: "idea",
				Title:    "Add tasks",
				Body:     &body,
				Lane:     &lane1,
			},
			{
				CardType: "task",
				Title:    "Set up CLI framework",
				Lane:     &lane2,
			},
		},
	}
}

func TestToCommandsCreatesSpecCommand(t *testing.T) {
	result := sampleImportResult()
	commands := ToCommands(result)

	if len(commands) < 1 {
		t.Fatal("expected at least 1 command")
	}

	spec, ok := commands[0].(core.CreateSpecCommand)
	if !ok {
		t.Fatalf("expected CreateSpecCommand, got %T", commands[0])
	}
	if spec.Title != "Todo App" {
		t.Errorf("expected title 'Todo App', got '%s'", spec.Title)
	}
	if spec.OneLiner != "A simple task manager" {
		t.Errorf("expected one_liner 'A simple task manager', got '%s'", spec.OneLiner)
	}
	if spec.Goal != "Build a CLI todo application" {
		t.Errorf("expected goal 'Build a CLI todo application', got '%s'", spec.Goal)
	}
}

func TestToCommandsCreatesUpdateSpecCore(t *testing.T) {
	result := sampleImportResult()
	commands := ToCommands(result)

	if len(commands) < 2 {
		t.Fatal("expected at least 2 commands")
	}

	update, ok := commands[1].(core.UpdateSpecCoreCommand)
	if !ok {
		t.Fatalf("expected UpdateSpecCoreCommand, got %T", commands[1])
	}
	if update.Description == nil || *update.Description != "A todo app with persistent storage" {
		t.Error("expected description")
	}
	if update.Constraints == nil || *update.Constraints != "Must work offline" {
		t.Error("expected constraints")
	}
	if update.SuccessCriteria != nil {
		t.Error("expected nil success_criteria")
	}
}

func TestToCommandsCreatesCardCommands(t *testing.T) {
	result := sampleImportResult()
	commands := ToCommands(result)

	// CreateSpec + UpdateSpecCore + 2 cards = 4 commands
	if len(commands) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(commands))
	}

	card1, ok := commands[2].(core.CreateCardCommand)
	if !ok {
		t.Fatalf("expected CreateCardCommand, got %T", commands[2])
	}
	if card1.CardType != "idea" {
		t.Errorf("expected card_type 'idea', got '%s'", card1.CardType)
	}
	if card1.Title != "Add tasks" {
		t.Errorf("expected title 'Add tasks', got '%s'", card1.Title)
	}
	if card1.Body == nil || *card1.Body != "Users can add tasks with a title" {
		t.Error("expected body")
	}
	if card1.CreatedBy != "import" {
		t.Errorf("expected created_by 'import', got '%s'", card1.CreatedBy)
	}

	card2, ok := commands[3].(core.CreateCardCommand)
	if !ok {
		t.Fatalf("expected CreateCardCommand, got %T", commands[3])
	}
	if card2.CardType != "task" {
		t.Errorf("expected card_type 'task', got '%s'", card2.CardType)
	}
	if card2.Body != nil {
		t.Error("expected nil body")
	}
}

func TestToCommandsHandlesEmptyCards(t *testing.T) {
	result := &ImportResult{
		Spec:  ImportSpec{Title: "Empty", OneLiner: "Nothing here", Goal: "Test empty"},
		Cards: []ImportCard{},
	}
	commands := ToCommands(result)

	// Just CreateSpec, no UpdateSpecCore (update is nil), no cards
	if len(commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(commands))
	}
	if _, ok := commands[0].(core.CreateSpecCommand); !ok {
		t.Errorf("expected CreateSpecCommand, got %T", commands[0])
	}
}

func TestToCommandsSkipsUpdateWhenAllFieldsNone(t *testing.T) {
	result := &ImportResult{
		Spec:   ImportSpec{Title: "Minimal", OneLiner: "Bare bones", Goal: "Test minimal"},
		Update: &ImportUpdate{},
		Cards:  []ImportCard{},
	}
	commands := ToCommands(result)

	// Should skip UpdateSpecCore since all fields are nil
	if len(commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(commands))
	}
}

func TestExtractJSONParsesRawJSON(t *testing.T) {
	data, _ := json.Marshal(sampleImportResult())
	result, err := ExtractJSON(string(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Spec.Title != "Todo App" {
		t.Errorf("expected title 'Todo App', got '%s'", result.Spec.Title)
	}
	if len(result.Cards) != 2 {
		t.Errorf("expected 2 cards, got %d", len(result.Cards))
	}
}

func TestExtractJSONStripsCodeFences(t *testing.T) {
	data, _ := json.Marshal(sampleImportResult())
	fenced := "```json\n" + string(data) + "\n```"
	result, err := ExtractJSON(fenced)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Spec.Title != "Todo App" {
		t.Errorf("expected title 'Todo App', got '%s'", result.Spec.Title)
	}
}

func TestExtractJSONFindsBraceSubstring(t *testing.T) {
	data, _ := json.Marshal(sampleImportResult())
	wrapped := "Here is the extracted spec:\n" + string(data) + "\nHope that helps!"
	result, err := ExtractJSON(wrapped)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Spec.Title != "Todo App" {
		t.Errorf("expected title 'Todo App', got '%s'", result.Spec.Title)
	}
}

func TestExtractJSONRejectsGarbage(t *testing.T) {
	_, err := ExtractJSON("this is not json at all")
	if err == nil {
		t.Fatal("expected error for garbage input")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("expected 'failed to parse' in error, got: %s", err)
	}
}

func TestImportResultSerdeRoundTrip(t *testing.T) {
	original := sampleImportResult()
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var deserialized ImportResult
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if deserialized.Spec.Title != original.Spec.Title {
		t.Errorf("title mismatch")
	}
	if deserialized.Spec.OneLiner != original.Spec.OneLiner {
		t.Errorf("one_liner mismatch")
	}
	if len(deserialized.Cards) != len(original.Cards) {
		t.Errorf("cards len mismatch")
	}
	if deserialized.Cards[0].CardType != "idea" {
		t.Errorf("expected first card type 'idea', got '%s'", deserialized.Cards[0].CardType)
	}
	if deserialized.Cards[1].CardType != "task" {
		t.Errorf("expected second card type 'task', got '%s'", deserialized.Cards[1].CardType)
	}
}

func TestSystemPromptIncludesCardTypes(t *testing.T) {
	prompt := BuildImportSystemPrompt("")
	for _, cardType := range []string{"idea", "task", "plan", "decision", "constraint", "risk"} {
		if !strings.Contains(prompt, cardType) {
			t.Errorf("expected prompt to contain '%s'", cardType)
		}
	}
}

func TestSystemPromptIncludesSourceHint(t *testing.T) {
	prompt := BuildImportSystemPrompt("DOT graph")
	if !strings.Contains(prompt, "DOT graph") {
		t.Error("expected prompt to contain 'DOT graph'")
	}
}

func TestSystemPromptWithoutHintHasNoFormatNote(t *testing.T) {
	prompt := BuildImportSystemPrompt("")
	if strings.Contains(prompt, "The input is in") {
		t.Error("expected no 'The input is in' when no hint provided")
	}
}
