// ABOUTME: Tests for FanInHandler verify_command support.
// ABOUTME: Covers post-merge verification with pass, fail, absent verify_command, and working_dir scenarios.
package attractor

import (
	"context"
	"path/filepath"
	"strings"
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

func TestFanInHandlerVerifyCommandLastStage(t *testing.T) {
	h := &FanInHandler{}
	node := &Node{
		ID: "fanin_stage",
		Attrs: map[string]string{
			"shape":          "tripleoctagon",
			"verify_command": "exit 1",
		},
	}
	pctx := NewContext()
	pctx.Set("parallel.results", []any{"branch1"})
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.ContextUpdates == nil || outcome.ContextUpdates["last_stage"] != "fanin_stage" {
		t.Errorf("expected last_stage='fanin_stage' in context updates, got %v", outcome.ContextUpdates)
	}
}

func TestFanInHandlerVerifyCommandWorkingDir(t *testing.T) {
	dir := t.TempDir()
	h := &FanInHandler{}
	node := &Node{
		ID: "fanin_workdir",
		Attrs: map[string]string{
			"shape":          "tripleoctagon",
			"verify_command": "pwd",
			"working_dir":    dir,
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
	if !store.Has("fanin_workdir.verify_output") {
		t.Fatal("expected verify_output artifact to be stored")
	}
	artData, err := store.Retrieve("fanin_workdir.verify_output")
	if err != nil {
		t.Fatalf("failed to retrieve artifact: %v", err)
	}
	// Compare both paths after resolving symlinks (macOS /var -> /private/var)
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	output := string(artData)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.Contains(trimmed, "=") || trimmed == "stdout:" || trimmed == "stderr:" {
			continue
		}
		resolvedOutput, _ := filepath.EvalSymlinks(trimmed)
		if resolvedOutput == resolvedDir {
			return // found it
		}
	}
	t.Errorf("expected verify output to contain working dir %q, got %q", resolvedDir, output)
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
