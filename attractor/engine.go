// ABOUTME: Pipeline execution engine implementing the 5-phase lifecycle: PARSE, VALIDATE, INITIALIZE, EXECUTE, FINALIZE.
// ABOUTME: Orchestrates graph traversal, handler dispatch, retry logic, checkpointing, and edge selection.
package attractor

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// EngineEventType identifies the kind of engine lifecycle event.
type EngineEventType string

const (
	EventPipelineStarted   EngineEventType = "pipeline.started"
	EventPipelineCompleted EngineEventType = "pipeline.completed"
	EventPipelineFailed    EngineEventType = "pipeline.failed"
	EventStageStarted      EngineEventType = "stage.started"
	EventStageCompleted    EngineEventType = "stage.completed"
	EventStageFailed       EngineEventType = "stage.failed"
	EventStageRetrying     EngineEventType = "stage.retrying"
	EventCheckpointSaved   EngineEventType = "checkpoint.saved"
)

// EngineEvent represents a lifecycle event emitted by the engine during pipeline execution.
type EngineEvent struct {
	Type   EngineEventType
	NodeID string
	Data   map[string]any
}

// EngineConfig holds configuration for the pipeline execution engine.
type EngineConfig struct {
	CheckpointDir  string           // directory for checkpoint files (empty = no checkpoints)
	ArtifactDir    string           // directory for artifact storage (empty = temp dir)
	Transforms     []Transform      // transforms to apply (nil = DefaultTransforms)
	ExtraLintRules []LintRule        // additional validation rules
	DefaultRetry   RetryPolicy      // default retry policy for nodes
	Handlers       *HandlerRegistry // nil = DefaultHandlerRegistry
	EventHandler   func(EngineEvent) // optional event callback
}

// Engine is the pipeline execution engine that runs attractor graph pipelines.
type Engine struct {
	config EngineConfig
}

// RunResult holds the final state of a completed pipeline execution.
type RunResult struct {
	FinalOutcome   *Outcome
	CompletedNodes []string
	NodeOutcomes   map[string]*Outcome
	Context        *Context
}

// NewEngine creates a new pipeline execution engine with the given configuration.
func NewEngine(config EngineConfig) *Engine {
	return &Engine{config: config}
}

// Run parses DOT source, then runs the resulting graph through the full 5-phase lifecycle.
func (e *Engine) Run(ctx context.Context, source string) (*RunResult, error) {
	// Phase 1: PARSE
	graph, err := Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return e.RunGraph(ctx, graph)
}

// RunGraph runs an already-parsed graph through the VALIDATE, INITIALIZE, EXECUTE, and FINALIZE phases.
func (e *Engine) RunGraph(ctx context.Context, graph *Graph) (*RunResult, error) {
	// Phase 2: VALIDATE
	transforms := e.config.Transforms
	if transforms == nil {
		transforms = DefaultTransforms()
	}
	graph = ApplyTransforms(graph, transforms...)

	_, err := ValidateOrError(graph, e.config.ExtraLintRules...)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Phase 3: INITIALIZE
	pctx := NewContext()

	// Mirror graph attributes into context
	for k, v := range graph.Attrs {
		pctx.Set(k, v)
	}

	artifactDir := e.config.ArtifactDir
	if artifactDir == "" {
		artifactDir = ""
	}
	store := NewArtifactStore(artifactDir)

	registry := e.config.Handlers
	if registry == nil {
		registry = DefaultHandlerRegistry()
	}

	// Phase 4: EXECUTE
	e.emitEvent(EngineEvent{Type: EventPipelineStarted})

	result, err := e.executeGraph(ctx, graph, pctx, store, registry)
	if err != nil {
		e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": err.Error()}})
		return result, err
	}

	// Phase 5: FINALIZE
	e.emitEvent(EngineEvent{Type: EventPipelineCompleted})

	return result, nil
}

// executeGraph implements the core traversal loop.
func (e *Engine) executeGraph(
	ctx context.Context,
	graph *Graph,
	pctx *Context,
	store *ArtifactStore,
	registry *HandlerRegistry,
) (*RunResult, error) {
	startNode := graph.FindStartNode()
	if startNode == nil {
		return nil, fmt.Errorf("graph has no start node (shape=Mdiamond)")
	}

	completedNodes := make([]string, 0)
	nodeOutcomes := make(map[string]*Outcome)
	nodeRetries := make(map[string]int)

	currentNode := startNode
	var finalOutcome *Outcome

	// Guard against infinite loops with a visit counter
	const maxIterations = 10000
	iteration := 0

	for {
		iteration++
		if iteration > maxIterations {
			return nil, fmt.Errorf("execution exceeded maximum iterations (%d), possible infinite loop", maxIterations)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		node := currentNode

		// Step 1: Check for terminal node (shape=Msquare)
		if isTerminal(node) {
			// Execute the terminal node handler before checking gates
			handler := registry.Resolve(node)
			if handler != nil {
				e.emitEvent(EngineEvent{Type: EventStageStarted, NodeID: node.ID})
				outcome, err := handler.Execute(ctx, node, pctx, store)
				if err != nil {
					e.emitEvent(EngineEvent{Type: EventStageFailed, NodeID: node.ID})
					return nil, fmt.Errorf("terminal node %q handler error: %w", node.ID, err)
				}
				completedNodes = append(completedNodes, node.ID)
				nodeOutcomes[node.ID] = outcome
				if outcome.ContextUpdates != nil {
					pctx.ApplyUpdates(outcome.ContextUpdates)
				}
				e.emitEvent(EngineEvent{Type: EventStageCompleted, NodeID: node.ID})
				finalOutcome = outcome
			}

			// Check goal gates
			gateOK, failedNode := checkGoalGates(graph, nodeOutcomes)
			if !gateOK {
				retryTarget := getRetryTarget(failedNode, graph)
				if retryTarget != "" {
					targetNode := graph.FindNode(retryTarget)
					if targetNode != nil {
						currentNode = targetNode
						continue
					}
				}
				return nil, fmt.Errorf("goal gate unsatisfied for node %q, no retry target available", failedNode.ID)
			}

			// Pipeline complete
			break
		}

		// Step 2: Execute node handler with retry
		handler := registry.Resolve(node)
		if handler == nil {
			return nil, fmt.Errorf("no handler found for node %q", node.ID)
		}

		e.emitEvent(EngineEvent{Type: EventStageStarted, NodeID: node.ID})

		retryPolicy := buildRetryPolicy(node, graph, e.config.DefaultRetry)
		outcome, err := executeWithRetry(ctx, handler, node, pctx, store, retryPolicy, nodeRetries, func(attempt int) {
			e.emitEvent(EngineEvent{
				Type:   EventStageRetrying,
				NodeID: node.ID,
				Data:   map[string]any{"attempt": attempt},
			})
		})
		if err != nil {
			e.emitEvent(EngineEvent{Type: EventStageFailed, NodeID: node.ID})
			return nil, fmt.Errorf("node %q execution error: %w", node.ID, err)
		}

		// Step 3: Record completion
		completedNodes = append(completedNodes, node.ID)
		nodeOutcomes[node.ID] = outcome

		if outcome.Status == StatusSuccess || outcome.Status == StatusPartialSuccess {
			e.emitEvent(EngineEvent{Type: EventStageCompleted, NodeID: node.ID})
		} else {
			e.emitEvent(EngineEvent{Type: EventStageFailed, NodeID: node.ID})
		}

		// Step 4: Apply context updates
		if outcome.ContextUpdates != nil {
			pctx.ApplyUpdates(outcome.ContextUpdates)
		}
		pctx.Set("outcome", string(outcome.Status))
		if outcome.PreferredLabel != "" {
			pctx.Set("preferred_label", outcome.PreferredLabel)
		}

		// Step 5: Save checkpoint
		if e.config.CheckpointDir != "" {
			cp := NewCheckpoint(pctx, node.ID, completedNodes, nodeRetries)
			cpPath := filepath.Join(e.config.CheckpointDir, fmt.Sprintf("checkpoint_%s_%d.json", node.ID, time.Now().UnixNano()))
			if saveErr := cp.Save(cpPath); saveErr != nil {
				pctx.AppendLog(fmt.Sprintf("warning: failed to save checkpoint: %v", saveErr))
			} else {
				e.emitEvent(EngineEvent{Type: EventCheckpointSaved, NodeID: node.ID})
			}
		}

		// Step 6: Select next edge
		nextEdge := SelectEdge(node, outcome, pctx, graph)
		if nextEdge == nil {
			if outcome.Status == StatusFail {
				return nil, fmt.Errorf("stage %q failed with no outgoing fail edge", node.ID)
			}
			// No next edge and not a failure -- pipeline ends naturally
			finalOutcome = outcome
			break
		}

		// Step 7: Advance
		nextNode := graph.FindNode(nextEdge.To)
		if nextNode == nil {
			return nil, fmt.Errorf("edge from %q points to nonexistent node %q", node.ID, nextEdge.To)
		}
		currentNode = nextNode
	}

	return &RunResult{
		FinalOutcome:   finalOutcome,
		CompletedNodes: completedNodes,
		NodeOutcomes:   nodeOutcomes,
		Context:        pctx,
	}, nil
}

// executeWithRetry runs a handler with retry logic according to the given policy.
// The onRetry callback (if non-nil) is called before each retry sleep.
func executeWithRetry(
	ctx context.Context,
	handler NodeHandler,
	node *Node,
	pctx *Context,
	store *ArtifactStore,
	policy RetryPolicy,
	nodeRetries map[string]int,
	onRetry func(attempt int),
) (*Outcome, error) {
	shouldRetry := policy.ShouldRetry
	if shouldRetry == nil {
		shouldRetry = DefaultShouldRetry
	}

	var lastOutcome *Outcome
	var lastErr error

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		// Check context cancellation before each attempt
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		outcome, err := handler.Execute(ctx, node, pctx, store)

		if err != nil {
			lastErr = err
			if attempt < policy.MaxAttempts && shouldRetry(err) {
				nodeRetries[node.ID]++
				if onRetry != nil {
					onRetry(attempt)
				}
				delay := policy.Backoff.DelayForAttempt(attempt - 1)
				sleepWithContext(ctx, delay)
				continue
			}
			// Error retries exhausted or not retryable
			if node.Attrs != nil && node.Attrs["allow_partial"] == "true" {
				return &Outcome{
					Status:        StatusPartialSuccess,
					FailureReason: fmt.Sprintf("retries exhausted with error: %v", err),
				}, nil
			}
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("execution error after %d attempt(s): %v", attempt, err),
			}, nil
		}

		lastOutcome = outcome

		switch outcome.Status {
		case StatusSuccess, StatusPartialSuccess:
			// Reset retry counter on success
			nodeRetries[node.ID] = 0
			return outcome, nil

		case StatusRetry:
			if attempt < policy.MaxAttempts {
				nodeRetries[node.ID]++
				if onRetry != nil {
					onRetry(attempt)
				}
				delay := policy.Backoff.DelayForAttempt(attempt - 1)
				sleepWithContext(ctx, delay)
				continue
			}
			// Retries exhausted
			if node.Attrs != nil && node.Attrs["allow_partial"] == "true" {
				return &Outcome{
					Status:        StatusPartialSuccess,
					FailureReason: "retries exhausted",
				}, nil
			}
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("retries exhausted after %d attempt(s)", attempt),
			}, nil

		case StatusFail:
			// Immediate failure, no retry
			return outcome, nil

		case StatusSkipped:
			return outcome, nil
		}
	}

	// Should not reach here, but handle gracefully
	if lastOutcome != nil {
		return lastOutcome, nil
	}
	return nil, lastErr
}

// sleepWithContext sleeps for the given duration, but returns early if the context is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		return
	}
}

// emitEvent sends an event to the configured event handler, if any.
func (e *Engine) emitEvent(evt EngineEvent) {
	if e.config.EventHandler != nil {
		e.config.EventHandler(evt)
	}
}
