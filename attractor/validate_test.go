// ABOUTME: Tests for pipeline validation rules that check graph structure and node/edge attributes.
// ABOUTME: Covers all built-in lint rules plus custom rule extension via the LintRule interface.
package attractor

import (
	"testing"
)

// helper builds a minimal valid pipeline graph: start -> work -> exit
func validPipelineGraph() *Graph {
	return &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "type": "start"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "codergen", "prompt": "do stuff"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare", "type": "exit"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}
}

// hasDiagnostic checks if any diagnostic matches the given rule and severity.
func hasDiagnostic(diags []Diagnostic, rule string, sev Severity) bool {
	for _, d := range diags {
		if d.Rule == rule && d.Severity == sev {
			return true
		}
	}
	return false
}

// countDiagnostics counts diagnostics matching the given rule.
func countDiagnostics(diags []Diagnostic, rule string) int {
	n := 0
	for _, d := range diags {
		if d.Rule == rule {
			n++
		}
	}
	return n
}

func TestValidate_ValidPipeline(t *testing.T) {
	g := validPipelineGraph()
	diags := Validate(g)

	// A valid pipeline should produce zero ERROR diagnostics.
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("unexpected ERROR diagnostic: rule=%s message=%s", d.Rule, d.Message)
		}
	}
}

func TestValidate_NoStartNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"work": {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"exit": {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "start_node", SeverityError) {
		t.Errorf("expected start_node ERROR diagnostic, got: %v", diags)
	}
}

func TestValidate_MultipleStartNodes(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start1": {ID: "start1", Attrs: map[string]string{"shape": "Mdiamond"}},
			"start2": {ID: "start2", Attrs: map[string]string{"shape": "Mdiamond"}},
			"exit":   {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start1", To: "exit", Attrs: map[string]string{}},
			{From: "start2", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "start_node", SeverityError) {
		t.Errorf("expected start_node ERROR diagnostic for multiple start nodes, got: %v", diags)
	}
}

func TestValidate_NoTerminalNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "terminal_node", SeverityError) {
		t.Errorf("expected terminal_node ERROR diagnostic, got: %v", diags)
	}
}

func TestValidate_UnreachableNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":   {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"island": {ID: "island", Attrs: map[string]string{"shape": "box"}},
			"exit":   {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "reachability", SeverityError) {
		t.Errorf("expected reachability ERROR diagnostic for island node, got: %v", diags)
	}
	// The diagnostic should mention the unreachable node.
	found := false
	for _, d := range diags {
		if d.Rule == "reachability" && d.NodeID == "island" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reachability diagnostic with NodeID=island, got: %v", diags)
	}
}

func TestValidate_EdgeTargetMissing(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "ghost", Attrs: map[string]string{}},
			{From: "start", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "edge_target_exists", SeverityError) {
		t.Errorf("expected edge_target_exists ERROR diagnostic, got: %v", diags)
	}
}

func TestValidate_StartHasIncoming(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
			{From: "work", To: "start", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "start_no_incoming", SeverityError) {
		t.Errorf("expected start_no_incoming ERROR diagnostic, got: %v", diags)
	}
}

func TestValidate_ExitHasOutgoing(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
			{From: "exit", To: "work", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "exit_no_outgoing", SeverityError) {
		t.Errorf("expected exit_no_outgoing ERROR diagnostic, got: %v", diags)
	}
}

func TestValidate_InvalidConditionSyntax(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{"condition": "status >> done"}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "condition_syntax", SeverityError) {
		t.Errorf("expected condition_syntax ERROR diagnostic, got: %v", diags)
	}

	// Valid conditions should not trigger the rule.
	g2 := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{"condition": "status = done && quality != bad"}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}
	diags2 := Validate(g2)
	if hasDiagnostic(diags2, "condition_syntax", SeverityError) {
		t.Errorf("valid condition triggered condition_syntax ERROR: %v", diags2)
	}
}

func TestValidate_UnknownType(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond", "type": "start"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "banana_launcher"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare", "type": "exit"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "type_known", SeverityWarning) {
		t.Errorf("expected type_known WARNING diagnostic for unknown type, got: %v", diags)
	}
}

func TestValidate_InvalidFidelity(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "fidelity": "ultra_mega"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "fidelity_valid", SeverityWarning) {
		t.Errorf("expected fidelity_valid WARNING diagnostic, got: %v", diags)
	}
}

func TestValidate_RetryTargetMissing(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "retry_target": "phantom_node"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "retry_target_exists", SeverityWarning) {
		t.Errorf("expected retry_target_exists WARNING diagnostic, got: %v", diags)
	}
}

func TestValidate_GoalGateNoRetry(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "goal_gate": "true"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "goal_gate_has_retry", SeverityWarning) {
		t.Errorf("expected goal_gate_has_retry WARNING diagnostic, got: %v", diags)
	}

	// A goal_gate node with a retry_target should not trigger the warning.
	g2 := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "goal_gate": "true", "retry_target": "work"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}
	diags2 := Validate(g2)
	if hasDiagnostic(diags2, "goal_gate_has_retry", SeverityWarning) {
		t.Errorf("goal_gate node with retry_target should not trigger warning, got: %v", diags2)
	}
}

func TestValidate_PromptMissing(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "codergen"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g)
	if !hasDiagnostic(diags, "prompt_on_llm_nodes", SeverityWarning) {
		t.Errorf("expected prompt_on_llm_nodes WARNING diagnostic, got: %v", diags)
	}

	// A codergen node with a prompt should not trigger the warning.
	g2 := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "codergen", "prompt": "write code"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}
	diags2 := Validate(g2)
	if hasDiagnostic(diags2, "prompt_on_llm_nodes", SeverityWarning) {
		t.Errorf("codergen node with prompt should not trigger warning, got: %v", diags2)
	}

	// A codergen node with a label (but no prompt) should also be ok.
	g3 := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "codergen", "label": "Generate Code"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}
	diags3 := Validate(g3)
	if hasDiagnostic(diags3, "prompt_on_llm_nodes", SeverityWarning) {
		t.Errorf("codergen node with label should not trigger warning, got: %v", diags3)
	}
}

func TestValidateOrError_ReturnsError(t *testing.T) {
	// Graph with no start node should produce an error.
	g := &Graph{
		Nodes: map[string]*Node{
			"work": {ID: "work", Attrs: map[string]string{"shape": "box"}},
			"exit": {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags, err := ValidateOrError(g)
	if err == nil {
		t.Errorf("expected error from ValidateOrError, got nil")
	}
	if len(diags) == 0 {
		t.Errorf("expected diagnostics from ValidateOrError, got none")
	}
}

func TestValidateOrError_NoError(t *testing.T) {
	// Graph with only warnings should not produce an error.
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "type": "banana_launcher"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags, err := ValidateOrError(g)
	if err != nil {
		t.Errorf("expected nil error from ValidateOrError (only warnings), got: %v", err)
	}
	// Should still have warning diagnostics.
	if !hasDiagnostic(diags, "type_known", SeverityWarning) {
		t.Errorf("expected type_known WARNING, got: %v", diags)
	}
}

// testCustomRule is a custom lint rule used only in tests.
type testCustomRule struct{}

func (r *testCustomRule) Name() string { return "custom_test_rule" }

func (r *testCustomRule) Apply(g *Graph) []Diagnostic {
	var diags []Diagnostic
	for _, n := range g.Nodes {
		if n.Attrs["color"] == "red" {
			diags = append(diags, Diagnostic{
				Rule:     r.Name(),
				Severity: SeverityInfo,
				Message:  "node has red color",
				NodeID:   n.ID,
			})
		}
	}
	return diags
}

func TestValidate_CustomRule(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"shape": "box", "color": "red"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "work", Attrs: map[string]string{}},
			{From: "work", To: "exit", Attrs: map[string]string{}},
		},
	}

	diags := Validate(g, &testCustomRule{})
	if !hasDiagnostic(diags, "custom_test_rule", SeverityInfo) {
		t.Errorf("expected custom_test_rule INFO diagnostic, got: %v", diags)
	}
}
