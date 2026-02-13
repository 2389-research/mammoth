// ABOUTME: Tests for the ToolHandler which executes shell commands via os/exec.
// ABOUTME: Covers command execution, timeout, env vars, working directory, output capture, and artifact storage.
package attractor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- ToolHandler command execution tests ---

func TestToolHandlerExecutesCommand(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "run_echo",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": "echo hello world",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	if !strings.Contains(outcome.Notes, "hello world") {
		t.Errorf("expected stdout in notes, got %q", outcome.Notes)
	}
	if outcome.ContextUpdates["tool.exit_code"] != 0 {
		t.Errorf("expected exit code 0, got %v", outcome.ContextUpdates["tool.exit_code"])
	}
	stdout, ok := outcome.ContextUpdates["tool.stdout"].(string)
	if !ok {
		t.Fatalf("expected tool.stdout to be a string, got %T", outcome.ContextUpdates["tool.stdout"])
	}
	if !strings.Contains(stdout, "hello world") {
		t.Errorf("expected 'hello world' in tool.stdout, got %q", stdout)
	}
}

func TestToolHandlerUsesPromptAsFallback(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "run_prompt",
		Attrs: map[string]string{
			"shape":  "parallelogram",
			"prompt": "echo from prompt",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	stdout, ok := outcome.ContextUpdates["tool.stdout"].(string)
	if !ok {
		t.Fatalf("expected tool.stdout to be a string, got %T", outcome.ContextUpdates["tool.stdout"])
	}
	if !strings.Contains(stdout, "from prompt") {
		t.Errorf("expected 'from prompt' in tool.stdout, got %q", stdout)
	}
}

func TestToolHandlerCommandPrecedesPrompt(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "run_both",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": "echo from command",
			"prompt":  "echo from prompt",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	stdout := outcome.ContextUpdates["tool.stdout"].(string)
	if !strings.Contains(stdout, "from command") {
		t.Errorf("expected 'from command' in tool.stdout, got %q", stdout)
	}
}

func TestToolHandlerFailsWithNoCommand(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "no_cmd",
		Attrs: map[string]string{
			"shape": "parallelogram",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail, got %v", outcome.Status)
	}
	if outcome.FailureReason == "" {
		t.Error("expected failure reason")
	}
}

func TestToolHandlerFailsWithNilAttrs(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID:    "nil_attrs",
		Attrs: nil,
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail, got %v", outcome.Status)
	}
}

// --- Non-zero exit code ---

func TestToolHandlerNonZeroExitCode(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "fail_cmd",
		Attrs: map[string]string{
			"shape":   "parallelogram",
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
		t.Errorf("expected status fail, got %v", outcome.Status)
	}
	exitCode, ok := outcome.ContextUpdates["tool.exit_code"].(int)
	if !ok {
		t.Fatalf("expected tool.exit_code to be an int, got %T", outcome.ContextUpdates["tool.exit_code"])
	}
	if exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", exitCode)
	}
	stderr, ok := outcome.ContextUpdates["tool.stderr"].(string)
	if !ok {
		t.Fatalf("expected tool.stderr to be a string, got %T", outcome.ContextUpdates["tool.stderr"])
	}
	if !strings.Contains(stderr, "oops") {
		t.Errorf("expected 'oops' in tool.stderr, got %q", stderr)
	}
}

// --- Timeout ---

func TestToolHandlerTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group killing not supported on windows")
	}
	h := &ToolHandler{}
	node := &Node{
		ID: "slow_cmd",
		Attrs: map[string]string{
			"shape":   "parallelogram",
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
		t.Errorf("expected status fail for timeout, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "timeout") && !strings.Contains(outcome.FailureReason, "killed") {
		t.Errorf("expected timeout-related failure reason, got %q", outcome.FailureReason)
	}
	// Should complete well within 10 seconds, not wait 60
	if elapsed > 10*time.Second {
		t.Errorf("expected timeout to kill early, but took %v", elapsed)
	}
}

func TestToolHandlerDefaultTimeout(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "quick_cmd",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": "echo fast",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected success with default timeout, got %v", outcome.Status)
	}
}

func TestToolHandlerInvalidTimeout(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "bad_timeout",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": "echo hello",
			"timeout": "not-a-duration",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail for invalid timeout, got %v", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "timeout") {
		t.Errorf("expected timeout-related failure reason, got %q", outcome.FailureReason)
	}
}

// --- Working directory ---

func TestToolHandlerWorkingDir(t *testing.T) {
	tmpDir := t.TempDir()
	h := &ToolHandler{}
	node := &Node{
		ID: "wd_cmd",
		Attrs: map[string]string{
			"shape":       "parallelogram",
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
		t.Errorf("expected status success, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	stdout := outcome.ContextUpdates["tool.stdout"].(string)
	// Resolve symlinks for macOS /var -> /private/var
	resolvedTmpDir, _ := filepath.EvalSymlinks(tmpDir)
	resolvedStdout, _ := filepath.EvalSymlinks(strings.TrimSpace(stdout))
	if resolvedStdout != resolvedTmpDir {
		t.Errorf("expected working dir %q, got %q", resolvedTmpDir, resolvedStdout)
	}
}

func TestToolHandlerInvalidWorkingDir(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "bad_wd",
		Attrs: map[string]string{
			"shape":       "parallelogram",
			"command":     "echo hello",
			"working_dir": "/nonexistent/path/that/does/not/exist",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusFail {
		t.Errorf("expected status fail for invalid working dir, got %v", outcome.Status)
	}
}

// --- Environment variables ---

func TestToolHandlerEnvVars(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "env_cmd",
		Attrs: map[string]string{
			"shape":      "parallelogram",
			"command":    "sh -c 'echo $MY_VAR'",
			"env_MY_VAR": "skull_value",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	stdout := outcome.ContextUpdates["tool.stdout"].(string)
	if !strings.Contains(stdout, "skull_value") {
		t.Errorf("expected 'skull_value' in stdout, got %q", stdout)
	}
}

func TestToolHandlerMultipleEnvVars(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "multi_env",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": "sh -c 'echo $FOO $BAR'",
			"env_FOO": "hello",
			"env_BAR": "world",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v", outcome.Status)
	}
	stdout := outcome.ContextUpdates["tool.stdout"].(string)
	if !strings.Contains(stdout, "hello") || !strings.Contains(stdout, "world") {
		t.Errorf("expected 'hello world' in stdout, got %q", stdout)
	}
}

// --- Context cancellation ---

func TestToolHandlerRespectsContextCancellation(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "cancel_cmd",
		Attrs: map[string]string{
			"shape":   "parallelogram",
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

// --- Output truncation and artifact storage ---

func TestToolHandlerTruncatesLargeOutput(t *testing.T) {
	h := &ToolHandler{}
	// Generate >10KB of output
	// Each line is ~80 chars, so 200 lines = ~16KB
	cmd := fmt.Sprintf("sh -c 'for i in $(seq 1 200); do printf \"%s\\n\"; done'",
		strings.Repeat("X", 80))
	node := &Node{
		ID: "big_output",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": cmd,
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected status success, got %v (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	// Notes should be truncated to <=10KB + some truncation message
	if len(outcome.Notes) > 11*1024 {
		t.Errorf("expected notes to be truncated, got %d bytes", len(outcome.Notes))
	}
	if !strings.Contains(outcome.Notes, "truncated") {
		t.Errorf("expected truncation notice in notes, got %q", outcome.Notes[:100])
	}
	// Full output should be stored as artifact
	artifactID := fmt.Sprintf("%s.stdout", node.ID)
	if !store.Has(artifactID) {
		t.Errorf("expected full output stored as artifact %q", artifactID)
	}
}

// --- last_stage context update ---

func TestToolHandlerSetsLastStage(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "stage_cmd",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": "echo ok",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.ContextUpdates["last_stage"] != "stage_cmd" {
		t.Errorf("expected last_stage = stage_cmd, got %v", outcome.ContextUpdates["last_stage"])
	}
}

// --- Stderr capture on success ---

func TestToolHandlerCapturesStderrOnSuccess(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "stderr_ok",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": "sh -c 'echo warning >&2; echo done'",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected success, got %v", outcome.Status)
	}
	stderr := outcome.ContextUpdates["tool.stderr"].(string)
	if !strings.Contains(stderr, "warning") {
		t.Errorf("expected 'warning' in tool.stderr, got %q", stderr)
	}
	stdout := outcome.ContextUpdates["tool.stdout"].(string)
	if !strings.Contains(stdout, "done") {
		t.Errorf("expected 'done' in tool.stdout, got %q", stdout)
	}
}

// --- Multiline command ---

func TestToolHandlerMultilineOutput(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "multi_line",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": "sh -c 'echo line1; echo line2; echo line3'",
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected success, got %v", outcome.Status)
	}
	stdout := outcome.ContextUpdates["tool.stdout"].(string)
	if !strings.Contains(stdout, "line1") || !strings.Contains(stdout, "line2") || !strings.Contains(stdout, "line3") {
		t.Errorf("expected all three lines in stdout, got %q", stdout)
	}
}

// --- Empty command string ---

func TestToolHandlerEmptyCommandString(t *testing.T) {
	h := &ToolHandler{}
	node := &Node{
		ID: "empty_cmd",
		Attrs: map[string]string{
			"shape":   "parallelogram",
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
		t.Errorf("expected fail for empty command, got %v", outcome.Status)
	}
}

// --- Verify env vars inherit from parent process ---

func TestToolHandlerEnvVarsInheritParent(t *testing.T) {
	// Set a parent env var and verify it's visible
	envKey := "SKULLCRUSHER_TEST_VAR_" + fmt.Sprintf("%d", time.Now().UnixNano())
	os.Setenv(envKey, "parent_value")
	defer os.Unsetenv(envKey)

	h := &ToolHandler{}
	node := &Node{
		ID: "inherit_env",
		Attrs: map[string]string{
			"shape":   "parallelogram",
			"command": fmt.Sprintf("sh -c 'echo $%s'", envKey),
		},
	}
	pctx := NewContext()
	store := NewArtifactStore(t.TempDir())

	outcome, err := h.Execute(context.Background(), node, pctx, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusSuccess {
		t.Errorf("expected success, got %v", outcome.Status)
	}
	stdout := outcome.ContextUpdates["tool.stdout"].(string)
	if !strings.Contains(stdout, "parent_value") {
		t.Errorf("expected parent env var in stdout, got %q", stdout)
	}
}
