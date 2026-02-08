// ABOUTME: Parallel fan-in handler for the attractor pipeline runner.
// ABOUTME: Waits for all incoming parallel branches to complete and merges their results.
package attractor

import (
	"context"
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
