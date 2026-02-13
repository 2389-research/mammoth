// ABOUTME: Table-driven tests for the unified DOT graph lint rules covering structure, attributes, and semantics.
// ABOUTME: Exercises all 24 check functions merged from mammoth-dot-editor and attractor/validate.go.
package validator

import (
	"testing"

	"github.com/2389-research/mammoth/dot"
)

// validGraph returns a minimal valid pipeline: start -> work -> exit.
func validGraph() *dot.Graph {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test the validator"},
	}
	return g
}

// hasDiag checks if any diagnostic matches the given rule and severity.
func hasDiag(diags []dot.Diagnostic, rule, severity string) bool {
	for _, d := range diags {
		if d.Rule == rule && d.Severity == severity {
			return true
		}
	}
	return false
}

// countDiags counts diagnostics matching the given rule.
func countDiags(diags []dot.Diagnostic, rule string) int {
	n := 0
	for _, d := range diags {
		if d.Rule == rule {
			n++
		}
	}
	return n
}

func TestLint_ValidGraph(t *testing.T) {
	g := validGraph()
	diags := Lint(g)

	for _, d := range diags {
		if d.Severity == "error" {
			t.Errorf("unexpected error diagnostic: rule=%s message=%s", d.Rule, d.Message)
		}
	}
}

func TestLint_MissingStartNode(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"work": {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"exit": {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "start_node", "error") {
		t.Errorf("expected start_node error, got: %v", diags)
	}
}

func TestLint_MultipleStartNodes(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"s1":   {ID: "s1", Attrs: map[string]string{"shape": "Mdiamond"}},
			"s2":   {ID: "s2", Attrs: map[string]string{"shape": "Mdiamond"}},
			"exit": {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "s1", To: "exit", Attrs: map[string]string{}},
			{From: "s2", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "start_node", "error") {
		t.Errorf("expected start_node error for multiple starts, got: %v", diags)
	}
}

func TestLint_MissingExitNode(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "exit_node", "error") {
		t.Errorf("expected exit_node error, got: %v", diags)
	}
}

func TestLint_UnreachableNode(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":   {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"island": {ID: "island", Attrs: map[string]string{"shape": "box"}},
			"exit":   {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "reachability", "error") {
		t.Errorf("expected reachability error for island node, got: %v", diags)
	}

	// Verify the diagnostic references the unreachable node.
	found := false
	for _, d := range diags {
		if d.Rule == "reachability" && d.NodeID == "island" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reachability diagnostic with NodeID=island")
	}
}

func TestLint_StartWithIncoming(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
			{From: "work", To: "start", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "start_no_incoming", "error") {
		t.Errorf("expected start_no_incoming error, got: %v", diags)
	}
}

func TestLint_ExitWithOutgoing(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
			{From: "exit", To: "work", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "exit_no_outgoing", "error") {
		t.Errorf("expected exit_no_outgoing error, got: %v", diags)
	}
}

func TestLint_SelfLoop(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "self_loop", "error") {
		t.Errorf("expected self_loop error, got: %v", diags)
	}
}

func TestLint_DeadEndNonExit(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start":   {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":    {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"deadend": {ID: "deadend", Attrs: map[string]string{"shape": "box"}},
			"exit":    {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "start", To: "deadend", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
			// deadend has no outgoing edges and is not exit
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "dead_end", "warning") {
		t.Errorf("expected dead_end warning for deadend node, got: %v", diags)
	}
}

func TestLint_UnknownShape(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "banana", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "valid_shape", "warning") {
		t.Errorf("expected valid_shape warning for banana shape, got: %v", diags)
	}
}

func TestLint_EmptyPromptOnCodergen(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "prompt_required", "warning") {
		t.Errorf("expected prompt_required warning for codergen without prompt, got: %v", diags)
	}
}

func TestLint_InvalidConditionSyntax(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{"condition": "status >> done"}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "condition_syntax", "error") {
		t.Errorf("expected condition_syntax error, got: %v", diags)
	}

	// Valid condition should not trigger error.
	g2 := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{"condition": "status = done && quality != bad"}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}
	diags2 := Lint(g2)
	if hasDiag(diags2, "condition_syntax", "error") {
		t.Errorf("valid condition should not trigger error, got: %v", diags2)
	}
}

func TestLint_InvalidMaxRetries(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff", "max_retries": "-1"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "max_retries", "warning") {
		t.Errorf("expected max_retries warning for negative value, got: %v", diags)
	}

	// Non-integer max_retries
	g2 := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff", "max_retries": "abc"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}
	diags2 := Lint(g2)
	if !hasDiag(diags2, "max_retries", "warning") {
		t.Errorf("expected max_retries warning for non-integer value, got: %v", diags2)
	}
}

func TestLint_GoalGateOnNonCodergen(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "diamond", "goal_gate": "true"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{"label": "success"}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "goal_gate_codergen", "warning") {
		t.Errorf("expected goal_gate_codergen warning, got: %v", diags)
	}
}

func TestLint_IncompleteOutcomes(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start":    {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"decision": {ID: "decision", Attrs: map[string]string{"shape": "diamond"}},
			"work":     {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":     {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "decision", Attrs: map[string]string{}},
			{From: "decision", To: "work", Attrs: map[string]string{"label": "success"}},
			// Missing fail edge
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "incomplete_outcomes", "warning") {
		t.Errorf("expected incomplete_outcomes warning for diamond missing fail edge, got: %v", diags)
	}
}

func TestLint_InvalidWeight(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{"weight": "0"}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "valid_weight", "warning") {
		t.Errorf("expected valid_weight warning for zero weight, got: %v", diags)
	}

	// Negative weight
	g2 := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{"weight": "-5"}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}
	diags2 := Lint(g2)
	if !hasDiag(diags2, "valid_weight", "warning") {
		t.Errorf("expected valid_weight warning for negative weight, got: %v", diags2)
	}

	// Non-integer weight
	g3 := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{"weight": "abc"}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}
	diags3 := Lint(g3)
	if !hasDiag(diags3, "valid_weight", "warning") {
		t.Errorf("expected valid_weight warning for non-integer weight, got: %v", diags3)
	}
}

func TestLint_InvalidFidelity(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff", "fidelity": "ultra_mega"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "valid_fidelity", "warning") {
		t.Errorf("expected valid_fidelity warning, got: %v", diags)
	}
}

func TestLint_InvalidRankdir(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test", "rankdir": "DIAGONAL"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "valid_rankdir", "warning") {
		t.Errorf("expected valid_rankdir warning, got: %v", diags)
	}
}

func TestLint_MissingGoal(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{},
	}

	diags := Lint(g)
	if !hasDiag(diags, "graph_goal", "warning") {
		t.Errorf("expected graph_goal warning, got: %v", diags)
	}
}

func TestLint_InvalidRetryTarget(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff", "retry_target": "phantom_node"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "retry_target", "warning") {
		t.Errorf("expected retry_target warning, got: %v", diags)
	}
}

func TestLint_EdgeReferencingNonExistentNode(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "ghost", Attrs: map[string]string{}},
			{From: "start", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "edge_target_exists", "error") {
		t.Errorf("expected edge_target_exists error, got: %v", diags)
	}
}

func TestLint_UnknownHandlerType(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "type": "start"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "banana_launcher", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare", "type": "exit"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "type_known", "warning") {
		t.Errorf("expected type_known warning for banana_launcher, got: %v", diags)
	}
}

func TestLint_GoalGateWithoutRetryTarget(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff", "goal_gate": "true"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "goal_gate_has_retry", "warning") {
		t.Errorf("expected goal_gate_has_retry warning, got: %v", diags)
	}

	// With retry_target, should not trigger.
	g2 := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff", "goal_gate": "true", "retry_target": "work"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}
	diags2 := Lint(g2)
	if hasDiag(diags2, "goal_gate_has_retry", "warning") {
		t.Errorf("goal_gate with retry_target should not trigger warning, got: %v", diags2)
	}
}

func TestLint_CodergenWithoutPromptOrLabel(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "codergen"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "prompt_required", "warning") {
		t.Errorf("expected prompt_required warning for codergen without prompt/label, got: %v", diags)
	}

	// With label should be OK.
	g2 := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "codergen", "label": "Generate Code"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}
	diags2 := Lint(g2)
	if hasDiag(diags2, "prompt_required", "warning") {
		t.Errorf("codergen with label should not trigger prompt_required warning, got: %v", diags2)
	}
}

func TestLint_EdgeFromNonExistent(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "exit", Attrs: map[string]string{}},
			{From: "phantom", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if !hasDiag(diags, "edge_target_exists", "error") {
		t.Errorf("expected edge_target_exists error for phantom source, got: %v", diags)
	}
}

func TestLint_ValidFidelityValues(t *testing.T) {
	validFidelities := []string{
		"compact", "standard", "detailed", "comprehensive", "full",
		"truncate", "summary:low", "summary:medium", "summary:high",
	}

	for _, fid := range validFidelities {
		g := &dot.Graph{
			Nodes: map[string]*dot.Node{
				"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
				"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff", "fidelity": fid}},
				"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
			},
			Edges: []*dot.Edge{
				{From: "start", To: "work", Attrs: map[string]string{}},
				{From: "work", To: "exit", Attrs: map[string]string{}},
			},
			Attrs: map[string]string{"goal": "test"},
		}
		diags := Lint(g)
		if hasDiag(diags, "valid_fidelity", "warning") {
			t.Errorf("fidelity=%q should be valid, got warning: %v", fid, diags)
		}
	}
}

func TestLint_ValidShapes(t *testing.T) {
	validShapes := []string{
		"Mdiamond", "Msquare", "box", "diamond", "hexagon",
		"parallelogram", "component", "ellipse", "circle",
		"doublecircle", "plaintext", "record", "oval",
	}

	for _, shape := range validShapes {
		g := &dot.Graph{
			Nodes: map[string]*dot.Node{
				"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
				"work":  {ID: "work", Attrs: map[string]string{"shape": shape, "prompt": "do stuff"}},
				"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
			},
			Edges: []*dot.Edge{
				{From: "start", To: "work", Attrs: map[string]string{}},
				{From: "work", To: "exit", Attrs: map[string]string{}},
			},
			Attrs: map[string]string{"goal": "test"},
		}
		diags := Lint(g)
		if hasDiag(diags, "valid_shape", "warning") {
			t.Errorf("shape=%q should be valid, got warning: %v", shape, diags)
		}
	}
}

func TestLint_ValidRankdirs(t *testing.T) {
	for _, rd := range []string{"LR", "TB", "RL", "BT"} {
		g := &dot.Graph{
			Nodes: map[string]*dot.Node{
				"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
				"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
				"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
			},
			Edges: []*dot.Edge{
				{From: "start", To: "work", Attrs: map[string]string{}},
				{From: "work", To: "exit", Attrs: map[string]string{}},
			},
			Attrs: map[string]string{"goal": "test", "rankdir": rd},
		}
		diags := Lint(g)
		if hasDiag(diags, "valid_rankdir", "warning") {
			t.Errorf("rankdir=%q should be valid, got warning: %v", rd, diags)
		}
	}
}

func TestLint_DiamondWithBothOutcomes(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start":    {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"decision": {ID: "decision", Attrs: map[string]string{"shape": "diamond"}},
			"work":     {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff"}},
			"fallback": {ID: "fallback", Attrs: map[string]string{"shape": "box", "prompt": "handle fail"}},
			"exit":     {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "decision", Attrs: map[string]string{}},
			{From: "decision", To: "work", Attrs: map[string]string{"label": "success"}},
			{From: "decision", To: "fallback", Attrs: map[string]string{"label": "fail"}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
			{From: "fallback", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if hasDiag(diags, "incomplete_outcomes", "warning") {
		t.Errorf("diamond with both outcomes should not trigger warning, got: %v", diags)
	}
}

func TestLint_ValidMaxRetries(t *testing.T) {
	g := &dot.Graph{
		Nodes: map[string]*dot.Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "prompt": "do stuff", "max_retries": "3"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*dot.Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
		Attrs: map[string]string{"goal": "test"},
	}

	diags := Lint(g)
	if hasDiag(diags, "max_retries", "warning") {
		t.Errorf("valid max_retries should not trigger warning, got: %v", diags)
	}
}

func TestLint_KnownTypes(t *testing.T) {
	knownTypes := []string{
		"start", "exit", "codergen", "wait.human", "conditional",
		"parallel", "parallel.fan_in", "tool", "stack.manager_loop",
	}

	for _, typ := range knownTypes {
		g := &dot.Graph{
			Nodes: map[string]*dot.Node{
				"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "type": "start"}},
				"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": typ, "prompt": "do stuff"}},
				"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare", "type": "exit"}},
			},
			Edges: []*dot.Edge{
				{From: "start", To: "work", Attrs: map[string]string{}},
				{From: "work", To: "exit", Attrs: map[string]string{}},
			},
			Attrs: map[string]string{"goal": "test"},
		}
		diags := Lint(g)
		if hasDiag(diags, "type_known", "warning") {
			t.Errorf("type=%q should be known, got warning: %v", typ, diags)
		}
	}
}
