// ABOUTME: Tests for ExitHandler verify_command support.
// ABOUTME: Covers pre-exit verification with pass, fail, absent verify_command, and working_dir scenarios.
package attractor

import (
	"context"
	"path/filepath"
	"strings"
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
	if outcome.ContextUpdates == nil || outcome.ContextUpdates["last_stage"] != "exit_verify" {
		t.Errorf("expected last_stage='exit_verify' in context updates, got %v", outcome.ContextUpdates)
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

func TestExitHandlerSuccessIncludesLastStage(t *testing.T) {
	h := &ExitHandler{}
	node := &Node{
		ID:    "exit_final",
		Attrs: map[string]string{"shape": "Msquare"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.ContextUpdates == nil || outcome.ContextUpdates["last_stage"] != "exit_final" {
		t.Errorf("expected last_stage='exit_final' in success context updates, got %v", outcome.ContextUpdates)
	}
}

func TestExitHandlerVerifyCommandWorkingDir(t *testing.T) {
	dir := t.TempDir()
	h := &ExitHandler{}
	node := &Node{
		ID: "exit_workdir",
		Attrs: map[string]string{
			"shape":          "Msquare",
			"verify_command": "pwd",
			"working_dir":    dir,
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
	// Check the stored artifact contains the working directory
	if !store.Has("exit_workdir.verify_output") {
		t.Fatal("expected verify_output artifact to be stored")
	}
	artData, err := store.Retrieve("exit_workdir.verify_output")
	if err != nil {
		t.Fatalf("failed to retrieve artifact: %v", err)
	}
	// Compare both paths after resolving symlinks (macOS /var -> /private/var)
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	output := string(artData)
	// Extract the stdout line from the artifact (format: "exit_code=0\nstdout:\n<path>\n...")
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
