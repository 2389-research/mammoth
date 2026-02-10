// ABOUTME: Stack manager loop handler implementing observe/guard/steer supervision for child pipelines.
// ABOUTME: Uses the ManagerBackend interface for LLM-powered supervision; nil backend falls back to stub logging.
package attractor

import (
	"context"
	"fmt"
	"strconv"
)

// ManagerBackend defines the interface for LLM-powered supervision actions.
// Implementations handle observing agent progress, guarding against drift,
// and steering corrections when the agent goes off-track.
type ManagerBackend interface {
	// Observe inspects the current state of the supervised pipeline and returns
	// a textual observation summarizing progress.
	Observe(ctx context.Context, nodeID string, iteration int, pctx *Context) (string, error)

	// Guard evaluates whether the supervised agent is on track. Returns true if
	// the guard condition is satisfied, false if steering is needed.
	Guard(ctx context.Context, nodeID string, iteration int, observation string, guardCondition string, pctx *Context) (bool, error)

	// Steer applies a correction to the supervised pipeline, returning a textual
	// description of the steering action taken.
	Steer(ctx context.Context, nodeID string, iteration int, steerPrompt string, pctx *Context) (string, error)
}

// ManagerLoopHandler handles stack manager loop nodes (shape=house).
// It runs a supervision loop that observes, guards, and steers a child pipeline
// or agent. When Backend is nil, the handler operates in stub mode, logging
// each step and returning success.
type ManagerLoopHandler struct {
	// Backend provides the LLM-powered observe/guard/steer operations.
	// If nil, the handler uses stub behavior that logs supervision steps.
	Backend ManagerBackend
}

// Type returns the handler type string "stack.manager_loop".
func (h *ManagerLoopHandler) Type() string {
	return "stack.manager_loop"
}

// Execute runs the supervision loop for the given manager node.
// It reads configuration from node attributes, then iterates through
// observe/guard/steer cycles up to max_iterations.
func (h *ManagerLoopHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	attrs := node.Attrs
	if attrs == nil {
		attrs = make(map[string]string)
	}

	cfg := h.readConfig(attrs, pctx)

	// Stub mode: no backend available
	if h.Backend == nil {
		return h.executeStub(node, cfg)
	}

	return h.executeSupervisionLoop(ctx, node, pctx, cfg)
}

// managerConfig holds the parsed configuration for a manager loop node.
type managerConfig struct {
	observePrompt  string
	guardCondition string
	steerPrompt    string
	maxIterations  int
	subPipeline    string

	// Legacy attributes preserved for backward compatibility
	pollInterval  string
	maxCycles     string
	stopCondition string
	actions       string
	childDotfile  string
}

// readConfig parses node and graph attributes into a managerConfig.
func (h *ManagerLoopHandler) readConfig(attrs map[string]string, pctx *Context) managerConfig {
	cfg := managerConfig{
		observePrompt:  attrs["observe_prompt"],
		guardCondition: attrs["guard_condition"],
		steerPrompt:    attrs["steer_prompt"],
		subPipeline:    attrs["sub_pipeline"],
	}

	// Parse max_iterations with default of 10
	cfg.maxIterations = 10
	if raw := attrs["max_iterations"]; raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			cfg.maxIterations = parsed
		}
	}

	// Legacy attributes for backward compatibility
	cfg.pollInterval = attrs["manager.poll_interval"]
	if cfg.pollInterval == "" {
		cfg.pollInterval = "45s"
	}

	cfg.maxCycles = attrs["manager.max_cycles"]
	if cfg.maxCycles == "" {
		cfg.maxCycles = "1000"
	}

	cfg.stopCondition = attrs["manager.stop_condition"]
	cfg.actions = attrs["manager.actions"]
	if cfg.actions == "" {
		cfg.actions = "observe,wait"
	}

	// Read child dotfile from graph attributes
	if graphVal := pctx.Get("_graph"); graphVal != nil {
		if g, ok := graphVal.(*Graph); ok {
			cfg.childDotfile = g.Attrs["stack.child_dotfile"]
		}
	}

	return cfg
}

// executeStub handles the nil-backend case, preserving backward compatibility
// with the original stub implementation while recording all configuration.
func (h *ManagerLoopHandler) executeStub(node *Node, cfg managerConfig) (*Outcome, error) {
	updates := map[string]any{
		"last_stage":            node.ID,
		"manager.poll_interval": cfg.pollInterval,
		"manager.max_cycles":    cfg.maxCycles,
		"manager.actions":       cfg.actions,
	}

	if cfg.childDotfile != "" {
		updates["manager.child_dotfile"] = cfg.childDotfile
	}
	if cfg.stopCondition != "" {
		updates["manager.stop_condition"] = cfg.stopCondition
	}
	if cfg.subPipeline != "" {
		updates["manager.sub_pipeline"] = cfg.subPipeline
	}

	updates["manager.iterations_completed"] = 0
	updates["manager.steers_applied"] = 0

	return &Outcome{
		Status:         StatusSuccess,
		Notes:          "Manager loop configured (stub) at node: " + node.ID,
		ContextUpdates: updates,
	}, nil
}

// executeSupervisionLoop runs the observe/guard/steer loop with a real backend.
func (h *ManagerLoopHandler) executeSupervisionLoop(
	ctx context.Context,
	node *Node,
	pctx *Context,
	cfg managerConfig,
) (*Outcome, error) {
	var lastObservation string
	steersApplied := 0
	iterationsCompleted := 0

	for i := 1; i <= cfg.maxIterations; i++ {
		// Check context cancellation at each iteration
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Step 1: Observe
		observation, err := h.Backend.Observe(ctx, node.ID, i, pctx)
		if err != nil {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("observe failed at iteration %d: %v", i, err),
				ContextUpdates: map[string]any{
					"last_stage":                  node.ID,
					"manager.iterations_completed": iterationsCompleted,
					"manager.steers_applied":       steersApplied,
				},
			}, nil
		}
		lastObservation = observation

		// Step 2: Guard
		onTrack, err := h.Backend.Guard(ctx, node.ID, i, observation, cfg.guardCondition, pctx)
		if err != nil {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("guard failed at iteration %d: %v", i, err),
				ContextUpdates: map[string]any{
					"last_stage":                  node.ID,
					"manager.iterations_completed": iterationsCompleted,
					"manager.steers_applied":       steersApplied,
				},
			}, nil
		}

		// Step 3: Steer (only if guard failed)
		if !onTrack {
			_, err := h.Backend.Steer(ctx, node.ID, i, cfg.steerPrompt, pctx)
			if err != nil {
				return &Outcome{
					Status:        StatusFail,
					FailureReason: fmt.Sprintf("steer failed at iteration %d: %v", i, err),
					ContextUpdates: map[string]any{
						"last_stage":                  node.ID,
						"manager.iterations_completed": iterationsCompleted,
						"manager.steers_applied":       steersApplied,
					},
				}, nil
			}
			steersApplied++
		}

		iterationsCompleted = i
	}

	updates := map[string]any{
		"last_stage":                  node.ID,
		"manager.iterations_completed": iterationsCompleted,
		"manager.steers_applied":       steersApplied,
		"manager.last_observation":     lastObservation,
	}

	if cfg.subPipeline != "" {
		updates["manager.sub_pipeline"] = cfg.subPipeline
	}

	return &Outcome{
		Status:         StatusSuccess,
		Notes:          fmt.Sprintf("Manager loop completed %d iterations with %d steers at node: %s", iterationsCompleted, steersApplied, node.ID),
		ContextUpdates: updates,
	}, nil
}
