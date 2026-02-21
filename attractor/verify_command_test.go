// ABOUTME: Tests for the shared runVerifyCommand helper used by codergen, conditional, fan-in, and exit handlers.
// ABOUTME: Covers exit code detection, timeout, working directory, and output capture.
package attractor

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunVerifyCommandSuccess(t *testing.T) {
	result := runVerifyCommand(context.Background(), "echo hello", "", 10*time.Second)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout == "" {
		t.Error("expected stdout to contain output")
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Errorf("expected stdout to contain 'hello', got %q", result.Stdout)
	}
	if !result.Success {
		t.Error("expected Success=true for exit code 0")
	}
}

func TestRunVerifyCommandFailure(t *testing.T) {
	result := runVerifyCommand(context.Background(), "exit 1", "", 10*time.Second)
	if result.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.ExitCode)
	}
	if result.Success {
		t.Error("expected Success=false for exit code 1")
	}
}

func TestRunVerifyCommandSpecificExitCode(t *testing.T) {
	result := runVerifyCommand(context.Background(), "exit 42", "", 10*time.Second)
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
	if result.Success {
		t.Error("expected Success=false for exit code 42")
	}
}

func TestRunVerifyCommandCapturesStderr(t *testing.T) {
	result := runVerifyCommand(context.Background(), "echo err >&2", "", 10*time.Second)
	if result.Stderr == "" {
		t.Error("expected stderr to contain output")
	}
	if !strings.Contains(result.Stderr, "err") {
		t.Errorf("expected stderr to contain 'err', got %q", result.Stderr)
	}
}

func TestRunVerifyCommandTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process group killing not supported on windows")
	}

	start := time.Now()
	result := runVerifyCommand(context.Background(), "sleep 60", "", 100*time.Millisecond)
	elapsed := time.Since(start)

	if result.Success {
		t.Error("expected failure on timeout")
	}
	if !result.TimedOut {
		t.Error("expected TimedOut=true")
	}
	// Should complete well within 10 seconds, not wait 60
	if elapsed > 10*time.Second {
		t.Errorf("expected timeout to kill early, but took %v", elapsed)
	}
}

func TestRunVerifyCommandWorkDir(t *testing.T) {
	dir := t.TempDir()
	result := runVerifyCommand(context.Background(), "pwd", dir, 10*time.Second)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	// Resolve symlinks for macOS /var -> /private/var
	resolvedDir, _ := filepath.EvalSymlinks(dir)
	resolvedStdout, _ := filepath.EvalSymlinks(strings.TrimSpace(result.Stdout))
	if resolvedStdout != resolvedDir {
		t.Errorf("expected working dir %q, got %q", resolvedDir, resolvedStdout)
	}
}

func TestRunVerifyCommandContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := runVerifyCommand(ctx, "echo hello", "", 10*time.Second)
	if result.Success {
		t.Error("expected failure on cancelled context")
	}
}

func TestRunVerifyCommandDefaultTimeout(t *testing.T) {
	// Passing zero should use the default timeout, not fail immediately
	result := runVerifyCommand(context.Background(), "echo ok", "", 0)
	if !result.Success {
		t.Error("expected success with default timeout (zero value)")
	}
	if !strings.Contains(result.Stdout, "ok") {
		t.Errorf("expected stdout to contain 'ok', got %q", result.Stdout)
	}
}

func TestRunVerifyCommandCapturesBothStreams(t *testing.T) {
	result := runVerifyCommand(context.Background(), "sh -c 'echo out; echo err >&2'", "", 10*time.Second)
	if !result.Success {
		t.Error("expected success")
	}
	if !strings.Contains(result.Stdout, "out") {
		t.Errorf("expected stdout to contain 'out', got %q", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "err") {
		t.Errorf("expected stderr to contain 'err', got %q", result.Stderr)
	}
}
