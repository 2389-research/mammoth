// ABOUTME: Deterministic verify handler that executes shell commands without an LLM.
// ABOUTME: Maps to shape=octagon. Uses exit code for pass/fail, zero token cost.
package attractor

import (
	"context"
	"fmt"
	"time"
)

// VerifyHandler handles deterministic verification nodes (shape=octagon).
// It runs a shell command and uses the exit code for pass/fail routing.
// No LLM is involved — this is pure command execution.
type VerifyHandler struct{}

// Type returns the handler type string "verify".
func (h *VerifyHandler) Type() string {
	return "verify"
}

// Execute runs the command specified in the node's "command" attribute.
// Exit code 0 -> StatusSuccess, non-zero -> StatusFail.
// Sets "outcome" in ContextUpdates for conditional edge routing.
func (h *VerifyHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	command := attrs["command"]
	if command == "" {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "no command attribute specified for verify node: " + node.ID,
			ContextUpdates: map[string]any{
				"outcome":    "fail",
				"last_stage": node.ID,
			},
		}, nil
	}

	// Parse timeout from node attribute, falling back to the default
	timeout := defaultVerifyTimeout
	if timeoutStr := attrs["timeout"]; timeoutStr != "" {
		if parsed, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = parsed
		}
	}

	// Resolve working directory: explicit attribute > artifact store base dir
	workDir := attrs["working_dir"]
	if workDir == "" && store != nil && store.BaseDir() != "" {
		workDir = store.BaseDir()
	}

	result := runVerifyCommand(ctx, command, workDir, timeout)

	// Store the combined output as an artifact for later inspection
	if store != nil {
		artifactID := node.ID + ".output"
		output := fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", result.ExitCode, result.Stdout, result.Stderr)
		_, _ = store.Store(artifactID, "verify_output", []byte(output))
	}

	status := StatusSuccess
	outcomeStr := "success"
	if !result.Success {
		status = StatusFail
		outcomeStr = "fail"
	}

	failureReason := ""
	if !result.Success {
		failureReason = fmt.Sprintf("verify command failed (exit %d): %s", result.ExitCode, result.Stderr)
		if result.TimedOut {
			failureReason = fmt.Sprintf("verify command timed out after %s", timeout)
		}
	}

	return &Outcome{
		Status:        status,
		Notes:         result.Stdout,
		FailureReason: failureReason,
		ContextUpdates: map[string]any{
			"outcome":    outcomeStr,
			"last_stage": node.ID,
		},
	}, nil
}
