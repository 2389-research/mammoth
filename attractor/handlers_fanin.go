// ABOUTME: Parallel fan-in handler for the attractor pipeline runner.
// ABOUTME: Waits for all incoming parallel branches to complete and merges their results.
package attractor

import (
	"context"
	"fmt"
)

// FanInHandler handles parallel fan-in nodes (shape=tripleoctagon).
// It reads parallel results from the pipeline context and consolidates them.
// If no parallel results are available, it returns a failure.
type FanInHandler struct{}

// Type returns the handler type string "parallel.fan_in".
func (h *FanInHandler) Type() string {
	return "parallel.fan_in"
}

// Execute reads parallel branch results from context and merges them.
// Returns success when results are present, or failure if none are found.
func (h *FanInHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Read parallel results from context
	results := pctx.Get("parallel.results")
	if results == nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "No parallel results to evaluate for fan-in node: " + node.ID,
		}, nil
	}

	// Post-merge verification
	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}
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
				FailureReason: fmt.Sprintf("fan-in verify_command failed (exit %d): %s", vResult.ExitCode, vResult.Stderr),
				ContextUpdates: map[string]any{
					"last_stage": node.ID,
				},
			}, nil
		}
	}

	// The results are present; record the merge
	return &Outcome{
		Status: StatusSuccess,
		Notes:  "Fan-in merged parallel results at node: " + node.ID,
		ContextUpdates: map[string]any{
			"last_stage":                node.ID,
			"parallel.fan_in.completed": true,
		},
	}, nil
}
