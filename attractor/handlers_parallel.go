// ABOUTME: Parallel fan-out handler for the attractor pipeline runner.
// ABOUTME: Records outgoing branches for concurrent execution by the engine.
package attractor

import (
	"context"
)

// ParallelHandler handles parallel fan-out nodes (shape=component).
// It identifies all outgoing edges as parallel branches and records them
// in the outcome. The actual concurrent execution is managed by the engine.
type ParallelHandler struct{}

// Type returns the handler type string "parallel".
func (h *ParallelHandler) Type() string {
	return "parallel"
}

// Execute identifies outgoing branches and returns an outcome listing them.
// If there are no outgoing edges, it returns a failure.
func (h *ParallelHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// We need the graph to find outgoing edges, but the handler interface
	// receives only the node. The graph reference is stored in the pipeline
	// context by the engine before handler dispatch. For the stub, we look
	// at the context for a graph reference, or use node attrs.
	// Since we receive pctx (pipeline context), we can check for a graph ref.
	// However, the spec says handlers receive the graph. Since our interface
	// passes pctx rather than the graph directly, we store outgoing edge info
	// in a different way.

	// For now, the parallel handler operates as a stub that reads its
	// configuration from node attrs and signals the engine to fan out.
	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	// Read configuration
	joinPolicy := attrs["join_policy"]
	if joinPolicy == "" {
		joinPolicy = "wait_all"
	}
	errorPolicy := attrs["error_policy"]
	if errorPolicy == "" {
		errorPolicy = "continue"
	}
	maxParallel := attrs["max_parallel"]
	if maxParallel == "" {
		maxParallel = "4"
	}

	// Get branches from the graph stored in context
	graphVal := pctx.Get("_graph")
	var branchIDs []string
	if g, ok := graphVal.(*Graph); ok {
		edges := g.OutgoingEdges(node.ID)
		for _, e := range edges {
			branchIDs = append(branchIDs, e.To)
		}
	}

	if len(branchIDs) == 0 {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "No outgoing branches for parallel node: " + node.ID,
		}, nil
	}

	return &Outcome{
		Status: StatusSuccess,
		Notes:  "Parallel fan-out spawning branches from: " + node.ID,
		ContextUpdates: map[string]any{
			"last_stage":            node.ID,
			"parallel.branches":     branchIDs,
			"parallel.join_policy":  joinPolicy,
			"parallel.error_policy": errorPolicy,
			"parallel.max_parallel": maxParallel,
		},
	}, nil
}
