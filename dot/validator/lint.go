// ABOUTME: Unified lint rules for DOT pipeline graphs, merged from mammoth-dot-editor and attractor/validate.go.
// ABOUTME: Provides a single Lint(g) function that runs all structural and semantic checks, returning diagnostics.
package validator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/2389-research/mammoth/dot"
)

// validShapes is the set of recognized DOT shape values for pipeline nodes.
var validShapes = map[string]bool{
	"Mdiamond":      true,
	"Msquare":       true,
	"box":           true,
	"diamond":       true,
	"hexagon":       true,
	"parallelogram": true,
	"component":     true,
	"ellipse":       true,
	"circle":        true,
	"doublecircle":  true,
	"plaintext":     true,
	"record":        true,
	"oval":          true,
}

// validFidelities is the set of recognized fidelity mode strings.
var validFidelities = map[string]bool{
	"compact":        true,
	"standard":       true,
	"detailed":       true,
	"comprehensive":  true,
	"full":           true,
	"truncate":       true,
	"summary:low":    true,
	"summary:medium": true,
	"summary:high":   true,
}

// validRankdirs is the set of valid rankdir attribute values.
var validRankdirs = map[string]bool{
	"LR": true,
	"TB": true,
	"RL": true,
	"BT": true,
}

// knownHandlerTypes lists all recognized node handler type values.
var knownHandlerTypes = map[string]bool{
	"start":              true,
	"exit":               true,
	"codergen":           true,
	"wait.human":         true,
	"conditional":        true,
	"parallel":           true,
	"parallel.fan_in":    true,
	"tool":               true,
	"stack.manager_loop": true,
}

// Lint runs all lint rules on the graph and returns any diagnostics found.
func Lint(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic

	diags = append(diags, checkStartNodes(g)...)
	diags = append(diags, checkExitNodes(g)...)
	diags = append(diags, checkReachability(g)...)
	diags = append(diags, checkStartIncoming(g)...)
	diags = append(diags, checkExitOutgoing(g)...)
	diags = append(diags, checkSelfLoops(g)...)
	diags = append(diags, checkDeadEnds(g)...)
	diags = append(diags, checkShapes(g)...)
	diags = append(diags, checkPrompts(g)...)
	diags = append(diags, checkConditions(g)...)
	diags = append(diags, checkMaxRetries(g)...)
	diags = append(diags, checkGoalGate(g)...)
	diags = append(diags, checkIncompleteOutcomes(g)...)
	diags = append(diags, checkWeights(g)...)
	diags = append(diags, checkFidelity(g)...)
	diags = append(diags, checkRankdir(g)...)
	diags = append(diags, checkGoal(g)...)
	diags = append(diags, checkRetryTarget(g)...)
	diags = append(diags, checkEdgeTargets(g)...)
	diags = append(diags, checkTypeKnown(g)...)
	diags = append(diags, checkGoalGateHasRetry(g)...)

	return diags
}

// isStartNode returns true if the node is a start node.
func isStartNode(n *dot.Node) bool {
	if n.Attrs == nil {
		return false
	}
	if n.Attrs["shape"] == "Mdiamond" {
		return true
	}
	if n.Attrs["node_type"] == "start" || n.Attrs["type"] == "start" {
		return true
	}
	return false
}

// isExitNode returns true if the node is an exit/terminal node.
func isExitNode(n *dot.Node) bool {
	if n.Attrs == nil {
		return false
	}
	if n.Attrs["shape"] == "Msquare" {
		return true
	}
	if n.Attrs["node_type"] == "exit" || n.Attrs["type"] == "exit" {
		return true
	}
	return false
}

// isCodergenNode returns true if the node is a codergen/LLM node.
func isCodergenNode(n *dot.Node) bool {
	if n.Attrs == nil {
		return false
	}
	if n.Attrs["type"] == "codergen" {
		return true
	}
	// shape=box with no explicit type maps to codergen.
	if n.Attrs["shape"] == "box" && n.Attrs["type"] == "" {
		return true
	}
	return false
}

// checkStartNodes verifies exactly one start node (shape=Mdiamond) exists.
func checkStartNodes(g *dot.Graph) []dot.Diagnostic {
	var startIDs []string
	for _, n := range g.Nodes {
		if isStartNode(n) {
			startIDs = append(startIDs, n.ID)
		}
	}

	switch len(startIDs) {
	case 0:
		return []dot.Diagnostic{{
			Severity: "error",
			Message:  "graph has no start node (shape=Mdiamond)",
			Rule:     "start_node",
		}}
	case 1:
		return nil
	default:
		return []dot.Diagnostic{{
			Severity: "error",
			Message:  fmt.Sprintf("graph has %d start nodes, expected exactly 1: %v", len(startIDs), startIDs),
			Rule:     "start_node",
		}}
	}
}

// checkExitNodes verifies at least one exit node (shape=Msquare) exists.
func checkExitNodes(g *dot.Graph) []dot.Diagnostic {
	for _, n := range g.Nodes {
		if isExitNode(n) {
			return nil
		}
	}
	return []dot.Diagnostic{{
		Severity: "error",
		Message:  "graph has no exit node (shape=Msquare)",
		Rule:     "exit_node",
	}}
}

// checkReachability performs BFS from start and flags unreachable nodes.
func checkReachability(g *dot.Graph) []dot.Diagnostic {
	start := g.FindStartNode()
	if start == nil {
		return nil
	}

	visited := make(map[string]bool)
	queue := []string{start.ID}
	visited[start.ID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, e := range g.OutgoingEdges(current) {
			if !visited[e.To] {
				visited[e.To] = true
				queue = append(queue, e.To)
			}
		}
	}

	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		if !visited[id] {
			diags = append(diags, dot.Diagnostic{
				Severity: "error",
				Message:  fmt.Sprintf("node %q is not reachable from start node %q", id, start.ID),
				NodeID:   id,
				Rule:     "reachability",
			})
		}
	}
	return diags
}

// checkStartIncoming verifies no incoming edges to the start node.
func checkStartIncoming(g *dot.Graph) []dot.Diagnostic {
	start := g.FindStartNode()
	if start == nil {
		return nil
	}

	incoming := g.IncomingEdges(start.ID)
	if len(incoming) > 0 {
		return []dot.Diagnostic{{
			Severity: "error",
			Message:  fmt.Sprintf("start node %q has %d incoming edge(s)", start.ID, len(incoming)),
			NodeID:   start.ID,
			Rule:     "start_no_incoming",
		}}
	}
	return nil
}

// checkExitOutgoing verifies no outgoing edges from exit nodes.
func checkExitOutgoing(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, n := range g.Nodes {
		if isExitNode(n) {
			outgoing := g.OutgoingEdges(n.ID)
			if len(outgoing) > 0 {
				diags = append(diags, dot.Diagnostic{
					Severity: "error",
					Message:  fmt.Sprintf("exit node %q has %d outgoing edge(s)", n.ID, len(outgoing)),
					NodeID:   n.ID,
					Rule:     "exit_no_outgoing",
				})
			}
		}
	}
	return diags
}

// checkSelfLoops flags edges where From == To.
func checkSelfLoops(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, e := range g.Edges {
		if e.From == e.To {
			diags = append(diags, dot.Diagnostic{
				Severity: "error",
				Message:  fmt.Sprintf("self-loop on node %q", e.From),
				EdgeID:   e.From + "->" + e.To,
				Rule:     "self_loop",
			})
		}
	}
	return diags
}

// checkDeadEnds flags non-exit nodes with no outgoing edges.
func checkDeadEnds(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || isExitNode(n) {
			continue
		}
		outgoing := g.OutgoingEdges(id)
		if len(outgoing) == 0 {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("non-exit node %q has no outgoing edges (dead end)", id),
				NodeID:   id,
				Rule:     "dead_end",
			})
		}
	}
	return diags
}

// checkShapes validates that node shape attributes use recognized values.
func checkShapes(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || n.Attrs == nil {
			continue
		}
		shape, ok := n.Attrs["shape"]
		if !ok || shape == "" {
			continue
		}
		if !validShapes[shape] {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("node %q has unknown shape %q", id, shape),
				NodeID:   id,
				Rule:     "valid_shape",
			})
		}
	}
	return diags
}

// checkPrompts verifies codergen (box) nodes have a prompt or label attribute.
func checkPrompts(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || !isCodergenNode(n) {
			continue
		}
		hasPrompt := n.Attrs["prompt"] != ""
		hasLabel := n.Attrs["label"] != ""
		if !hasPrompt && !hasLabel {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("codergen node %q has no prompt or label attribute", id),
				NodeID:   id,
				Rule:     "prompt_required",
			})
		}
	}
	return diags
}

// checkConditions validates condition expression syntax on edges.
func checkConditions(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, e := range g.Edges {
		cond, ok := e.Attrs["condition"]
		if !ok || cond == "" {
			continue
		}
		if err := validateConditionExpr(cond); err != nil {
			diags = append(diags, dot.Diagnostic{
				Severity: "error",
				Message:  fmt.Sprintf("invalid condition on edge %s->%s: %v", e.From, e.To, err),
				EdgeID:   e.From + "->" + e.To,
				Rule:     "condition_syntax",
			})
		}
	}
	return diags
}

// validateConditionExpr validates a condition expression string.
// Valid format: clauses separated by &&, each clause is "key = value" or "key != value".
func validateConditionExpr(expr string) error {
	clauses := strings.Split(expr, "&&")
	for _, clause := range clauses {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			return fmt.Errorf("empty clause in condition")
		}

		// Try != first (longer operator).
		if strings.Contains(clause, "!=") {
			parts := strings.SplitN(clause, "!=", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key == "" || val == "" {
				return fmt.Errorf("invalid clause %q: key and value must not be empty", clause)
			}
			continue
		}

		// Try = operator.
		if strings.Contains(clause, "=") {
			parts := strings.SplitN(clause, "=", 2)
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key == "" || val == "" {
				return fmt.Errorf("invalid clause %q: key and value must not be empty", clause)
			}
			continue
		}

		return fmt.Errorf("clause %q has no valid operator (= or !=)", clause)
	}
	return nil
}

// checkMaxRetries validates max_retries is a non-negative integer.
func checkMaxRetries(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || n.Attrs == nil {
			continue
		}
		mr, ok := n.Attrs["max_retries"]
		if !ok || mr == "" {
			continue
		}
		val, err := strconv.Atoi(mr)
		if err != nil {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("node %q has non-integer max_retries %q", id, mr),
				NodeID:   id,
				Rule:     "max_retries",
			})
			continue
		}
		if val < 0 {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("node %q has negative max_retries %q", id, mr),
				NodeID:   id,
				Rule:     "max_retries",
			})
		}
	}
	return diags
}

// checkGoalGate verifies goal_gate is only set on codergen nodes.
func checkGoalGate(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || n.Attrs == nil {
			continue
		}
		if n.Attrs["goal_gate"] != "true" {
			continue
		}
		if !isCodergenNode(n) {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("node %q has goal_gate=true but is not a codergen node", id),
				NodeID:   id,
				Rule:     "goal_gate_codergen",
			})
		}
	}
	return diags
}

// checkIncompleteOutcomes verifies diamond (conditional) nodes have both success and fail edges.
func checkIncompleteOutcomes(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || n.Attrs == nil {
			continue
		}
		if n.Attrs["shape"] != "diamond" {
			continue
		}
		outgoing := g.OutgoingEdges(id)
		hasSuccess := false
		hasFail := false
		for _, e := range outgoing {
			label := e.Attrs["label"]
			if label == "success" {
				hasSuccess = true
			}
			if label == "fail" {
				hasFail = true
			}
		}
		if !hasSuccess || !hasFail {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("diamond node %q is missing success and/or fail outcome edges", id),
				NodeID:   id,
				Rule:     "incomplete_outcomes",
			})
		}
	}
	return diags
}

// checkWeights validates edge weight attributes are positive integers.
func checkWeights(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, e := range g.Edges {
		w, ok := e.Attrs["weight"]
		if !ok || w == "" {
			continue
		}
		val, err := strconv.Atoi(w)
		if err != nil {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("edge %s->%s has non-integer weight %q", e.From, e.To, w),
				EdgeID:   e.From + "->" + e.To,
				Rule:     "valid_weight",
			})
			continue
		}
		if val <= 0 {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("edge %s->%s has non-positive weight %q (must be > 0)", e.From, e.To, w),
				EdgeID:   e.From + "->" + e.To,
				Rule:     "valid_weight",
			})
		}
	}
	return diags
}

// checkFidelity validates fidelity attribute values on nodes.
func checkFidelity(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || n.Attrs == nil {
			continue
		}
		fid, ok := n.Attrs["fidelity"]
		if !ok || fid == "" {
			continue
		}
		if !validFidelities[fid] {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("node %q has invalid fidelity mode %q", id, fid),
				NodeID:   id,
				Rule:     "valid_fidelity",
			})
		}
	}
	return diags
}

// checkRankdir validates the graph-level rankdir attribute.
func checkRankdir(g *dot.Graph) []dot.Diagnostic {
	if g.Attrs == nil {
		return nil
	}
	rd, ok := g.Attrs["rankdir"]
	if !ok || rd == "" {
		return nil
	}
	if !validRankdirs[rd] {
		return []dot.Diagnostic{{
			Severity: "warning",
			Message:  fmt.Sprintf("graph has invalid rankdir %q", rd),
			Rule:     "valid_rankdir",
		}}
	}
	return nil
}

// checkGoal verifies the graph has a goal attribute.
func checkGoal(g *dot.Graph) []dot.Diagnostic {
	if g.Attrs == nil || g.Attrs["goal"] == "" {
		return []dot.Diagnostic{{
			Severity: "warning",
			Message:  "graph has no goal attribute",
			Rule:     "graph_goal",
		}}
	}
	return nil
}

// checkRetryTarget verifies retry_target references an existing node.
func checkRetryTarget(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || n.Attrs == nil {
			continue
		}
		target, ok := n.Attrs["retry_target"]
		if !ok || target == "" {
			continue
		}
		if g.FindNode(target) == nil {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("node %q has retry_target %q which does not exist", id, target),
				NodeID:   id,
				Rule:     "retry_target",
			})
		}
	}
	return diags
}

// checkEdgeTargets verifies every edge references existing nodes.
func checkEdgeTargets(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, e := range g.Edges {
		if g.FindNode(e.From) == nil {
			diags = append(diags, dot.Diagnostic{
				Severity: "error",
				Message:  fmt.Sprintf("edge source %q does not exist", e.From),
				EdgeID:   e.From + "->" + e.To,
				Rule:     "edge_target_exists",
			})
		}
		if g.FindNode(e.To) == nil {
			diags = append(diags, dot.Diagnostic{
				Severity: "error",
				Message:  fmt.Sprintf("edge target %q does not exist", e.To),
				EdgeID:   e.From + "->" + e.To,
				Rule:     "edge_target_exists",
			})
		}
	}
	return diags
}

// checkTypeKnown verifies node type values are recognized handler types.
func checkTypeKnown(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || n.Attrs == nil {
			continue
		}
		typ, ok := n.Attrs["type"]
		if !ok || typ == "" {
			continue
		}
		if !knownHandlerTypes[typ] {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("node %q has unknown type %q", id, typ),
				NodeID:   id,
				Rule:     "type_known",
			})
		}
	}
	return diags
}

// checkGoalGateHasRetry verifies goal_gate=true nodes have a retry_target.
func checkGoalGateHasRetry(g *dot.Graph) []dot.Diagnostic {
	var diags []dot.Diagnostic
	for _, id := range g.NodeIDs() {
		n := g.FindNode(id)
		if n == nil || n.Attrs == nil {
			continue
		}
		if n.Attrs["goal_gate"] != "true" {
			continue
		}
		if n.Attrs["retry_target"] == "" {
			diags = append(diags, dot.Diagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("node %q has goal_gate=true but no retry_target", id),
				NodeID:   id,
				Rule:     "goal_gate_has_retry",
			})
		}
	}
	return diags
}
