// ABOUTME: Tests for the DOT DSL recursive descent parser.
// ABOUTME: Covers digraph parsing, attributes, chained edges, defaults, subgraphs, and error rejection.
package attractor

import (
	"strings"
	"testing"
)

func TestParseSimpleDigraph(t *testing.T) {
	input := `digraph Simple {
		start [shape=Mdiamond, label="Start"]
		exit  [shape=Msquare, label="Exit"]
		work  [label="Do Work"]
		start -> work -> exit
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if g.Name != "Simple" {
		t.Errorf("graph name = %q, want %q", g.Name, "Simple")
	}

	if len(g.Nodes) != 3 {
		t.Errorf("got %d nodes, want 3", len(g.Nodes))
	}

	if len(g.Edges) != 2 {
		t.Errorf("got %d edges, want 2", len(g.Edges))
	}

	// Verify start -> work edge
	if g.Edges[0].From != "start" || g.Edges[0].To != "work" {
		t.Errorf("edge[0] = %s -> %s, want start -> work", g.Edges[0].From, g.Edges[0].To)
	}

	// Verify work -> exit edge
	if g.Edges[1].From != "work" || g.Edges[1].To != "exit" {
		t.Errorf("edge[1] = %s -> %s, want work -> exit", g.Edges[1].From, g.Edges[1].To)
	}
}

func TestParseNodeAttributes(t *testing.T) {
	input := `digraph Test {
		mynode [label="My Node", shape=box, timeout="900s", prompt="Do something"]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	node := g.FindNode("mynode")
	if node == nil {
		t.Fatal("node 'mynode' not found")
	}

	tests := []struct {
		key  string
		want string
	}{
		{"label", "My Node"},
		{"shape", "box"},
		{"timeout", "900s"},
		{"prompt", "Do something"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, ok := node.Attrs[tt.key]
			if !ok {
				t.Errorf("node missing attribute %q", tt.key)
			}
			if got != tt.want {
				t.Errorf("node.Attrs[%q] = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestParseEdgeAttributes(t *testing.T) {
	input := `digraph Test {
		A [label="A"]
		B [label="B"]
		A -> B [label="Yes", condition="outcome=success", weight=10]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(g.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(g.Edges))
	}

	edge := g.Edges[0]
	tests := []struct {
		key  string
		want string
	}{
		{"label", "Yes"},
		{"condition", "outcome=success"},
		{"weight", "10"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, ok := edge.Attrs[tt.key]
			if !ok {
				t.Errorf("edge missing attribute %q", tt.key)
			}
			if got != tt.want {
				t.Errorf("edge.Attrs[%q] = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestParseChainedEdges(t *testing.T) {
	input := `digraph Test {
		A [label="A"]
		B [label="B"]
		C [label="C"]
		A -> B -> C [label="next"]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(g.Edges) != 2 {
		t.Fatalf("got %d edges, want 2 (chained expansion)", len(g.Edges))
	}

	// A -> B with label
	if g.Edges[0].From != "A" || g.Edges[0].To != "B" {
		t.Errorf("edge[0] = %s -> %s, want A -> B", g.Edges[0].From, g.Edges[0].To)
	}
	if g.Edges[0].Attrs["label"] != "next" {
		t.Errorf("edge[0] label = %q, want %q", g.Edges[0].Attrs["label"], "next")
	}

	// B -> C with label
	if g.Edges[1].From != "B" || g.Edges[1].To != "C" {
		t.Errorf("edge[1] = %s -> %s, want B -> C", g.Edges[1].From, g.Edges[1].To)
	}
	if g.Edges[1].Attrs["label"] != "next" {
		t.Errorf("edge[1] label = %q, want %q", g.Edges[1].Attrs["label"], "next")
	}
}

func TestParseGraphAttributes(t *testing.T) {
	input := `digraph Test {
		graph [goal="Run tests and report"]
		rankdir=LR
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if g.Attrs["goal"] != "Run tests and report" {
		t.Errorf("graph goal = %q, want %q", g.Attrs["goal"], "Run tests and report")
	}

	if g.Attrs["rankdir"] != "LR" {
		t.Errorf("graph rankdir = %q, want %q", g.Attrs["rankdir"], "LR")
	}
}

func TestParseNodeDefaults(t *testing.T) {
	input := `digraph Test {
		node [shape=box, timeout="900s"]
		work [label="Work"]
		plan [label="Plan"]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	// Node defaults should be stored
	if g.NodeDefaults["shape"] != "box" {
		t.Errorf("node default shape = %q, want %q", g.NodeDefaults["shape"], "box")
	}
	if g.NodeDefaults["timeout"] != "900s" {
		t.Errorf("node default timeout = %q, want %q", g.NodeDefaults["timeout"], "900s")
	}

	// Nodes should inherit defaults
	work := g.FindNode("work")
	if work == nil {
		t.Fatal("node 'work' not found")
	}
	if work.Attrs["shape"] != "box" {
		t.Errorf("work.shape = %q, want %q (from defaults)", work.Attrs["shape"], "box")
	}
	if work.Attrs["timeout"] != "900s" {
		t.Errorf("work.timeout = %q, want %q (from defaults)", work.Attrs["timeout"], "900s")
	}

	// Explicit attributes should override defaults
	input2 := `digraph Test2 {
		node [shape=box, timeout="900s"]
		special [label="Special", shape=diamond, timeout="1800s"]
	}`

	g2, err := Parse(input2)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	special := g2.FindNode("special")
	if special == nil {
		t.Fatal("node 'special' not found")
	}
	if special.Attrs["shape"] != "diamond" {
		t.Errorf("special.shape = %q, want %q (explicit override)", special.Attrs["shape"], "diamond")
	}
	if special.Attrs["timeout"] != "1800s" {
		t.Errorf("special.timeout = %q, want %q (explicit override)", special.Attrs["timeout"], "1800s")
	}
}

func TestParseEdgeDefaults(t *testing.T) {
	input := `digraph Test {
		edge [weight=0]
		A [label="A"]
		B [label="B"]
		C [label="C"]
		A -> B
		B -> C [weight=5]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if g.EdgeDefaults["weight"] != "0" {
		t.Errorf("edge default weight = %q, want %q", g.EdgeDefaults["weight"], "0")
	}

	// First edge should inherit default
	if g.Edges[0].Attrs["weight"] != "0" {
		t.Errorf("edge[0] weight = %q, want %q (from defaults)", g.Edges[0].Attrs["weight"], "0")
	}

	// Second edge should override default
	if g.Edges[1].Attrs["weight"] != "5" {
		t.Errorf("edge[1] weight = %q, want %q (explicit override)", g.Edges[1].Attrs["weight"], "5")
	}
}

func TestParseSubgraph(t *testing.T) {
	input := `digraph Test {
		subgraph cluster_loop {
			label = "Loop A"
			node [thread_id="loop-a", timeout="900s"]
			Plan      [label="Plan next step"]
			Implement [label="Implement", timeout="1800s"]
		}
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(g.Subgraphs) != 1 {
		t.Fatalf("got %d subgraphs, want 1", len(g.Subgraphs))
	}

	sg := g.Subgraphs[0]
	if sg.Name != "cluster_loop" {
		t.Errorf("subgraph name = %q, want %q", sg.Name, "cluster_loop")
	}

	if len(sg.Nodes) != 2 {
		t.Errorf("subgraph has %d nodes, want 2", len(sg.Nodes))
	}

	// Nodes should be in the subgraph
	nodeSet := make(map[string]bool)
	for _, id := range sg.Nodes {
		nodeSet[id] = true
	}
	if !nodeSet["Plan"] {
		t.Error("subgraph missing node 'Plan'")
	}
	if !nodeSet["Implement"] {
		t.Error("subgraph missing node 'Implement'")
	}

	// Plan should inherit subgraph node defaults
	plan := g.FindNode("Plan")
	if plan == nil {
		t.Fatal("node 'Plan' not found in graph")
	}
	if plan.Attrs["thread_id"] != "loop-a" {
		t.Errorf("Plan.thread_id = %q, want %q", plan.Attrs["thread_id"], "loop-a")
	}
	if plan.Attrs["timeout"] != "900s" {
		t.Errorf("Plan.timeout = %q, want %q", plan.Attrs["timeout"], "900s")
	}

	// Implement should override timeout but inherit thread_id
	impl := g.FindNode("Implement")
	if impl == nil {
		t.Fatal("node 'Implement' not found in graph")
	}
	if impl.Attrs["thread_id"] != "loop-a" {
		t.Errorf("Implement.thread_id = %q, want %q", impl.Attrs["thread_id"], "loop-a")
	}
	if impl.Attrs["timeout"] != "1800s" {
		t.Errorf("Implement.timeout = %q, want %q (explicit override)", impl.Attrs["timeout"], "1800s")
	}
}

func TestParseSubgraphClassDerivation(t *testing.T) {
	input := `digraph Test {
		subgraph cluster_loop {
			label = "Loop A"
			Plan [label="Plan"]
		}
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if len(g.Subgraphs) != 1 {
		t.Fatalf("got %d subgraphs, want 1", len(g.Subgraphs))
	}

	// Check that the Plan node gets the derived class
	plan := g.FindNode("Plan")
	if plan == nil {
		t.Fatal("node 'Plan' not found")
	}

	// Class should be derived from subgraph label: "Loop A" -> "loop-a"
	if plan.Attrs["class"] == "" {
		t.Error("Plan.class should be set from subgraph label derivation")
	}
	if plan.Attrs["class"] != "loop-a" {
		t.Errorf("Plan.class = %q, want %q", plan.Attrs["class"], "loop-a")
	}
}

func TestParseComplexPipeline(t *testing.T) {
	input := `digraph Branch {
		graph [goal="Implement and validate a feature"]
		rankdir=LR
		node [shape=box, timeout="900s"]

		start     [shape=Mdiamond, label="Start"]
		exit      [shape=Msquare, label="Exit"]
		plan      [label="Plan", prompt="Plan the implementation"]
		implement [label="Implement", prompt="Implement the plan"]
		validate  [label="Validate", prompt="Run tests"]
		gate      [shape=diamond, label="Tests passing?"]

		start -> plan -> implement -> validate -> gate
		gate -> exit      [label="Yes", condition="outcome=success"]
		gate -> implement [label="No", condition="outcome!=success"]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if g.Name != "Branch" {
		t.Errorf("graph name = %q, want %q", g.Name, "Branch")
	}

	if g.Attrs["goal"] != "Implement and validate a feature" {
		t.Errorf("graph goal = %q, want %q", g.Attrs["goal"], "Implement and validate a feature")
	}

	// Should have 6 nodes
	if len(g.Nodes) != 6 {
		t.Errorf("got %d nodes, want 6", len(g.Nodes))
	}

	// Chained edges: start->plan, plan->implement, implement->validate, validate->gate
	// Plus: gate->exit, gate->implement = 6 total edges
	if len(g.Edges) != 6 {
		t.Errorf("got %d edges, want 6", len(g.Edges))
	}

	// Start node should have Mdiamond shape (overriding default box)
	startNode := g.FindNode("start")
	if startNode == nil {
		t.Fatal("start node not found")
	}
	if startNode.Attrs["shape"] != "Mdiamond" {
		t.Errorf("start.shape = %q, want %q", startNode.Attrs["shape"], "Mdiamond")
	}

	// plan should inherit default shape=box
	planNode := g.FindNode("plan")
	if planNode == nil {
		t.Fatal("plan node not found")
	}
	if planNode.Attrs["shape"] != "box" {
		t.Errorf("plan.shape = %q, want %q (from defaults)", planNode.Attrs["shape"], "box")
	}
	if planNode.Attrs["timeout"] != "900s" {
		t.Errorf("plan.timeout = %q, want %q (from defaults)", planNode.Attrs["timeout"], "900s")
	}

	// gate should have explicit diamond shape (overriding default)
	gate := g.FindNode("gate")
	if gate == nil {
		t.Fatal("gate node not found")
	}
	if gate.Attrs["shape"] != "diamond" {
		t.Errorf("gate.shape = %q, want %q", gate.Attrs["shape"], "diamond")
	}

	// Check conditional edges from gate
	gateEdges := g.OutgoingEdges("gate")
	if len(gateEdges) != 2 {
		t.Fatalf("gate has %d outgoing edges, want 2", len(gateEdges))
	}
}

func TestParseHumanGate(t *testing.T) {
	input := `digraph Review {
		rankdir=LR

		start [shape=Mdiamond, label="Start"]
		exit  [shape=Msquare, label="Exit"]

		review_gate [
			shape=hexagon,
			label="Review Changes",
			type="wait.human"
		]

		start -> review_gate
		review_gate -> ship_it [label="[A] Approve"]
		review_gate -> fixes   [label="[F] Fix"]
		ship_it -> exit
		fixes -> review_gate
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if g.Name != "Review" {
		t.Errorf("graph name = %q, want %q", g.Name, "Review")
	}

	// Should have 5 nodes (start, exit, review_gate, ship_it, fixes)
	if len(g.Nodes) != 5 {
		t.Errorf("got %d nodes, want 5", len(g.Nodes))
	}

	// review_gate attributes
	rg := g.FindNode("review_gate")
	if rg == nil {
		t.Fatal("review_gate node not found")
	}
	if rg.Attrs["shape"] != "hexagon" {
		t.Errorf("review_gate.shape = %q, want %q", rg.Attrs["shape"], "hexagon")
	}
	if rg.Attrs["type"] != "wait.human" {
		t.Errorf("review_gate.type = %q, want %q", rg.Attrs["type"], "wait.human")
	}

	// Should have 5 edges
	if len(g.Edges) != 5 {
		t.Errorf("got %d edges, want 5", len(g.Edges))
	}

	// Check that ship_it and fixes are created as implicit nodes
	shipIt := g.FindNode("ship_it")
	if shipIt == nil {
		t.Error("ship_it node not found (should be implicitly created)")
	}
	fixes := g.FindNode("fixes")
	if fixes == nil {
		t.Error("fixes node not found (should be implicitly created)")
	}
}

func TestParseRejectUndirected(t *testing.T) {
	input := `digraph Test {
		A -- B
	}`

	_, err := Parse(input)
	if err == nil {
		t.Error("Parse should reject undirected edges (--)")
	}
	if err != nil && !strings.Contains(err.Error(), "undirected") && !strings.Contains(err.Error(), "--") {
		t.Errorf("error should mention undirected edges, got: %v", err)
	}
}

func TestParseRejectMultipleDigraphs(t *testing.T) {
	input := `digraph First {
		A [label="A"]
	}
	digraph Second {
		B [label="B"]
	}`

	_, err := Parse(input)
	if err == nil {
		t.Error("Parse should reject multiple digraphs")
	}
}

func TestParseRejectStrict(t *testing.T) {
	input := `strict digraph Test {
		A [label="A"]
	}`

	_, err := Parse(input)
	if err == nil {
		t.Error("Parse should reject strict modifier")
	}
}

func TestParseEmptyDigraph(t *testing.T) {
	input := `digraph Empty {}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.Name != "Empty" {
		t.Errorf("graph name = %q, want %q", g.Name, "Empty")
	}
	if len(g.Nodes) != 0 {
		t.Errorf("got %d nodes, want 0", len(g.Nodes))
	}
}

func TestParseSemicolons(t *testing.T) {
	input := `digraph Test {
		A [label="A"];
		B [label="B"];
		A -> B;
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Errorf("got %d nodes, want 2", len(g.Nodes))
	}
	if len(g.Edges) != 1 {
		t.Errorf("got %d edges, want 1", len(g.Edges))
	}
}

func TestParseGraphAttrDecl(t *testing.T) {
	input := `digraph Test {
		rankdir=LR
		label="My Pipeline"
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if g.Attrs["rankdir"] != "LR" {
		t.Errorf("graph rankdir = %q, want %q", g.Attrs["rankdir"], "LR")
	}
	if g.Attrs["label"] != "My Pipeline" {
		t.Errorf("graph label = %q, want %q", g.Attrs["label"], "My Pipeline")
	}
}

func TestParseMultilineNodeAttrs(t *testing.T) {
	input := `digraph Test {
		mynode [
			label="My Node",
			shape=hexagon,
			type="wait.human"
		]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	node := g.FindNode("mynode")
	if node == nil {
		t.Fatal("mynode not found")
	}
	if node.Attrs["label"] != "My Node" {
		t.Errorf("mynode.label = %q, want %q", node.Attrs["label"], "My Node")
	}
	if node.Attrs["shape"] != "hexagon" {
		t.Errorf("mynode.shape = %q, want %q", node.Attrs["shape"], "hexagon")
	}
	if node.Attrs["type"] != "wait.human" {
		t.Errorf("mynode.type = %q, want %q", node.Attrs["type"], "wait.human")
	}
}

func TestParseBooleanAttrs(t *testing.T) {
	input := `digraph Test {
		mynode [goal_gate=true, auto_status=false]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	node := g.FindNode("mynode")
	if node == nil {
		t.Fatal("mynode not found")
	}
	if node.Attrs["goal_gate"] != "true" {
		t.Errorf("mynode.goal_gate = %q, want %q", node.Attrs["goal_gate"], "true")
	}
	if node.Attrs["auto_status"] != "false" {
		t.Errorf("mynode.auto_status = %q, want %q", node.Attrs["auto_status"], "false")
	}
}

func TestParseNumberAttrs(t *testing.T) {
	input := `digraph Test {
		mynode [max_retries=3, weight=-1]
	}`

	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	node := g.FindNode("mynode")
	if node == nil {
		t.Fatal("mynode not found")
	}
	if node.Attrs["max_retries"] != "3" {
		t.Errorf("mynode.max_retries = %q, want %q", node.Attrs["max_retries"], "3")
	}
	if node.Attrs["weight"] != "-1" {
		t.Errorf("mynode.weight = %q, want %q", node.Attrs["weight"], "-1")
	}
}
