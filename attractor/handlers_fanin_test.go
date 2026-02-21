// ABOUTME: Tests for FanInHandler verify_command support.
// ABOUTME: Covers post-merge verification with pass, fail, and absent verify_command scenarios.
package attractor

import (
	"context"
	"testing"
)

func TestFanInHandlerVerifyCommandFailure(t *testing.T) {
	h := &FanInHandler{}
	node := &Node{
		ID: "fan_in_verify",
		Attrs: map[string]string{
			"shape":          "tripleoctagon",
			"verify_command": "exit 1",
		},
	}
	pctx := NewContext()
	pctx.Set("parallel.results", []any{"branch1", "branch2"})
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail when verify_command fails, got %v", outcome.Status)
	}
}

func TestFanInHandlerVerifyCommandSuccess(t *testing.T) {
	h := &FanInHandler{}
	node := &Node{
		ID: "fan_in_verify_pass",
		Attrs: map[string]string{
			"shape":          "tripleoctagon",
			"verify_command": "exit 0",
		},
	}
	pctx := NewContext()
	pctx.Set("parallel.results", []any{"branch1"})
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", outcome.Status)
	}
}

func TestFanInHandlerNoVerifyCommand(t *testing.T) {
	h := &FanInHandler{}
	node := &Node{
		ID:    "fan_in_no_verify",
		Attrs: map[string]string{"shape": "tripleoctagon"},
	}
	pctx := NewContext()
	pctx.Set("parallel.results", []any{"branch1"})
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess without verify_command, got %v", outcome.Status)
	}
}
