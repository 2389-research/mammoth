// ABOUTME: Tests for ExitHandler verify_command support.
// ABOUTME: Covers pre-exit verification with pass, fail, and absent verify_command scenarios.
package attractor

import (
	"context"
	"testing"
)

func TestExitHandlerVerifyCommandFailure(t *testing.T) {
	h := &ExitHandler{}
	node := &Node{
		ID: "exit_verify",
		Attrs: map[string]string{
			"shape":          "Msquare",
			"verify_command": "exit 1",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail when verify_command fails, got %v", outcome.Status)
	}
}

func TestExitHandlerVerifyCommandSuccess(t *testing.T) {
	h := &ExitHandler{}
	node := &Node{
		ID: "exit_verify_pass",
		Attrs: map[string]string{
			"shape":          "Msquare",
			"verify_command": "exit 0",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v", outcome.Status)
	}
}

func TestExitHandlerNoVerifyCommand(t *testing.T) {
	h := &ExitHandler{}
	node := &Node{
		ID:    "exit_no_verify",
		Attrs: map[string]string{"shape": "Msquare"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess without verify_command, got %v", outcome.Status)
	}
}
