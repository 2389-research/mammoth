// ABOUTME: Tests for model resolution logic used by the node edit form.
// ABOUTME: Verifies stylesheet parsing, direct attributes, and specificity ordering.

package editor

import (
	"testing"

	"github.com/2389-research/mammoth/dot"
)

func TestResolveNodeModel_DirectAttribute(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"llm_model": "gpt-5.2"},
	}
	model, source := resolveNodeModel(node, "")
	if model != "gpt-5.2" {
		t.Fatalf("expected model gpt-5.2, got %s", model)
	}
	if source != "node attribute" {
		t.Fatalf("expected source 'node attribute', got %s", source)
	}
}

func TestResolveNodeModel_UniversalStylesheet(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"label": "Do stuff"},
	}
	stylesheet := `* { llm_model: claude-sonnet-4-5; }`
	model, source := resolveNodeModel(node, stylesheet)
	if model != "claude-sonnet-4-5" {
		t.Fatalf("expected model claude-sonnet-4-5, got %s", model)
	}
	if source != "stylesheet (* rule)" {
		t.Fatalf("expected source 'stylesheet (* rule)', got %s", source)
	}
}

func TestResolveNodeModel_IDSelectorOverridesUniversal(t *testing.T) {
	node := &dot.Node{
		ID:    "critical_review",
		Attrs: map[string]string{"label": "Review"},
	}
	stylesheet := `* { llm_model: claude-sonnet-4-5; } #critical_review { llm_model: claude-opus-4-6; }`
	model, source := resolveNodeModel(node, stylesheet)
	if model != "claude-opus-4-6" {
		t.Fatalf("expected model claude-opus-4-6, got %s", model)
	}
	if source != "stylesheet (#critical_review rule)" {
		t.Fatalf("expected source 'stylesheet (#critical_review rule)', got %s", source)
	}
}

func TestResolveNodeModel_ClassSelector(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"class": "code"},
	}
	stylesheet := `* { llm_model: claude-sonnet-4-5; } .code { llm_model: claude-opus-4-6; }`
	model, source := resolveNodeModel(node, stylesheet)
	if model != "claude-opus-4-6" {
		t.Fatalf("expected model claude-opus-4-6, got %s", model)
	}
	if source != "stylesheet (.code rule)" {
		t.Fatalf("expected source 'stylesheet (.code rule)', got %s", source)
	}
}

func TestResolveNodeModel_NoStylesheet(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"label": "Do stuff"},
	}
	model, source := resolveNodeModel(node, "")
	if model != "" {
		t.Fatalf("expected empty model, got %s", model)
	}
	if source != "" {
		t.Fatalf("expected empty source, got %s", source)
	}
}

func TestResolveNodeModel_DirectOverridesStylesheet(t *testing.T) {
	node := &dot.Node{
		ID:    "task_0",
		Attrs: map[string]string{"llm_model": "gpt-5.2"},
	}
	stylesheet := `* { llm_model: claude-sonnet-4-5; }`
	model, source := resolveNodeModel(node, stylesheet)
	if model != "gpt-5.2" {
		t.Fatalf("expected model gpt-5.2, got %s", model)
	}
	if source != "node attribute" {
		t.Fatalf("expected source 'node attribute', got %s", source)
	}
}
