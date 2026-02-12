// ABOUTME: Pipeline validation rules that check graph structure and node/edge attributes for correctness.
// ABOUTME: Provides a pluggable LintRule interface, built-in rules, Validate, and ValidateOrError functions.
package attractor

import (
	"fmt"
	"strings"
)

// Severity represents diagnostic severity level.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
	SeverityInfo
)

// String returns a human-readable name for the severity level.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "ERROR"
	case SeverityWarning:
		return "WARNING"
	case SeverityInfo:
		return "INFO"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(s))
	}
}

// Diagnostic represents a validation finding.
type Diagnostic struct {
	Rule     string
	Severity Severity
	Message  string
	NodeID   string     // optional
	Edge     *[2]string // optional (from, to)
	Fix      string     // optional suggested fix
}

// LintRule is the interface for validation rules.
type LintRule interface {
	Name() string
	Apply(g *Graph) []Diagnostic
}

// knownHandlerTypes lists all recognized handler type values.
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

// validFidelityModes is defined in fidelity.go as the authoritative source.

// builtinRules returns all built-in lint rules.
func builtinRules() []LintRule {
	return []LintRule{
		&startNodeRule{},
		&terminalNodeRule{},
		&reachabilityRule{},
		&edgeTargetExistsRule{},
		&startNoIncomingRule{},
		&exitNoOutgoingRule{},
		&conditionSyntaxRule{},
		&typeKnownRule{},
		&fidelityValidRule{},
		&retryTargetExistsRule{},
		&goalGateHasRetryRule{},
		&promptOnLLMNodesRule{},
	}
}

// Validate runs all built-in lint rules plus any extra rules on the graph.
func Validate(g *Graph, extraRules ...LintRule) []Diagnostic {
	var diags []Diagnostic

	rules := builtinRules()
	rules = append(rules, extraRules...)

	for _, rule := range rules {
		diags = append(diags, rule.Apply(g)...)
	}

	return diags
}

// ValidateOrError runs validation and returns an error if any ERROR-severity diagnostics exist.
func ValidateOrError(g *Graph, extraRules ...LintRule) ([]Diagnostic, error) {
	diags := Validate(g, extraRules...)

	var errCount int
	for _, d := range diags {
		if d.Severity == SeverityError {
			errCount++
		}
	}

	if errCount > 0 {
		return diags, fmt.Errorf("pipeline validation failed with %d error(s)", errCount)
	}

	return diags, nil
}

// --- Built-in lint rules ---

// isStartNode returns true if the node is a start node.
// Recognized via shape=Mdiamond, node_type=start, or type=start.
func isStartNode(n *Node) bool {
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

// startNodeRule checks that exactly one start node exists.
// Recognized via shape=Mdiamond, node_type=start, or type=start.
type startNodeRule struct{}

func (r *startNodeRule) Name() string { return "start_node" }

func (r *startNodeRule) Apply(g *Graph) []Diagnostic {
	var startNodes []string
	for _, n := range g.Nodes {
		if isStartNode(n) {
			startNodes = append(startNodes, n.ID)
		}
	}

	switch len(startNodes) {
	case 0:
		return []Diagnostic{{
			Rule:     r.Name(),
			Severity: SeverityError,
			Message:  "graph has no start node (shape=Mdiamond)",
			Fix:      "add a node with shape=Mdiamond",
		}}
	case 1:
		return nil
	default:
		return []Diagnostic{{
			Rule:     r.Name(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("graph has %d start nodes (shape=Mdiamond), expected exactly 1: %v", len(startNodes), startNodes),
			Fix:      "ensure only one node has shape=Mdiamond",
		}}
	}
}

// terminalNodeRule checks that at least one terminal node exists.
// Recognized via shape=Msquare, node_type=exit, or type=exit.
type terminalNodeRule struct{}

func (r *terminalNodeRule) Name() string { return "terminal_node" }

func (r *terminalNodeRule) Apply(g *Graph) []Diagnostic {
	for _, n := range g.Nodes {
		if isTerminal(n) {
			return nil
		}
	}
	return []Diagnostic{{
		Rule:     r.Name(),
		Severity: SeverityError,
		Message:  "graph has no terminal node (shape=Msquare)",
		Fix:      "add a node with shape=Msquare",
	}}
}

// reachabilityRule checks that all nodes are reachable from the start node via BFS.
type reachabilityRule struct{}

func (r *reachabilityRule) Name() string { return "reachability" }

func (r *reachabilityRule) Apply(g *Graph) []Diagnostic {
	start := g.FindStartNode()
	if start == nil {
		// Can't check reachability without a start node; start_node rule handles this.
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

	var diags []Diagnostic
	for _, id := range g.NodeIDs() {
		if !visited[id] {
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("node %q is not reachable from start node %q", id, start.ID),
				NodeID:   id,
				Fix:      fmt.Sprintf("add an edge path from start to %q", id),
			})
		}
	}

	return diags
}

// edgeTargetExistsRule checks that every edge references existing nodes.
type edgeTargetExistsRule struct{}

func (r *edgeTargetExistsRule) Name() string { return "edge_target_exists" }

func (r *edgeTargetExistsRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, e := range g.Edges {
		if g.FindNode(e.From) == nil {
			edge := [2]string{e.From, e.To}
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("edge source %q does not exist", e.From),
				Edge:     &edge,
				Fix:      fmt.Sprintf("add node %q or fix the edge source", e.From),
			})
		}
		if g.FindNode(e.To) == nil {
			edge := [2]string{e.From, e.To}
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("edge target %q does not exist", e.To),
				Edge:     &edge,
				Fix:      fmt.Sprintf("add node %q or fix the edge target", e.To),
			})
		}
	}
	return diags
}

// startNoIncomingRule checks that the start node has no incoming edges.
type startNoIncomingRule struct{}

func (r *startNoIncomingRule) Name() string { return "start_no_incoming" }

func (r *startNoIncomingRule) Apply(g *Graph) []Diagnostic {
	start := g.FindStartNode()
	if start == nil {
		return nil
	}

	incoming := g.IncomingEdges(start.ID)
	if len(incoming) > 0 {
		return []Diagnostic{{
			Rule:     r.Name(),
			Severity: SeverityError,
			Message:  fmt.Sprintf("start node %q has %d incoming edge(s)", start.ID, len(incoming)),
			NodeID:   start.ID,
			Fix:      "remove incoming edges to the start node",
		}}
	}
	return nil
}

// exitNoOutgoingRule checks that exit nodes have no outgoing edges.
type exitNoOutgoingRule struct{}

func (r *exitNoOutgoingRule) Name() string { return "exit_no_outgoing" }

func (r *exitNoOutgoingRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		if isTerminal(n) {
			outgoing := g.OutgoingEdges(n.ID)
			if len(outgoing) > 0 {
				diags = append(diags, Diagnostic{
					Rule:     r.Name(),
					Severity: SeverityError,
					Message:  fmt.Sprintf("exit node %q has %d outgoing edge(s)", n.ID, len(outgoing)),
					NodeID:   n.ID,
					Fix:      "remove outgoing edges from the exit node",
				})
			}
		}
	}
	return diags
}

// conditionSyntaxRule checks that edge condition expressions parse correctly.
// Valid syntax: key = value, key != value, joined by &&.
type conditionSyntaxRule struct{}

func (r *conditionSyntaxRule) Name() string { return "condition_syntax" }

func (r *conditionSyntaxRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, e := range g.Edges {
		cond, ok := e.Attrs["condition"]
		if !ok || cond == "" {
			continue
		}
		if err := validateConditionExpr(cond); err != nil {
			edge := [2]string{e.From, e.To}
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityError,
				Message:  fmt.Sprintf("invalid condition on edge %s->%s: %v", e.From, e.To, err),
				Edge:     &edge,
				Fix:      "use format: key = value or key != value, joined by &&",
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

// typeKnownRule checks that node type values are recognized handler types.
type typeKnownRule struct{}

func (r *typeKnownRule) Name() string { return "type_known" }

func (r *typeKnownRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		typ, ok := n.Attrs["type"]
		if !ok || typ == "" {
			continue
		}
		if !knownHandlerTypes[typ] {
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("node %q has unknown type %q", n.ID, typ),
				NodeID:   n.ID,
				Fix:      "use a recognized handler type: start, exit, codergen, wait.human, conditional, parallel, parallel.fan_in, tool, stack.manager_loop",
			})
		}
	}
	return diags
}

// fidelityValidRule checks that fidelity mode values are valid.
type fidelityValidRule struct{}

func (r *fidelityValidRule) Name() string { return "fidelity_valid" }

func (r *fidelityValidRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		fid, ok := n.Attrs["fidelity"]
		if !ok || fid == "" {
			continue
		}
		if !validFidelityModes[fid] {
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("node %q has invalid fidelity mode %q", n.ID, fid),
				NodeID:   n.ID,
				Fix:      "use a valid fidelity mode: full, truncate, compact, summary:low, summary:medium, summary:high",
			})
		}
	}
	return diags
}

// retryTargetExistsRule checks that retry_target references existing nodes.
type retryTargetExistsRule struct{}

func (r *retryTargetExistsRule) Name() string { return "retry_target_exists" }

func (r *retryTargetExistsRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		target, ok := n.Attrs["retry_target"]
		if !ok || target == "" {
			continue
		}
		if g.FindNode(target) == nil {
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("node %q has retry_target %q which does not exist", n.ID, target),
				NodeID:   n.ID,
				Fix:      fmt.Sprintf("add node %q or fix the retry_target value", target),
			})
		}
	}
	return diags
}

// goalGateHasRetryRule checks that goal_gate=true nodes have a retry_target.
type goalGateHasRetryRule struct{}

func (r *goalGateHasRetryRule) Name() string { return "goal_gate_has_retry" }

func (r *goalGateHasRetryRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		if n.Attrs["goal_gate"] != "true" {
			continue
		}
		if n.Attrs["retry_target"] == "" {
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("node %q has goal_gate=true but no retry_target", n.ID),
				NodeID:   n.ID,
				Fix:      "add a retry_target attribute pointing to a valid node",
			})
		}
	}
	return diags
}

// promptOnLLMNodesRule checks that codergen nodes have a prompt or label attribute.
type promptOnLLMNodesRule struct{}

func (r *promptOnLLMNodesRule) Name() string { return "prompt_on_llm_nodes" }

func (r *promptOnLLMNodesRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		// Check if this is a codergen node by explicit type or by shape mapping.
		isCodergen := n.Attrs["type"] == "codergen"
		if !isCodergen && n.Attrs["shape"] == "box" && n.Attrs["type"] == "" {
			// Shape=box maps to codergen when no explicit type is set.
			isCodergen = true
		}
		if !isCodergen {
			continue
		}

		hasPrompt := n.Attrs["prompt"] != ""
		hasLabel := n.Attrs["label"] != ""
		if !hasPrompt && !hasLabel {
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("codergen node %q has no prompt or label attribute", n.ID),
				NodeID:   n.ID,
				Fix:      "add a prompt or label attribute to describe what this node does",
			})
		}
	}
	return diags
}
