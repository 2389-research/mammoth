// ABOUTME: Pipeline execution engine implementing the 5-phase lifecycle: PARSE, VALIDATE, INITIALIZE, EXECUTE, FINALIZE.
// ABOUTME: Orchestrates graph traversal, handler dispatch, retry logic, checkpointing, and edge selection.
package attractor

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
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
	EventStageStalled      EngineEventType = "stage.stalled"
	EventCheckpointSaved   EngineEventType = "checkpoint.saved"

	// Agent-level observability events bridged from the coding agent session.
	EventAgentToolCallStart EngineEventType = "agent.tool_call.start"
	EventAgentToolCallEnd   EngineEventType = "agent.tool_call.end"
	EventAgentLLMTurn       EngineEventType = "agent.llm_turn"
	EventAgentSteering      EngineEventType = "agent.steering"
	EventAgentLoopDetected  EngineEventType = "agent.loop_detected"
)

// EngineEvent represents a lifecycle event emitted by the engine during pipeline execution.
type EngineEvent struct {
	Type      EngineEventType
	NodeID    string
	Data      map[string]any
	Timestamp time.Time
}

// EngineConfig holds configuration for the pipeline execution engine.
type EngineConfig struct {
	CheckpointDir      string            // directory for checkpoint files (empty = no checkpoints)
	AutoCheckpointPath string            // path to overwrite with latest checkpoint after each node (empty = disabled)
	ArtifactDir        string            // directory for artifact storage (empty = use ArtifactsBaseDir/<RunID>)
	ArtifactsBaseDir   string            // base directory for run directories (default = "./artifacts")
	RunID              string            // run identifier for the artifact subdirectory (empty = auto-generated)
	Transforms         []Transform       // transforms to apply (nil = DefaultTransforms)
	ExtraLintRules     []LintRule        // additional validation rules
	DefaultRetry       RetryPolicy       // default retry policy for nodes
	Handlers           *HandlerRegistry  // nil = DefaultHandlerRegistry
	EventHandler       func(EngineEvent) // optional event callback
	Backend            CodergenBackend   // backend for codergen nodes (nil = stub behavior)
	BaseURL            string            // default API base URL for codergen nodes (overridable per-node)
	RestartConfig      *RestartConfig    // loop restart configuration (nil = DefaultRestartConfig)
}

// NodeHandlerUnwrapper allows handler wrappers to expose their inner handler.
// This enables backend wiring to reach through decorator layers (e.g.
// interviewerInjectingHandler) to the underlying CodergenHandler.
type NodeHandlerUnwrapper interface {
	InnerHandler() NodeHandler
}

// unwrapHandler peels through wrapper layers implementing NodeHandlerUnwrapper
// until it reaches a handler that does not wrap another handler.
func unwrapHandler(h NodeHandler) NodeHandler {
	for {
		u, ok := h.(NodeHandlerUnwrapper)
		if !ok {
			return h
		}
		h = u.InnerHandler()
	}
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
		parseErr := fmt.Errorf("parse error: %w", err)
		e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": parseErr.Error()}})
		return nil, parseErr
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
		validationErr := fmt.Errorf("validation failed: %w", err)
		e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": validationErr.Error()}})
		return nil, validationErr
	}

	// Phase 2b: PREFLIGHT — verify backend availability, env vars, etc.
	preflightChecks := BuildPreflightChecks(graph, e.config)
	if len(preflightChecks) > 0 {
		preflightResult := RunPreflight(ctx, preflightChecks)
		if !preflightResult.OK() {
			preflightErr := fmt.Errorf("%s", preflightResult.Error())
			e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": preflightErr.Error()}})
			return nil, preflightErr
		}
	}

	// Phase 3: INITIALIZE
	pctx := NewContext()

	// Mirror graph attributes into context
	for k, v := range graph.Attrs {
		pctx.Set(k, v)
	}

	// Store graph reference for handlers that need graph traversal (e.g. ParallelHandler)
	pctx.Set("_graph", graph)

	artifactDir, err := e.resolveArtifactDir()
	if err != nil {
		return nil, err
	}
	store := NewArtifactStore(artifactDir)
	pctx.Set("_workdir", artifactDir)

	registry := e.config.Handlers
	if registry == nil {
		registry = DefaultHandlerRegistry()
	}

	// Wire the backend into the codergen handler if configured.
	// unwrapHandler peels through decorator layers (e.g. interviewerInjectingHandler)
	// so the type assertion reaches the underlying CodergenHandler.
	if e.config.Backend != nil {
		if codergenHandler := registry.Get("codergen"); codergenHandler != nil {
			if ch, ok := unwrapHandler(codergenHandler).(*CodergenHandler); ok {
				ch.Backend = e.config.Backend
				ch.BaseURL = e.config.BaseURL
				ch.EventHandler = e.emitEvent
			}
		}
	}

	// Phase 4: EXECUTE with restart loop
	e.emitEvent(EngineEvent{Type: EventPipelineStarted})

	restartCfg := e.config.RestartConfig
	if restartCfg == nil {
		restartCfg = DefaultRestartConfig()
	}

	var startAtNode *Node
	restartCount := 0

	for {
		// Check context cancellation before each execution attempt
		select {
		case <-ctx.Done():
			e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": ctx.Err().Error()}})
			return nil, ctx.Err()
		default:
		}

		result, err := e.executeGraph(ctx, graph, pctx, store, registry, startAtNode, nil)

		var restartErr *ErrLoopRestart
		if errors.As(err, &restartErr) {
			restartCount++
			if restartCount > restartCfg.MaxRestarts {
				e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": "max restart limit exceeded"}})
				return nil, fmt.Errorf("loop_restart limit exceeded: %d restart(s) performed, max is %d", restartCount, restartCfg.MaxRestarts)
			}

			// Create fresh context, re-mirror graph attributes
			pctx = NewContext()
			for k, v := range graph.Attrs {
				pctx.Set(k, v)
			}
			pctx.Set("_graph", graph)
			pctx.Set("_workdir", artifactDir)

			// Set the restart target node
			targetNode := graph.FindNode(restartErr.TargetNode)
			if targetNode == nil {
				e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": "restart target not found"}})
				return nil, fmt.Errorf("loop_restart target node %q not found", restartErr.TargetNode)
			}
			startAtNode = targetNode
			continue
		}

		if err != nil {
			e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": err.Error()}})
			return result, err
		}

		// Phase 5: FINALIZE
		e.emitEvent(EngineEvent{Type: EventPipelineCompleted})
		return result, nil
	}
}

// resumeState holds state for checkpoint resume, carrying forward previously
// completed nodes and retry counters from the checkpoint.
type resumeState struct {
	// completedNodes pre-populates the completed list from checkpoint state.
	completedNodes []string
	// nodeRetries pre-populates retry counters from checkpoint state.
	nodeRetries map[string]int
}

// ResumeFromCheckpoint loads a checkpoint from disk and resumes graph execution
// from the node after the checkpointed node. If the previous node used full fidelity,
// the first hop after resume is degraded to summary:high since in-memory LLM sessions
// cannot be serialized.
func (e *Engine) ResumeFromCheckpoint(ctx context.Context, graph *Graph, checkpointPath string) (*RunResult, error) {
	cp, err := LoadCheckpoint(checkpointPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint: %w", err)
	}

	// Validate that the checkpoint node exists in the graph
	cpNode := graph.FindNode(cp.CurrentNode)
	if cpNode == nil {
		return nil, fmt.Errorf("checkpoint references node %q which does not exist in graph", cp.CurrentNode)
	}

	// Restore context from checkpoint so SelectEdge can use "outcome" and "preferred_label".
	pctx := NewContext()
	for k, v := range cp.ContextValues {
		pctx.Set(k, v)
	}
	for _, logEntry := range cp.Logs {
		pctx.AppendLog(logEntry)
	}

	// Reconstruct the outcome from the checkpointed context so SelectEdge picks the
	// correct edge for branched graphs (instead of always taking outEdges[0]).
	cpOutcome := &Outcome{Status: StatusSuccess}
	if outcomeStr, ok := cp.ContextValues["outcome"]; ok {
		if s, ok := outcomeStr.(string); ok {
			cpOutcome.Status = StageStatus(s)
		}
	}
	if prefLabel, ok := cp.ContextValues["preferred_label"]; ok {
		if s, ok := prefLabel.(string); ok {
			cpOutcome.PreferredLabel = s
		}
	}

	// Select the edge from the checkpoint node using the same logic as normal execution.
	selectedEdge := SelectEdge(cpNode, cpOutcome, pctx, graph)
	if selectedEdge == nil {
		// Fallback: try first outgoing edge
		outEdges := graph.OutgoingEdges(cp.CurrentNode)
		if len(outEdges) == 0 {
			return nil, fmt.Errorf("checkpoint node %q has no outgoing edges, cannot resume", cp.CurrentNode)
		}
		selectedEdge = outEdges[0]
	}

	nextNode := graph.FindNode(selectedEdge.To)
	if nextNode == nil {
		return nil, fmt.Errorf("edge from checkpoint node %q points to nonexistent node %q", cp.CurrentNode, selectedEdge.To)
	}

	// Determine if the fidelity for the selected edge would be full.
	// If so, we must degrade the first hop to summary:high.
	fidelityMode := ResolveFidelity(selectedEdge, nextNode, graph)

	// Mirror graph attributes into context
	for k, v := range graph.Attrs {
		pctx.Set(k, v)
	}
	pctx.Set("_graph", graph)

	// Apply fidelity degradation on resume: if the previous node (checkpoint node)
	// used full fidelity, degrade to summary:high for the first resumed node because
	// in-memory LLM sessions cannot be serialized across checkpoint boundaries.
	if fidelityMode == FidelityFull {
		transformed, fidelityPreamble := ApplyFidelity(pctx, FidelitySummaryHigh, FidelityOptions{})
		pctx = transformed
		if fidelityPreamble != "" {
			pctx.Set("_fidelity_preamble", fidelityPreamble)
		}
		pctx.Set("_graph", graph)
	} else {
		// Non-full fidelity mode: apply it normally without degradation
		transformed, fidelityPreamble := ApplyFidelity(pctx, fidelityMode, FidelityOptions{})
		pctx = transformed
		if fidelityPreamble != "" {
			pctx.Set("_fidelity_preamble", fidelityPreamble)
		}
		pctx.Set("_graph", graph)
	}

	artifactDir, resolveErr := e.resolveArtifactDir()
	if resolveErr != nil {
		return nil, resolveErr
	}
	store := NewArtifactStore(artifactDir)

	registry := e.config.Handlers
	if registry == nil {
		registry = DefaultHandlerRegistry()
	}

	if e.config.Backend != nil {
		if codergenHandler := registry.Get("codergen"); codergenHandler != nil {
			if ch, ok := unwrapHandler(codergenHandler).(*CodergenHandler); ok {
				ch.Backend = e.config.Backend
				ch.BaseURL = e.config.BaseURL
				ch.EventHandler = e.emitEvent
			}
		}
	}

	e.emitEvent(EngineEvent{Type: EventPipelineStarted, Data: map[string]any{"resumed": true, "from_node": cp.CurrentNode}})

	rs := &resumeState{
		completedNodes: cp.CompletedNodes,
		nodeRetries:    cp.NodeRetries,
	}

	result, err := e.executeGraph(ctx, graph, pctx, store, registry, nextNode, rs)
	if err != nil {
		e.emitEvent(EngineEvent{Type: EventPipelineFailed, Data: map[string]any{"error": err.Error()}})
		return result, err
	}

	e.emitEvent(EngineEvent{Type: EventPipelineCompleted, Data: map[string]any{"resumed": true}})
	return result, nil
}

// executeGraph implements the core traversal loop.
// startAtNode overrides the start node when non-nil (used for loop_restart or resume).
// rs provides optional resume state; pass nil for fresh runs.
func (e *Engine) executeGraph(
	ctx context.Context,
	graph *Graph,
	pctx *Context,
	store *ArtifactStore,
	registry *HandlerRegistry,
	startAtNode *Node,
	rs *resumeState,
) (*RunResult, error) {
	var currentNode *Node
	if startAtNode != nil {
		currentNode = startAtNode
	} else {
		startNode := graph.FindStartNode()
		if startNode == nil {
			return nil, fmt.Errorf("graph has no start node (shape=Mdiamond)")
		}
		currentNode = startNode
	}

	completedNodes := make([]string, 0)
	nodeOutcomes := make(map[string]*Outcome)
	nodeRetries := make(map[string]int)

	// If resuming, pre-populate completed nodes and retry counters from checkpoint
	if rs != nil {
		completedNodes = append(completedNodes, rs.completedNodes...)
		for k, v := range rs.nodeRetries {
			nodeRetries[k] = v
		}
	}

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
				outcome, err := safeExecute(ctx, handler, node, pctx, store)
				if err != nil {
					e.emitEvent(EngineEvent{Type: EventStageFailed, NodeID: node.ID, Data: map[string]any{"reason": err.Error()}})
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
			e.emitEvent(EngineEvent{Type: EventStageFailed, NodeID: node.ID, Data: map[string]any{"reason": err.Error()}})
			return nil, fmt.Errorf("node %q execution error: %w", node.ID, err)
		}

		// Step 3: Record completion
		completedNodes = append(completedNodes, node.ID)
		nodeOutcomes[node.ID] = outcome

		if outcome.Status == StatusSuccess || outcome.Status == StatusPartialSuccess {
			e.emitEvent(EngineEvent{Type: EventStageCompleted, NodeID: node.ID})
		} else {
			failData := map[string]any{"status": string(outcome.Status)}
			if outcome.FailureReason != "" {
				failData["reason"] = outcome.FailureReason
			}
			e.emitEvent(EngineEvent{Type: EventStageFailed, NodeID: node.ID, Data: failData})
		}

		// Step 4: Apply context updates
		if outcome.ContextUpdates != nil {
			pctx.ApplyUpdates(outcome.ContextUpdates)
		}
		pctx.Set("outcome", string(outcome.Status))
		if outcome.PreferredLabel != "" {
			pctx.Set("preferred_label", outcome.PreferredLabel)
		}

		// Step 4b: Parallel branch detection and execution
		// When a ParallelHandler runs, it sets parallel.branches in context.
		// Detect this and dispatch parallel execution before continuing.
		if branchesVal := pctx.Get("parallel.branches"); branchesVal != nil {
			if branchIDs, ok := branchesVal.([]string); ok && len(branchIDs) > 0 {
				parallelCfg := ParallelConfigFromContext(pctx)
				branchResults, parallelErr := ExecuteParallelBranches(ctx, graph, pctx, store, registry, branchIDs, parallelCfg)
				if parallelErr != nil {
					return nil, fmt.Errorf("parallel execution from node %q failed: %w", node.ID, parallelErr)
				}

				mergeErr := MergeContexts(pctx, branchResults, parallelCfg.JoinPolicy)
				if mergeErr != nil {
					return nil, fmt.Errorf("parallel merge at node %q failed: %w", node.ID, mergeErr)
				}

				// Record branch nodes as completed
				for _, br := range branchResults {
					completedNodes = append(completedNodes, br.NodeID)
					if br.Outcome != nil {
						nodeOutcomes[br.NodeID] = br.Outcome
					}
				}

				// Clear parallel.branches to prevent re-triggering
				pctx.Set("parallel.branches", nil)

				// Find and advance to the fan-in node
				fanInNode := findFanInNode(graph, branchIDs)
				if fanInNode != nil {
					currentNode = fanInNode
					continue
				}
				// No fan-in node found, fall through to normal edge selection
			}
		}

		// Step 5: Save checkpoint
		if e.config.CheckpointDir != "" {
			cp := NewCheckpoint(pctx, node.ID, completedNodes, nodeRetries)
			cpPath := filepath.Join(e.config.CheckpointDir, fmt.Sprintf("checkpoint_%s_%d.json", sanitizeNodeID(node.ID), time.Now().UnixNano()))
			if saveErr := cp.Save(cpPath); saveErr != nil {
				pctx.AppendLog(fmt.Sprintf("warning: failed to save checkpoint: %v", saveErr))
			} else {
				e.emitEvent(EngineEvent{Type: EventCheckpointSaved, NodeID: node.ID})
			}
		}

		// Step 5b: Save auto-checkpoint (single overwriting file for auto-resume)
		// Only save on success so the checkpoint represents the last known good state.
		// On resume, the engine will re-execute from the node after the checkpoint.
		if e.config.AutoCheckpointPath != "" && (outcome.Status == StatusSuccess || outcome.Status == StatusPartialSuccess) {
			cp := NewCheckpoint(pctx, node.ID, completedNodes, nodeRetries)
			if saveErr := cp.Save(e.config.AutoCheckpointPath); saveErr != nil {
				pctx.AppendLog(fmt.Sprintf("warning: failed to save auto-checkpoint: %v", saveErr))
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

		// Step 7: Handle loop_restart
		if EdgeHasLoopRestart(nextEdge) {
			return nil, &ErrLoopRestart{TargetNode: nextEdge.To}
		}

		// Step 7.5: Apply fidelity transform for the transition
		nextNode := graph.FindNode(nextEdge.To)
		if nextNode == nil {
			return nil, fmt.Errorf("edge from %q points to nonexistent node %q", node.ID, nextEdge.To)
		}
		fidelityMode := ResolveFidelity(nextEdge, nextNode, graph)
		if fidelityMode != FidelityFull {
			transformed, fidelityPreamble := ApplyFidelity(pctx, fidelityMode, FidelityOptions{})
			pctx = transformed
			if fidelityPreamble != "" {
				pctx.Set("_fidelity_preamble", fidelityPreamble)
			}
			// Restore engine-managed references that compact mode may have removed
			pctx.Set("_graph", graph)
			if store != nil && store.BaseDir() != "" {
				pctx.Set("_workdir", store.BaseDir())
			}
		} else {
			// Full fidelity: clear any stale preamble from a prior non-full transition
			// (e.g. resume degradation or a previous compact/summary edge)
			pctx.Set("_fidelity_preamble", nil)
		}

		// Step 8: Advance
		currentNode = nextNode
	}

	return &RunResult{
		FinalOutcome:   finalOutcome,
		CompletedNodes: completedNodes,
		NodeOutcomes:   nodeOutcomes,
		Context:        pctx,
	}, nil
}

// sanitizeNodeID replaces path separators and other unsafe characters in a node ID
// to prevent path traversal attacks when used in filenames.
func sanitizeNodeID(id string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", "..", "_", string(os.PathSeparator), "_")
	return r.Replace(id)
}

// safeExecute wraps handler.Execute with panic recovery, converting panics into errors.
// This prevents a single misbehaving handler from crashing the entire engine.
// The stack trace is included in the error message to aid debugging.
func safeExecute(ctx context.Context, handler NodeHandler, node *Node, pctx *Context, store *ArtifactStore) (outcome *Outcome, err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			err = fmt.Errorf("handler panic in node %q: %v\n%s", node.ID, r, stack)
			outcome = nil
		}
	}()
	return handler.Execute(ctx, node, pctx, store)
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

		outcome, err := safeExecute(ctx, handler, node, pctx, store)

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

// SetEventHandler sets the engine's event callback after creation.
// This allows external components (like the TUI) to wire into the event stream.
func (e *Engine) SetEventHandler(handler func(EngineEvent)) {
	e.config.EventHandler = handler
}

// GetEventHandler returns the engine's current event callback, or nil if none is set.
func (e *Engine) GetEventHandler() func(EngineEvent) {
	return e.config.EventHandler
}

// GetHandler returns the handler registered for the given type string from the engine's
// handler registry. Returns nil if no registry is configured or the handler type is not found.
// If no registry was configured, a default registry is initialized first.
func (e *Engine) GetHandler(typeName string) NodeHandler {
	if e.config.Handlers == nil {
		e.config.Handlers = DefaultHandlerRegistry()
	}
	return e.config.Handlers.Get(typeName)
}

// SetHandler registers a handler in the engine's handler registry.
// If no registry was configured, a default registry is initialized first.
func (e *Engine) SetHandler(handler NodeHandler) {
	if e.config.Handlers == nil {
		e.config.Handlers = DefaultHandlerRegistry()
	}
	e.config.Handlers.Register(handler)
}

// resolveArtifactDir determines the artifact directory for this pipeline run.
// If ArtifactDir is set explicitly, it is used as-is. Otherwise, a structured
// run directory is created under ArtifactsBaseDir (default "./artifacts") using
// RunID as the subdirectory name (auto-generated if not set).
func (e *Engine) resolveArtifactDir() (string, error) {
	if e.config.ArtifactDir != "" {
		return e.config.ArtifactDir, nil
	}

	baseDir := e.config.ArtifactsBaseDir
	if baseDir == "" {
		baseDir = "artifacts"
	}

	runID := e.config.RunID
	if runID == "" {
		runID = generateRunID()
	}

	rd, err := NewRunDirectory(baseDir, runID)
	if err != nil {
		return "", fmt.Errorf("create run directory: %w", err)
	}

	return rd.BaseDir, nil
}

// generateRunID creates a random hex ID for a pipeline run.
func generateRunID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// emitEvent sends an event to the configured event handler, if any.
// It stamps each event with the current time if Timestamp is not already set.
func (e *Engine) emitEvent(evt EngineEvent) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	if e.config.EventHandler != nil {
		e.config.EventHandler(evt)
	}
}
