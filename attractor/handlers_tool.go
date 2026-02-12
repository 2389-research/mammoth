// ABOUTME: Tool handler that executes shell commands via os/exec for the attractor pipeline.
// ABOUTME: Supports timeout, working directory, env vars, and stores large output as artifacts.
package attractor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// maxNotesBytes is the maximum size of stdout to include directly in Outcome.Notes.
// Output exceeding this threshold is stored as an artifact instead.
const maxNotesBytes = 10 * 1024 // 10KB

// defaultToolTimeout is used when no timeout attribute is specified on the node.
const defaultToolTimeout = 30 * time.Second

// ToolHandler handles external tool execution nodes (shape=parallelogram).
// It reads the command from node attributes, executes it via the system shell,
// and captures stdout, stderr, and exit code.
type ToolHandler struct{}

// Type returns the handler type string "tool".
func (h *ToolHandler) Type() string {
	return "tool"
}

// Execute runs the shell command specified in the node's "command" (or "prompt") attribute.
// It configures timeout, working directory, and environment variables from node attributes,
// then returns an Outcome with stdout, stderr, and exit code.
func (h *ToolHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	// Resolve command: "command" takes precedence, then "prompt"
	command := attrs["command"]
	if command == "" {
		command = attrs["prompt"]
	}
	if command == "" {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "no command or prompt attribute specified for tool node: " + node.ID,
		}, nil
	}

	// Parse timeout
	timeout := defaultToolTimeout
	if timeoutStr, ok := attrs["timeout"]; ok && timeoutStr != "" {
		parsed, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("invalid timeout %q for tool node %s: %v", timeoutStr, node.ID, err),
			}, nil
		}
		timeout = parsed
	}

	// Collect env_* attributes
	envVars := collectEnvVars(attrs)

	// Build the command with timeout context
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)

	// Set process group so we can kill the entire tree on timeout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Set working directory
	if workDir, ok := attrs["working_dir"]; ok && workDir != "" {
		if _, err := os.Stat(workDir); err != nil {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("invalid working_dir %q for tool node %s: %v", workDir, node.ID, err),
			}, nil
		}
		cmd.Dir = workDir
	}

	// Set environment: inherit parent env, then overlay env_* attributes
	if len(envVars) > 0 {
		cmd.Env = buildEnv(envVars)
	}

	// Capture stdout and stderr
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Run the command
	runErr := cmd.Run()

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()
	exitCode := 0

	updates := map[string]any{
		"last_stage":     node.ID,
		"tool.stdout":    stdout,
		"tool.stderr":    stderr,
		"tool.exit_code": exitCode,
	}

	if runErr != nil {
		// Extract exit code from the error
		exitCode = extractExitCode(runErr)
		updates["tool.exit_code"] = exitCode

		// Determine failure reason
		failureReason := fmt.Sprintf("command failed with exit code %d: %s", exitCode, runErr.Error())
		if cmdCtx.Err() == context.DeadlineExceeded {
			failureReason = fmt.Sprintf("command timeout after %s for tool node %s", timeout, node.ID)
			// Kill the process group on timeout
			killProcessGroup(cmd)
		}

		notes := buildNotes(stdout, node.ID, store)

		return &Outcome{
			Status:         StatusFail,
			FailureReason:  failureReason,
			Notes:          notes,
			ContextUpdates: updates,
		}, nil
	}

	notes := buildNotes(stdout, node.ID, store)

	return &Outcome{
		Status:         StatusSuccess,
		Notes:          notes,
		ContextUpdates: updates,
	}, nil
}

// collectEnvVars extracts all env_* attributes into a key-value map,
// stripping the "env_" prefix from the keys.
func collectEnvVars(attrs map[string]string) map[string]string {
	envVars := make(map[string]string)
	for k, v := range attrs {
		if strings.HasPrefix(k, "env_") {
			envName := strings.TrimPrefix(k, "env_")
			if envName != "" {
				envVars[envName] = v
			}
		}
	}
	return envVars
}

// buildEnv constructs the full environment for the child process by inheriting
// the parent environment and overlaying the provided env vars.
func buildEnv(envVars map[string]string) []string {
	env := os.Environ()
	for k, v := range envVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

// buildNotes creates the Notes string from stdout, truncating if the output
// exceeds maxNotesBytes and storing the full output as an artifact.
func buildNotes(stdout string, nodeID string, store *ArtifactStore) string {
	if len(stdout) <= maxNotesBytes {
		return stdout
	}

	// Store full output as an artifact
	artifactID := fmt.Sprintf("%s.stdout", nodeID)
	_, _ = store.Store(artifactID, "stdout", []byte(stdout))

	truncated := stdout[:maxNotesBytes]
	return truncated + fmt.Sprintf("\n\n[output truncated at 10KB; full output stored as artifact %q]", artifactID)
}

// extractExitCode pulls the integer exit code from an *exec.ExitError,
// defaulting to 1 if the type doesn't match.
func extractExitCode(err error) int {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

// killProcessGroup sends SIGKILL to the entire process group of the command.
// This ensures child processes spawned by the shell are also terminated.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}
