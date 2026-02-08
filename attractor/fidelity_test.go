// ABOUTME: Tests for fidelity mode validation and resolution precedence.
// ABOUTME: Verifies edge > node > graph > default fallback ordering.
package attractor

import (
	"testing"
)

func TestIsValidFidelity(t *testing.T) {
	validModes := []string{
		"full", "truncate", "compact",
		"summary:low", "summary:medium", "summary:high",
	}
	for _, mode := range validModes {
		if !IsValidFidelity(mode) {
			t.Errorf("expected %q to be valid", mode)
		}
	}

	invalidModes := []string{"", "invalid", "summary", "FULL", "summary:"}
	for _, mode := range invalidModes {
		if IsValidFidelity(mode) {
			t.Errorf("expected %q to be invalid", mode)
		}
	}
}

func TestResolveFidelity_EdgePrecedence(t *testing.T) {
	edge := &Edge{
		From:  "a",
		To:    "b",
		Attrs: map[string]string{"fidelity": "full"},
	}
	targetNode := &Node{
		ID:    "b",
		Attrs: map[string]string{"fidelity": "truncate"},
	}
	graph := &Graph{
		Attrs: map[string]string{"default_fidelity": "compact"},
	}

	got := ResolveFidelity(edge, targetNode, graph)
	if got != FidelityFull {
		t.Errorf("expected edge fidelity 'full', got %q", got)
	}
}

func TestResolveFidelity_NodePrecedence(t *testing.T) {
	edge := &Edge{
		From:  "a",
		To:    "b",
		Attrs: map[string]string{},
	}
	targetNode := &Node{
		ID:    "b",
		Attrs: map[string]string{"fidelity": "truncate"},
	}
	graph := &Graph{
		Attrs: map[string]string{"default_fidelity": "compact"},
	}

	got := ResolveFidelity(edge, targetNode, graph)
	if got != FidelityTruncate {
		t.Errorf("expected node fidelity 'truncate', got %q", got)
	}
}

func TestResolveFidelity_GraphDefault(t *testing.T) {
	edge := &Edge{
		From:  "a",
		To:    "b",
		Attrs: map[string]string{},
	}
	targetNode := &Node{
		ID:    "b",
		Attrs: map[string]string{},
	}
	graph := &Graph{
		Attrs: map[string]string{"default_fidelity": "summary:high"},
	}

	got := ResolveFidelity(edge, targetNode, graph)
	if got != FidelitySummaryHigh {
		t.Errorf("expected graph default 'summary:high', got %q", got)
	}
}

func TestResolveFidelity_DefaultCompact(t *testing.T) {
	edge := &Edge{
		From:  "a",
		To:    "b",
		Attrs: map[string]string{},
	}
	targetNode := &Node{
		ID:    "b",
		Attrs: map[string]string{},
	}
	graph := &Graph{
		Attrs: map[string]string{},
	}

	got := ResolveFidelity(edge, targetNode, graph)
	if got != FidelityCompact {
		t.Errorf("expected default 'compact', got %q", got)
	}
}
