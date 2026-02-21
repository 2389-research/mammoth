// ABOUTME: Shared verify_command execution helper for pipeline node handlers.
// ABOUTME: Runs a shell command and returns exit code, stdout, stderr for post-execution verification.
package attractor

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

// VerifyResult holds the outcome of a verify_command execution.
type VerifyResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Success  bool // true when ExitCode == 0
	TimedOut bool
}

// defaultVerifyTimeout is used when no timeout is specified.
const defaultVerifyTimeout = 60 * time.Second

// runVerifyCommand executes a shell command and returns the result.
// It uses the same process-group management as ToolHandler for clean cleanup.
func runVerifyCommand(ctx context.Context, command, workDir string, timeout time.Duration) VerifyResult {
	if timeout <= 0 {
		timeout = defaultVerifyTimeout
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)

	// Set process group so we can kill the entire tree on timeout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// When context expires, kill the entire process group (not just the main process).
	// This ensures child processes spawned by the shell are also terminated.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			pgid, err := syscall.Getpgid(cmd.Process.Pid)
			if err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second

	if workDir != "" {
		cmd.Dir = workDir
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()

	result := VerifyResult{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
		}
	}

	result.Success = result.ExitCode == 0
	return result
}
