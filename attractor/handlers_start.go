// ABOUTME: Start node handler for the attractor pipeline runner.
// ABOUTME: Initializes pipeline execution by recording a start timestamp and returning success.
package attractor

import (
	"context"
	"time"
)

// StartHandler handles the pipeline entry point node (shape=Mdiamond).
// It performs no work beyond recording the start time and returning success.
type StartHandler struct{}

// Type returns the handler type string "start".
func (h *StartHandler) Type() string {
	return "start"
}

// Execute initializes context with a start timestamp and returns success.
func (h *StartHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &Outcome{
		Status: StatusSuccess,
		Notes:  "Pipeline started at node: " + node.ID,
		ContextUpdates: map[string]any{
			"_started_at": time.Now().Format(time.RFC3339Nano),
		},
	}, nil
}
