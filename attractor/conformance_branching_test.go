// ABOUTME: Tests that the conformance_test.dot pipeline parses, validates, and has correct structure.
// ABOUTME: Verifies the attractor-independent conformance test exercises the expected spec features.
package attractor

import (
	"os"
	"testing"

	"github.com/2389-research/mammoth/dot"
)

func TestConformanceTestDOT_ParsesCleanly(t *testing.T) {
	data, err := os.ReadFile("../examples/conformance_test.dot")
	if err != nil {
		t.Fatalf("reading conformance_test.dot: %v", err)
	}

	graph, err := dot.Parse(string(data))
	if err != nil {
		t.Fatalf("parsing conformance_test.dot: %v", err)
	}

	if graph.Name != "ConformanceTest" {
		t.Errorf("graph name = %q, want ConformanceTest", graph.Name)
	}
}

func TestConformanceTestDOT_ValidatesWithoutErrors(t *testing.T) {
	data, err := os.ReadFile("../examples/conformance_test.dot")
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	graph, err := dot.Parse(string(data))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	transforms := DefaultTransforms()
	graph = ApplyTransforms(graph, transforms...)

	diags := Validate(graph)
	for _, d := range diags {
		if d.Severity == SeverityError {
			t.Errorf("validation error: %s", d.Message)
		}
	}
}

func TestConformanceTestDOT_HasRequiredStructure(t *testing.T) {
	data, err := os.ReadFile("../examples/conformance_test.dot")
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	graph, err := dot.Parse(string(data))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}

	// Verify start and exit nodes exist
	var hasStart, hasExit bool
	nodesByID := make(map[string]*dot.Node)
	for _, n := range graph.Nodes {
		nodesByID[n.ID] = n
		if n.Attrs["shape"] == "Mdiamond" {
			hasStart = true
		}
		if n.Attrs["shape"] == "Msquare" {
			hasExit = true
		}
	}
	if !hasStart {
		t.Error("missing start node (shape=Mdiamond)")
	}
	if !hasExit {
		t.Error("missing exit node (shape=Msquare)")
	}

	// Verify key conformance features are present
	requiredNodes := []struct {
		id   string
		desc string
	}{
		{"Analyze", "Phase 1: linear flow"},
		{"Branch", "Phase 2: conditional branching"},
		{"BranchSuccessTarget", "Phase 2: success path target"},
		{"BranchFailTarget", "Phase 2: fail path target"},
		{"GoalGate", "Phase 3: goal gate"},
		{"ParallelSetup", "Phase 4: parallel fan-out"},
		{"ParallelJoin", "Phase 4: parallel fan-in"},
		{"BranchAlpha", "Phase 4: parallel branch"},
		{"BranchBeta", "Phase 4: parallel branch"},
		{"BranchGamma", "Phase 4: parallel branch"},
		{"ContextCheck", "Phase 5: context verification"},
		{"WeightedA", "Phase 5: low-weight target"},
		{"WeightedB", "Phase 5: high-weight target"},
		{"Summary", "Phase 6: final summary"},
		{"FailSink", "failure reporting"},
	}

	for _, rn := range requiredNodes {
		if _, ok := nodesByID[rn.id]; !ok {
			t.Errorf("missing required node %q (%s)", rn.id, rn.desc)
		}
	}

	// Verify GoalGate has goal_gate=true
	if gg, ok := nodesByID["GoalGate"]; ok {
		if gg.Attrs["goal_gate"] != "true" {
			t.Error("GoalGate node missing goal_gate=true attribute")
		}
	}

	// Verify parallel fan-out/fan-in shapes
	if ps, ok := nodesByID["ParallelSetup"]; ok {
		if ps.Attrs["shape"] != "component" {
			t.Errorf("ParallelSetup shape = %q, want component", ps.Attrs["shape"])
		}
	}
	if pj, ok := nodesByID["ParallelJoin"]; ok {
		if pj.Attrs["shape"] != "tripleoctagon" {
			t.Errorf("ParallelJoin shape = %q, want tripleoctagon", pj.Attrs["shape"])
		}
	}

	// Verify conditional edges exist from Branch with correct targets
	var hasSuccessEdge, hasFailEdge bool
	for _, e := range graph.Edges {
		if e.From == "Branch" && e.Attrs["condition"] == "outcome=success" && e.To == "BranchSuccessTarget" {
			hasSuccessEdge = true
		}
		if e.From == "Branch" && e.Attrs["condition"] == "outcome=fail" && e.To == "BranchFailTarget" {
			hasFailEdge = true
		}
	}
	if !hasSuccessEdge {
		t.Error("missing conditional success edge: Branch -> BranchSuccessTarget [condition=outcome=success]")
	}
	if !hasFailEdge {
		t.Error("missing conditional fail edge: Branch -> BranchFailTarget [condition=outcome=fail]")
	}

	// Verify Phase 3 goal-gate wiring
	edgeExists := func(from, to string) bool {
		for _, e := range graph.Edges {
			if e.From == from && e.To == to {
				return true
			}
		}
		return false
	}
	if !edgeExists("GoalGateSetup", "GoalGate") {
		t.Error("missing edge: GoalGateSetup -> GoalGate")
	}
	if !edgeExists("GoalGate", "ParallelSetup") {
		t.Error("missing edge: GoalGate -> ParallelSetup")
	}
	if !edgeExists("GoalGate", "GoalGateSetup") {
		t.Error("missing retry edge: GoalGate -> GoalGateSetup")
	}

	// Verify Phase 4 parallel fan-out/fan-in wiring
	for _, branch := range []string{"BranchAlpha", "BranchBeta", "BranchGamma"} {
		if !edgeExists("ParallelSetup", branch) {
			t.Errorf("missing fan-out edge: ParallelSetup -> %s", branch)
		}
		if !edgeExists(branch, "ParallelJoin") {
			t.Errorf("missing fan-in edge: %s -> ParallelJoin", branch)
		}
	}

	// Verify completion path
	if !edgeExists("Summary", "Exit") {
		t.Error("missing completion edge: Summary -> Exit")
	}

	// Verify weighted edges from ContextCheck
	var hasLowWeight, hasHighWeight bool
	for _, e := range graph.Edges {
		if e.From == "ContextCheck" {
			if e.To == "WeightedA" && e.Attrs["weight"] == "1" {
				hasLowWeight = true
			}
			if e.To == "WeightedB" && e.Attrs["weight"] == "10" {
				hasHighWeight = true
			}
		}
	}
	if !hasLowWeight {
		t.Error("missing low-weight edge from ContextCheck to WeightedA")
	}
	if !hasHighWeight {
		t.Error("missing high-weight edge from ContextCheck to WeightedB")
	}
}

// TestConformancePhase2BranchingScenario verifies conditional routing
// behavior for the conformance test Phase 2 branching logic in isolation.
func TestConformancePhase2BranchingScenario(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"Branch": {
				ID: "Branch",
				Attrs: map[string]string{
					"shape":  "box",
					"label":  "Branch Test",
					"prompt": "Return outcome=success.",
				},
			},
			"BranchSuccessTarget": {
				ID: "BranchSuccessTarget",
				Attrs: map[string]string{
					"shape": "box",
					"label": "Success Path",
				},
			},
			"BranchFailTarget": {
				ID: "BranchFailTarget",
				Attrs: map[string]string{
					"shape": "box",
					"label": "Fail Path",
				},
			},
		},
		Edges: []*Edge{
			{
				From:  "Branch",
				To:    "BranchSuccessTarget",
				Attrs: map[string]string{"condition": "outcome=success", "label": "pass"},
			},
			{
				From:  "Branch",
				To:    "BranchFailTarget",
				Attrs: map[string]string{"condition": "outcome=fail", "label": "fail"},
			},
		},
	}

	branchNode := g.Nodes["Branch"]
	ctx := NewContext()
	outcome := &Outcome{Status: StatusSuccess}

	selectedEdge := SelectEdge(branchNode, outcome, ctx, g)
	if selectedEdge == nil {
		t.Fatal("expected edge to be selected, got nil")
	}
	if selectedEdge.To != "BranchSuccessTarget" {
		t.Errorf("expected edge to BranchSuccessTarget, got edge to %q", selectedEdge.To)
	}
}
