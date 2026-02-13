// ABOUTME: Tests for the YAML exporter covering round-trip, determinism, card inclusion, and error cases.
// ABOUTME: Uses external test package (export_test) to test the public API surface.
package export_test

import (
	"strings"
	"testing"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/core/export"
	"gopkg.in/yaml.v3"
)

func TestExportYAMLRoundTrip(t *testing.T) {
	state := makeStateWithCore()
	state.Core.Goal = "Verify the YAML exporter"

	card := makeCard("idea", "Test Card", "Ideas", 1.0, "human")
	state.Cards.Set(card.CardID, card)

	yamlStr, err := export.ExportYAML(state)
	if err != nil {
		t.Fatalf("export should succeed: %v", err)
	}

	// Parse back as generic YAML value to verify structure
	var value map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &value); err != nil {
		t.Fatalf("should parse as valid YAML: %v", err)
	}

	// Verify required fields exist and match
	if value["name"] != "Test Spec" {
		t.Errorf("expected name=Test Spec, got %v", value["name"])
	}
	if value["version"] != "0.1" {
		t.Errorf("expected version=0.1, got %v", value["version"])
	}
	if value["one_liner"] != "A test specification" {
		t.Errorf("expected one_liner=A test specification, got %v", value["one_liner"])
	}
	if value["goal"] != "Verify the YAML exporter" {
		t.Errorf("expected goal=Verify the YAML exporter, got %v", value["goal"])
	}

	// Verify lanes structure
	lanes, ok := value["lanes"].([]interface{})
	if !ok || len(lanes) == 0 {
		t.Fatal("expected non-empty lanes array")
	}

	// Verify the Ideas lane has the card
	ideasLane, ok := lanes[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected Ideas lane to be a mapping")
	}
	if ideasLane["name"] != "Ideas" {
		t.Errorf("expected first lane=Ideas, got %v", ideasLane["name"])
	}

	cards, ok := ideasLane["cards"].([]interface{})
	if !ok || len(cards) != 1 {
		t.Fatalf("expected 1 card in Ideas lane, got %v", ideasLane["cards"])
	}

	cardMap, ok := cards[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected card to be a mapping")
	}
	if cardMap["title"] != "Test Card" {
		t.Errorf("expected card title=Test Card, got %v", cardMap["title"])
	}
}

func TestExportYAMLDeterministic(t *testing.T) {
	state := makeStateWithCore()

	cardA := makeCard("idea", "Alpha", "Ideas", 1.0, "human")
	cardB := makeCard("task", "Beta", "Plan", 2.0, "agent")
	state.Cards.Set(cardA.CardID, cardA)
	state.Cards.Set(cardB.CardID, cardB)

	yaml1, err := export.ExportYAML(state)
	if err != nil {
		t.Fatalf("export 1 failed: %v", err)
	}
	yaml2, err := export.ExportYAML(state)
	if err != nil {
		t.Fatalf("export 2 failed: %v", err)
	}

	if yaml1 != yaml2 {
		t.Error("YAML export must be deterministic")
	}
}

func TestExportYAMLIncludesAllCards(t *testing.T) {
	state := makeStateWithCore()

	cardA := makeCard("idea", "Card A", "Ideas", 1.0, "human")
	cardB := makeCard("plan", "Card B", "Plan", 1.0, "human")
	cardC := makeCard("task", "Card C", "Spec", 1.0, "human")

	state.Cards.Set(cardA.CardID, cardA)
	state.Cards.Set(cardB.CardID, cardB)
	state.Cards.Set(cardC.CardID, cardC)

	yamlStr, err := export.ExportYAML(state)
	if err != nil {
		t.Fatalf("export should succeed: %v", err)
	}

	// All three cards should appear in the YAML
	if !strings.Contains(yamlStr, "Card A") {
		t.Error("missing Card A")
	}
	if !strings.Contains(yamlStr, "Card B") {
		t.Error("missing Card B")
	}
	if !strings.Contains(yamlStr, "Card C") {
		t.Error("missing Card C")
	}

	// Parse and count cards across all lanes
	var value map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &value); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	lanes := value["lanes"].([]interface{})
	totalCards := 0
	for _, lane := range lanes {
		laneMap := lane.(map[string]interface{})
		if cards, ok := laneMap["cards"].([]interface{}); ok {
			totalCards += len(cards)
		}
	}
	if totalCards != 3 {
		t.Errorf("expected 3 total cards, got %d", totalCards)
	}
}

func TestExportYAMLOmitsOptionalFieldsWhenNone(t *testing.T) {
	state := makeStateWithCore()
	yamlStr, err := export.ExportYAML(state)
	if err != nil {
		t.Fatalf("export should succeed: %v", err)
	}

	// Optional fields that are nil should not appear
	if strings.Contains(yamlStr, "description:") {
		t.Error("description should not appear when nil")
	}
	if strings.Contains(yamlStr, "constraints:") {
		t.Error("constraints should not appear when nil")
	}
	if strings.Contains(yamlStr, "success_criteria:") {
		t.Error("success_criteria should not appear when nil")
	}
	if strings.Contains(yamlStr, "risks:") {
		t.Error("risks should not appear when nil")
	}
	if strings.Contains(yamlStr, "notes:") {
		t.Error("notes should not appear when nil")
	}
}

func TestExportYAMLReturnsErrWhenCoreIsNone(t *testing.T) {
	state := core.NewSpecState()
	if state.Core != nil {
		t.Fatal("expected nil core")
	}
	_, err := export.ExportYAML(state)
	if err == nil {
		t.Error("export_yaml should return error when core is nil")
	}
}

func TestExportYAMLIncludesOptionalFieldsWhenPresent(t *testing.T) {
	state := makeStateWithCore()
	desc := "A description"
	constr := "Must be fast"
	state.Core.Description = &desc
	state.Core.Constraints = &constr

	yamlStr, err := export.ExportYAML(state)
	if err != nil {
		t.Fatalf("export should succeed: %v", err)
	}

	if !strings.Contains(yamlStr, "description:") {
		t.Error("missing description field")
	}
	if !strings.Contains(yamlStr, "A description") {
		t.Error("missing description value")
	}
	if !strings.Contains(yamlStr, "constraints:") {
		t.Error("missing constraints field")
	}
	if !strings.Contains(yamlStr, "Must be fast") {
		t.Error("missing constraints value")
	}
}
