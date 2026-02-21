// ABOUTME: Tests for VerifyHandler that executes deterministic shell commands without an LLM.
// ABOUTME: Covers exit code routing, timeout, working directory, and outcome context updates.
package attractor

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVerifyHandlerSuccess(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_tests",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "echo all tests pass",
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
	if outcome.ContextUpdates["outcome"] != "success" {
		t.Errorf("expected outcome=success in context, got %v", outcome.ContextUpdates["outcome"])
	}
}

func TestVerifyHandlerFailure(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_tests_fail",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "exit 1",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail, got %v", outcome.Status)
	}
	if outcome.ContextUpdates["outcome"] != "fail" {
		t.Errorf("expected outcome=fail in context, got %v", outcome.ContextUpdates["outcome"])
	}
}

func TestVerifyHandlerNoCommand(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID:    "verify_no_cmd",
		Attrs: map[string]string{"shape": "octagon"},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail when no command, got %v", outcome.Status)
	}
}

func TestVerifyHandlerType(t *testing.T) {
	h := &VerifyHandler{}
	if h.Type() != "verify" {
		t.Errorf("expected type 'verify', got %q", h.Type())
	}
}

func TestVerifyHandlerShapeMapping(t *testing.T) {
	handlerType := ShapeToHandlerType("octagon")
	if handlerType != "verify" {
		t.Errorf("expected octagon to map to 'verify', got %q", handlerType)
	}
}

func TestVerifyHandlerInDefaultRegistry(t *testing.T) {
	reg := DefaultHandlerRegistry()
	h := reg.Get("verify")
	if h == nil {
		t.Fatal("expected verify handler in default registry")
	}
}

func TestVerifyHandlerRespectsContextCancellation(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_cancel",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "echo hello",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := h.Execute(ctx, node, pctx, store)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestVerifyHandlerNilAttrs(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID:    "verify_nil_attrs",
		Attrs: nil,
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail for nil attrs, got %v", outcome.Status)
	}
}

func TestVerifyHandlerTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group killing not supported on windows")
	}
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_slow",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "sleep 60",
			"timeout": "500ms",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	start := time.Now()
	outcome, err := h.Execute(context.Background(), node, pctx, store)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail for timeout, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "timed out") {
		t.Errorf("expected timeout-related failure reason, got %q", outcome.FailureReason)
	}
	// Should complete well within 10 seconds, not wait 60
	if elapsed > 10*time.Second {
		t.Errorf("expected timeout to kill early, but took %v", elapsed)
	}
}

func TestVerifyHandlerWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_wd",
		Attrs: map[string]string{
			"shape":       "octagon",
			"command":     "pwd",
			"working_dir": tmpDir,
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected StatusSuccess, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	// Resolve symlinks for macOS /var -> /private/var
	resolvedTmpDir, _ := filepath.EvalSymlinks(tmpDir)
	resolvedStdout, _ := filepath.EvalSymlinks(strings.TrimSpace(outcome.Notes))
	if resolvedStdout != resolvedTmpDir {
		t.Errorf("expected working dir %q in notes, got %q", resolvedTmpDir, resolvedStdout)
	}
}

func TestVerifyHandlerStoresArtifact(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_artifact",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "echo artifact output",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	_, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	artifactID := "verify_artifact.output"
	if !store.Has(artifactID) {
		t.Errorf("expected artifact %q to be stored", artifactID)
	}
}

func TestVerifyHandlerSetsLastStage(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_stage",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "echo ok",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.ContextUpdates["last_stage"] != "verify_stage" {
		t.Errorf("expected last_stage=verify_stage, got %v", outcome.ContextUpdates["last_stage"])
	}
}

func TestVerifyHandlerFailureReason(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_fail_reason",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "sh -c 'echo oops >&2; exit 42'",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail, got %v", outcome.Status)
	}
	if outcome.FailureReason == "" {
		t.Error("expected a failure reason for non-zero exit code")
	}
	if !strings.Contains(outcome.FailureReason, "exit") {
		t.Errorf("expected exit code info in failure reason, got %q", outcome.FailureReason)
	}
}

func TestVerifyHandlerEmptyCommand(t *testing.T) {
	h := &VerifyHandler{}
	node := &Node{
		ID: "verify_empty_cmd",
		Attrs: map[string]string{
			"shape":   "octagon",
			"command": "",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected StatusFail for empty command, got %v", outcome.Status)
	}
}

func TestVerifyHandlerResolvesViaRegistry(t *testing.T) {
	reg := DefaultHandlerRegistry()
	node := &Node{
		ID: "verify_resolve",
		Attrs: map[string]string{
			"shape": "octagon",
		},
	}
	h := reg.Resolve(node)
	if h == nil {
		t.Fatal("expected handler to be resolved for octagon shape")
	}
	if h.Type() != "verify" {
		t.Errorf("expected resolved handler type 'verify', got %q", h.Type())
	}
}
