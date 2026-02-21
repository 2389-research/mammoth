// ABOUTME: Exit node handler for the attractor pipeline runner.
// ABOUTME: Captures the final pipeline state and returns success at the terminal node.
package attractor

import (
	"context"
	"fmt"
	"time"
)

// ExitHandler handles the pipeline exit point node (shape=Msquare).
// It records the finish time and returns success. Goal gate enforcement
// is handled by the execution engine, not by this handler.
type ExitHandler struct{}

// Type returns the handler type string "exit".
func (h *ExitHandler) Type() string {
	return "exit"
}

// Execute captures the final outcome and writes a summary to context.
func (h *ExitHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	// Pre-exit verification
	if verifyCmd := attrs["verify_command"]; verifyCmd != "" {
		workDir := ""
		if store != nil && store.BaseDir() != "" {
			workDir = store.BaseDir()
		}
		vResult := runVerifyCommand(ctx, verifyCmd, workDir, defaultVerifyTimeout)

		if store != nil {
			artifactID := node.ID + ".verify_output"
			output := fmt.Sprintf("exit_code=%d\nstdout:\n%s\nstderr:\n%s", vResult.ExitCode, vResult.Stdout, vResult.Stderr)
			_, _ = store.Store(artifactID, "verify_output", []byte(output))
		}

		if !vResult.Success {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("exit verify_command failed (exit %d): %s", vResult.ExitCode, vResult.Stderr),
				ContextUpdates: map[string]any{
					"_finished_at": time.Now().Format(time.RFC3339Nano),
				},
			}, nil
		}
	}

	return &Outcome{
		Status: StatusSuccess,
		Notes:  "Pipeline exited at node: " + node.ID,
		ContextUpdates: map[string]any{
			"_finished_at": time.Now().Format(time.RFC3339Nano),
		},
	}, nil
}
