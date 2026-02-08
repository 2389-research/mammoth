// ABOUTME: Stack manager loop handler for the attractor pipeline runner.
// ABOUTME: Stub implementation that records loop config; actual child pipeline management is wired in by the engine.
package attractor

import (
	"context"
)

// ManagerLoopHandler handles stack manager loop nodes (shape=house).
// It manages a loop of sub-tasks by delegating to child agents. This stub
// implementation records the loop configuration; actual child pipeline
// management is wired in by the engine.
type ManagerLoopHandler struct{}

// Type returns the handler type string "stack.manager_loop".
func (h *ManagerLoopHandler) Type() string {
	return "stack.manager_loop"
}

// Execute reads manager loop configuration from node and graph attributes,
// records it in the outcome, and returns success.
func (h *ManagerLoopHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	// Read configuration from node attributes
	pollInterval := attrs["manager.poll_interval"]
	if pollInterval == "" {
		pollInterval = "45s"
	}

	maxCycles := attrs["manager.max_cycles"]
	if maxCycles == "" {
		maxCycles = "1000"
	}

	stopCondition := attrs["manager.stop_condition"]
	actions := attrs["manager.actions"]
	if actions == "" {
		actions = "observe,wait"
	}

	// Read child dotfile from graph attributes (stored in pctx by the engine)
	childDotfile := ""
	if graphVal := pctx.Get("_graph"); graphVal != nil {
		if g, ok := graphVal.(*Graph); ok {
			childDotfile = g.Attrs["stack.child_dotfile"]
		}
	}

	updates := map[string]any{
		"last_stage":            node.ID,
		"manager.poll_interval": pollInterval,
		"manager.max_cycles":    maxCycles,
		"manager.actions":       actions,
	}

	if childDotfile != "" {
		updates["manager.child_dotfile"] = childDotfile
	}
	if stopCondition != "" {
		updates["manager.stop_condition"] = stopCondition
	}

	return &Outcome{
		Status:         StatusSuccess,
		Notes:          "Manager loop configured (stub) at node: " + node.ID,
		ContextUpdates: updates,
	}, nil
}
