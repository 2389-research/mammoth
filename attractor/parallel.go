// ABOUTME: Parallel branch execution and context merging for concurrent pipeline fan-out/fan-in.
// ABOUTME: Provides ExecuteParallelBranches, MergeContexts, and supporting types for the engine.
package attractor

import (
	"context"
	"fmt"
	"strconv"
	"sync"
)

// BranchResult holds the outcome of executing a single parallel branch.
type BranchResult struct {
	NodeID        string
	Outcome       *Outcome
	BranchContext *Context
	Error         error
}

// ParallelConfig holds parsed configuration for parallel execution.
type ParallelConfig struct {
	MaxParallel int
	JoinPolicy  string
	ErrorPolicy string
	KRequired   int // For k_of_n policy: minimum number of branches that must succeed
}

// ParallelConfigFromContext reads parallel configuration values from the pipeline
// context and returns a ParallelConfig with defaults applied for missing values.
func ParallelConfigFromContext(pctx *Context) ParallelConfig {
	config := ParallelConfig{
		MaxParallel: 4,
		JoinPolicy:  "wait_all",
		ErrorPolicy: "continue",
	}

	if policy := pctx.GetString("parallel.join_policy", ""); policy != "" {
		config.JoinPolicy = policy
	}
	if policy := pctx.GetString("parallel.error_policy", ""); policy != "" {
		config.ErrorPolicy = policy
	}
	if maxStr := pctx.GetString("parallel.max_parallel", ""); maxStr != "" {
		if n, err := strconv.Atoi(maxStr); err == nil && n > 0 {
			config.MaxParallel = n
		}
	}
	if kStr := pctx.GetString("parallel.k_required", ""); kStr != "" {
		if n, err := strconv.Atoi(kStr); err == nil && n > 0 {
			config.KRequired = n
		}
	}

	return config
}

// ExecuteParallelBranches forks the context for each branch and executes them
// concurrently in separate goroutines. Each branch follows edges from its start
// node until it reaches a fan-in node (shape=tripleoctagon) or a terminal node.
// A buffered channel is used as a semaphore to respect MaxParallel.
//
// Error policies:
//   - "continue": all branches run to completion regardless of failures (default).
//   - "fail_fast": on the first branch error or failure outcome, cancel remaining branches.
func ExecuteParallelBranches(
	ctx context.Context,
	graph *Graph,
	pctx *Context,
	store *ArtifactStore,
	registry *HandlerRegistry,
	branches []string,
	config ParallelConfig,
) ([]BranchResult, error) {
	if len(branches) == 0 {
		return nil, fmt.Errorf("no branches to execute")
	}

	maxParallel := config.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 4
	}

	// For fail_fast, wrap the context with a cancel function so we can
	// cancel remaining branches when one fails.
	branchCtx := ctx
	var cancelBranches context.CancelFunc
	if config.ErrorPolicy == "fail_fast" {
		branchCtx, cancelBranches = context.WithCancel(ctx)
		defer cancelBranches()
	}

	semaphore := make(chan struct{}, maxParallel)
	results := make([]BranchResult, len(branches))
	var wg sync.WaitGroup

	for i, branchID := range branches {
		wg.Add(1)
		go func(idx int, nodeID string) {
			defer wg.Done()

			// Acquire semaphore slot
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-branchCtx.Done():
				results[idx] = BranchResult{
					NodeID: nodeID,
					Error:  branchCtx.Err(),
				}
				return
			}

			// Fork context for this branch
			forkedCtx := pctx.Clone()

			// Execute the branch chain
			outcome, err := executeBranchChain(branchCtx, graph, forkedCtx, store, registry, nodeID)
			results[idx] = BranchResult{
				NodeID:        nodeID,
				Outcome:       outcome,
				BranchContext: forkedCtx,
				Error:         err,
			}

			// For fail_fast, cancel other branches on first failure
			if config.ErrorPolicy == "fail_fast" && cancelBranches != nil {
				if err != nil || (outcome != nil && outcome.Status == StatusFail) {
					cancelBranches()
				}
			}
		}(i, branchID)
	}

	wg.Wait()
	return results, nil
}

// executeBranchChain runs nodes starting from startNodeID, following edges until
// it reaches a fan-in node (shape=tripleoctagon), a terminal node, or runs out
// of edges. It returns the outcome of the last executed node.
func executeBranchChain(
	ctx context.Context,
	graph *Graph,
	pctx *Context,
	store *ArtifactStore,
	registry *HandlerRegistry,
	startNodeID string,
) (*Outcome, error) {
	currentNodeID := startNodeID
	var lastOutcome *Outcome

	const maxSteps = 1000
	for step := 0; step < maxSteps; step++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		node := graph.FindNode(currentNodeID)
		if node == nil {
			return nil, fmt.Errorf("branch node %q not found in graph", currentNodeID)
		}

		// Stop at fan-in nodes without executing them
		if node.Attrs != nil && node.Attrs["shape"] == "tripleoctagon" {
			if lastOutcome == nil {
				return &Outcome{Status: StatusSuccess}, nil
			}
			return lastOutcome, nil
		}

		// Stop at terminal nodes without executing them
		if isTerminal(node) {
			if lastOutcome == nil {
				return &Outcome{Status: StatusSuccess}, nil
			}
			return lastOutcome, nil
		}

		// Resolve and execute the handler
		handler := registry.Resolve(node)
		if handler == nil {
			return nil, fmt.Errorf("no handler found for branch node %q", currentNodeID)
		}

		outcome, err := handler.Execute(ctx, node, pctx, store)
		if err != nil {
			return nil, err
		}
		lastOutcome = outcome

		// Apply context updates from the handler
		if outcome.ContextUpdates != nil {
			pctx.ApplyUpdates(outcome.ContextUpdates)
		}

		// Set outcome in context for edge selection
		pctx.Set("outcome", string(outcome.Status))
		if outcome.PreferredLabel != "" {
			pctx.Set("preferred_label", outcome.PreferredLabel)
		}

		// If the node failed, stop this branch
		if outcome.Status == StatusFail {
			return outcome, nil
		}

		// Select next edge
		nextEdge := SelectEdge(node, outcome, pctx, graph)
		if nextEdge == nil {
			// No outgoing edge, branch ends here
			return outcome, nil
		}

		currentNodeID = nextEdge.To
	}

	return nil, fmt.Errorf("branch execution exceeded maximum steps (%d)", maxSteps)
}

// MergeContexts merges branch results back into the parent context according
// to the specified join policy. All merge operations are logged to the parent
// context's log for visibility, including which branch wrote which values and
// conflict resolution via last-write-wins. Artifact references from branch
// contexts are consolidated into a parallel.artifacts manifest.
//
// Policies:
//   - "wait_all": All branches must succeed. Returns error if any branch failed.
//     Merges all branch contexts into parent (last-write-wins for conflicts).
//   - "wait_any": At least one branch must succeed. Only successful branches
//     are merged. Returns error if all branches failed.
//   - "k_of_n": At least K branches must succeed (K read from parent context
//     key "parallel.k_required", defaults to N). Only successful branches merged.
//   - "quorum": A strict majority (>50%) of branches must succeed.
//     Only successful branches are merged.
func MergeContexts(parent *Context, branches []BranchResult, policy string) error {
	if policy == "" {
		policy = "wait_all"
	}

	parent.AppendLog(fmt.Sprintf("[merge] starting merge with policy %q for %d branch(es)", policy, len(branches)))

	var err error
	switch policy {
	case "wait_all":
		err = mergeWaitAll(parent, branches)
	case "wait_any":
		err = mergeWaitAny(parent, branches)
	case "k_of_n":
		err = mergeKOfN(parent, branches)
	case "quorum":
		err = mergeQuorum(parent, branches)
	default:
		return fmt.Errorf("unknown join policy: %q", policy)
	}

	return err
}

// branchSucceeded returns true if a branch completed without error and without
// a StatusFail outcome.
func branchSucceeded(b BranchResult) bool {
	return b.Error == nil && b.Outcome != nil && b.Outcome.Status != StatusFail
}

// collectSuccessfulBranches filters branches to only those that succeeded.
func collectSuccessfulBranches(branches []BranchResult) []BranchResult {
	var result []BranchResult
	for _, b := range branches {
		if branchSucceeded(b) {
			result = append(result, b)
		}
	}
	return result
}

// mergeBranchContextsWithLogging merges the given branches into the parent context
// using last-write-wins semantics. It logs each branch's contributions and any
// key conflicts to the parent context's log.
func mergeBranchContextsWithLogging(parent *Context, branches []BranchResult) {
	// Take a snapshot of the parent's current keys to detect conflicts
	parentSnap := parent.Snapshot()
	seenKeys := make(map[string]string) // key -> last branch that wrote it

	for _, b := range branches {
		if b.BranchContext == nil {
			continue
		}

		snap := b.BranchContext.Snapshot()
		parent.AppendLog(fmt.Sprintf("[merge] merging %d key(s) from branch %q", len(snap), b.NodeID))

		for k, v := range snap {
			// Check for conflicts with previous branches or parent
			if prevBranch, exists := seenKeys[k]; exists {
				parent.AppendLog(fmt.Sprintf("[merge] key %q: conflict between branch %q and branch %q, resolved via last-write-wins (winner: %q)", k, prevBranch, b.NodeID, b.NodeID))
			} else if _, parentHas := parentSnap[k]; parentHas {
				parentVal := parentSnap[k]
				if fmt.Sprintf("%v", parentVal) != fmt.Sprintf("%v", v) {
					parent.AppendLog(fmt.Sprintf("[merge] key %q: branch %q overwrites parent value via last-write-wins", k, b.NodeID))
				}
			}
			seenKeys[k] = b.NodeID
		}

		parent.ApplyUpdates(snap)
	}
}

// buildArtifactManifest scans branch contexts for artifact ID references and
// builds a per-branch manifest. Artifact IDs are identified by context keys
// that contain "artifact_id" as a substring.
func buildArtifactManifest(branches []BranchResult) map[string][]string {
	manifest := make(map[string][]string)
	for _, b := range branches {
		if b.BranchContext == nil {
			manifest[b.NodeID] = nil
			continue
		}
		snap := b.BranchContext.Snapshot()
		var ids []string
		for k, v := range snap {
			if isArtifactKey(k) {
				if s, ok := v.(string); ok && s != "" {
					ids = append(ids, s)
				}
			}
		}
		manifest[b.NodeID] = ids
	}
	return manifest
}

// isArtifactKey returns true if the key name suggests it holds an artifact reference.
func isArtifactKey(key string) bool {
	// Match keys containing "artifact_id" as a substring
	for i := 0; i+len("artifact_id") <= len(key); i++ {
		if key[i:i+len("artifact_id")] == "artifact_id" {
			return true
		}
	}
	return false
}

// mergeWaitAll requires all branches to succeed and merges all contexts.
func mergeWaitAll(parent *Context, branches []BranchResult) error {
	// Check that all branches succeeded
	for _, b := range branches {
		if b.Error != nil {
			return fmt.Errorf("branch %q failed with error: %w", b.NodeID, b.Error)
		}
		if b.Outcome != nil && b.Outcome.Status == StatusFail {
			return fmt.Errorf("branch %q failed: %s", b.NodeID, b.Outcome.FailureReason)
		}
	}

	// Merge all branch contexts into parent with logging
	mergeBranchContextsWithLogging(parent, branches)

	// Build and store artifact manifest
	manifest := buildArtifactManifest(branches)
	parent.Set("parallel.artifacts", manifest)

	// Store the merged results
	parent.Set("parallel.results", branches)

	parent.AppendLog(fmt.Sprintf("[merge] completed wait_all merge: %d branch(es) merged", len(branches)))

	return nil
}

// mergeWaitAny requires at least one branch to succeed. Only successful branches
// are merged into the parent context.
func mergeWaitAny(parent *Context, branches []BranchResult) error {
	successBranches := collectSuccessfulBranches(branches)

	if len(successBranches) == 0 {
		return fmt.Errorf("all branches failed in wait_any policy")
	}

	parent.AppendLog(fmt.Sprintf("[merge] wait_any: %d of %d branch(es) succeeded", len(successBranches), len(branches)))

	// Merge only successful branches with logging
	mergeBranchContextsWithLogging(parent, successBranches)

	// Build artifact manifest from successful branches only
	manifest := buildArtifactManifest(successBranches)
	parent.Set("parallel.artifacts", manifest)

	// Store all results (including failures) for visibility
	parent.Set("parallel.results", branches)

	parent.AppendLog(fmt.Sprintf("[merge] completed wait_any merge: %d branch(es) merged", len(successBranches)))

	return nil
}

// mergeKOfN requires at least K branches to succeed. K is read from the parent
// context key "parallel.k_required". If not set, defaults to len(branches).
// Only successful branches are merged.
func mergeKOfN(parent *Context, branches []BranchResult) error {
	kStr := parent.GetString("parallel.k_required", "")
	k := len(branches) // default: require all
	if kStr != "" {
		if n, err := strconv.Atoi(kStr); err == nil && n > 0 {
			k = n
		}
	}

	successBranches := collectSuccessfulBranches(branches)

	if len(successBranches) < k {
		return fmt.Errorf("k_of_n policy requires %d successful branch(es) but only %d of %d succeeded", k, len(successBranches), len(branches))
	}

	parent.AppendLog(fmt.Sprintf("[merge] k_of_n: %d of %d branch(es) succeeded (required: %d)", len(successBranches), len(branches), k))

	// Merge only successful branches with logging
	mergeBranchContextsWithLogging(parent, successBranches)

	// Build artifact manifest from successful branches only
	manifest := buildArtifactManifest(successBranches)
	parent.Set("parallel.artifacts", manifest)

	// Store all results for visibility
	parent.Set("parallel.results", branches)

	parent.AppendLog(fmt.Sprintf("[merge] completed k_of_n merge: %d branch(es) merged", len(successBranches)))

	return nil
}

// mergeQuorum requires a strict majority (>50%) of branches to succeed.
// Only successful branches are merged.
func mergeQuorum(parent *Context, branches []BranchResult) error {
	total := len(branches)
	successBranches := collectSuccessfulBranches(branches)
	required := (total / 2) + 1 // Strict majority: more than half

	if len(successBranches) < required {
		return fmt.Errorf("quorum policy requires strict majority (%d of %d) but only %d succeeded", required, total, len(successBranches))
	}

	parent.AppendLog(fmt.Sprintf("[merge] quorum: %d of %d branch(es) succeeded (required majority: %d)", len(successBranches), total, required))

	// Merge only successful branches with logging
	mergeBranchContextsWithLogging(parent, successBranches)

	// Build artifact manifest from successful branches only
	manifest := buildArtifactManifest(successBranches)
	parent.Set("parallel.artifacts", manifest)

	// Store all results for visibility
	parent.Set("parallel.results", branches)

	parent.AppendLog(fmt.Sprintf("[merge] completed quorum merge: %d branch(es) merged", len(successBranches)))

	return nil
}

// findFanInNode locates the fan-in node (shape=tripleoctagon) that the given
// branch nodes converge to. It searches outgoing edges from each branch
// recursively until a tripleoctagon node is found.
func findFanInNode(graph *Graph, branchIDs []string) *Node {
	visited := make(map[string]bool)
	var queue []string

	// Start with all branch starting nodes
	for _, id := range branchIDs {
		queue = append(queue, id)
	}

	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]

		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true

		node := graph.FindNode(nodeID)
		if node == nil {
			continue
		}

		// Check if this node is a fan-in node
		if node.Attrs != nil && node.Attrs["shape"] == "tripleoctagon" {
			return node
		}

		// Follow outgoing edges
		for _, edge := range graph.OutgoingEdges(nodeID) {
			if !visited[edge.To] {
				queue = append(queue, edge.To)
			}
		}
	}

	return nil
}
