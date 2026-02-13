// ABOUTME: Exit node handler for the attractor pipeline runner.
// ABOUTME: Captures the final pipeline state and returns success at the terminal node.
package attractor

import (
	"context"
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

	return &Outcome{
		Status: StatusSuccess,
		Notes:  "Pipeline exited at node: " + node.ID,
		ContextUpdates: map[string]any{
			"_finished_at": time.Now().Format(time.RFC3339Nano),
		},
	}, nil
}
