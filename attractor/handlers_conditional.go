// ABOUTME: Conditional branching handler for the attractor pipeline runner.
// ABOUTME: Returns success for diamond-shaped routing nodes; actual routing is handled by the engine's edge selection.
package attractor

import (
	"context"
)

// ConditionalHandler handles conditional routing nodes (shape=diamond).
// The handler itself is a no-op that returns success. The actual routing
// is handled by the execution engine's edge selection algorithm, which
// evaluates conditions on outgoing edges using EvaluateCondition.
type ConditionalHandler struct{}

// Type returns the handler type string "conditional".
func (h *ConditionalHandler) Type() string {
	return "conditional"
}

// Execute returns success with a note describing the conditional evaluation.
// Edge condition evaluation and routing are performed by the engine after this handler returns.
func (h *ConditionalHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &Outcome{
		Status: StatusSuccess,
		Notes:  "Conditional node evaluated: " + node.ID,
		ContextUpdates: map[string]any{
			"last_stage": node.ID,
		},
	}, nil
}
