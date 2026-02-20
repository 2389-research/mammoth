// ABOUTME: Conditional branching handler for the attractor pipeline runner.
// ABOUTME: Passes through the prior node's outcome so edge conditions evaluate against the correct status.
package attractor

import (
	"context"
)

// ConditionalHandler handles conditional routing nodes (shape=diamond).
// It passes through the outcome status from the preceding node so that edge
// conditions like "outcome=FAIL" evaluate correctly. Without this pass-through,
// edge selection would always see "success" and never take the fail branch.
type ConditionalHandler struct{}

// Type returns the handler type string "conditional".
func (h *ConditionalHandler) Type() string {
	return "conditional"
}

// Execute reads the current outcome from the pipeline context (set by the
// preceding node) and returns it as this node's status. This lets the engine's
// edge selection algorithm evaluate conditions against the real upstream result
// rather than a hard-coded success.
func (h *ConditionalHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Read the outcome status set by the preceding node. If the preceding
	// node reported "fail", this diamond must also report "fail" so that
	// condition="outcome=FAIL" edges match during edge selection.
	status := StatusSuccess
	if prev, ok := pctx.Get("outcome").(string); ok && prev != "" {
		status = StageStatus(prev)
	}

	return &Outcome{
		Status: status,
		Notes:  "Conditional node evaluated: " + node.ID,
		ContextUpdates: map[string]any{
			"last_stage": node.ID,
		},
	}, nil
}
