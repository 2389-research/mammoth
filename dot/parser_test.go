// ABOUTME: Tests for the recursive descent DOT parser in the consolidated dot package.
// ABOUTME: Table-driven tests covering nodes, edges, chained edges, defaults, subgraphs, error cases, and edge ID assignment.
package dot

import (
	"strings"
	"testing"
)

func TestParseSimpleDigraph(t *testing.T) {
	input := `digraph MyGraph {}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", input, err)
	}
	if g.Name != "MyGraph" {
		t.Errorf("graph name = %q, want %q", g.Name, "MyGraph")
	}
	if len(g.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(g.Edges))
	}
}

func TestParseSingleNodeWithAttrs(t *testing.T) {
	input := `digraph G {
		start [shape=Mdiamond, label="Start"]
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
	node := g.Nodes["start"]
	if node == nil {
		t.Fatal("expected node 'start' to exist")
	}
	if node.Attrs["shape"] != "Mdiamond" {
		t.Errorf("node shape = %q, want %q", node.Attrs["shape"], "Mdiamond")
	}
	if node.Attrs["label"] != "Start" {
		t.Errorf("node label = %q, want %q", node.Attrs["label"], "Start")
	}
}

func TestParseSingleEdgeWithAttrs(t *testing.T) {
	input := `digraph G {
		a -> b [label="next", color=red]
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	e := g.Edges[0]
	if e.From != "a" || e.To != "b" {
		t.Errorf("edge = %s->%s, want a->b", e.From, e.To)
	}
	if e.Attrs["label"] != "next" {
		t.Errorf("edge label = %q, want %q", e.Attrs["label"], "next")
	}
	if e.Attrs["color"] != "red" {
		t.Errorf("edge color = %q, want %q", e.Attrs["color"], "red")
	}
	// Nodes should be auto-created
	if g.Nodes["a"] == nil {
		t.Error("expected node 'a' to be auto-created")
	}
	if g.Nodes["b"] == nil {
		t.Error("expected node 'b' to be auto-created")
	}
}

func TestParseChainedEdges(t *testing.T) {
	input := `digraph G {
		a -> b -> c
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(g.Edges) != 2 {
		t.Fatalf("expected 2 edges from chained a->b->c, got %d", len(g.Edges))
	}
	if g.Edges[0].From != "a" || g.Edges[0].To != "b" {
		t.Errorf("edge[0] = %s->%s, want a->b", g.Edges[0].From, g.Edges[0].To)
	}
	if g.Edges[1].From != "b" || g.Edges[1].To != "c" {
		t.Errorf("edge[1] = %s->%s, want b->c", g.Edges[1].From, g.Edges[1].To)
	}
	// All three nodes should exist
	if len(g.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(g.Nodes))
	}
}

func TestParseNodeDefaults(t *testing.T) {
	input := `digraph G {
		node [shape=box]
		a
		b
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// Node defaults should be recorded on the graph
	if g.NodeDefaults["shape"] != "box" {
		t.Errorf("graph NodeDefaults shape = %q, want %q", g.NodeDefaults["shape"], "box")
	}
	// Nodes created after the default should inherit it
	if g.Nodes["a"] == nil {
		t.Fatal("expected node 'a' to exist")
	}
	if g.Nodes["a"].Attrs["shape"] != "box" {
		t.Errorf("node 'a' shape = %q, want %q", g.Nodes["a"].Attrs["shape"], "box")
	}
	if g.Nodes["b"].Attrs["shape"] != "box" {
		t.Errorf("node 'b' shape = %q, want %q", g.Nodes["b"].Attrs["shape"], "box")
	}
}

func TestParseEdgeDefaults(t *testing.T) {
	input := `digraph G {
		edge [color=red]
		a -> b
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.EdgeDefaults["color"] != "red" {
		t.Errorf("graph EdgeDefaults color = %q, want %q", g.EdgeDefaults["color"], "red")
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	if g.Edges[0].Attrs["color"] != "red" {
		t.Errorf("edge color = %q, want %q (from defaults)", g.Edges[0].Attrs["color"], "red")
	}
}

func TestParseGraphAttributesBlock(t *testing.T) {
	input := `digraph G {
		graph [rankdir=LR]
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.Attrs["rankdir"] != "LR" {
		t.Errorf("graph attr rankdir = %q, want %q", g.Attrs["rankdir"], "LR")
	}
}

func TestParseGraphAttributesKeyValue(t *testing.T) {
	input := `digraph G {
		rankdir=LR
		label="My Pipeline"
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.Attrs["rankdir"] != "LR" {
		t.Errorf("graph attr rankdir = %q, want %q", g.Attrs["rankdir"], "LR")
	}
	if g.Attrs["label"] != "My Pipeline" {
		t.Errorf("graph attr label = %q, want %q", g.Attrs["label"], "My Pipeline")
	}
}

func TestParseSubgraph(t *testing.T) {
	input := `digraph G {
		subgraph cluster_0 {
			label = "Processing"
			node [shape=box]
			a
			b
		}
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(g.Subgraphs) != 1 {
		t.Fatalf("expected 1 subgraph, got %d", len(g.Subgraphs))
	}
	sg := g.Subgraphs[0]
	if sg.Name != "cluster_0" {
		t.Errorf("subgraph name = %q, want %q", sg.Name, "cluster_0")
	}
	if sg.Attrs["label"] != "Processing" {
		t.Errorf("subgraph label = %q, want %q", sg.Attrs["label"], "Processing")
	}
	if len(sg.NodeIDs) != 2 {
		t.Errorf("expected 2 nodes in subgraph, got %d", len(sg.NodeIDs))
	}
	// Nodes should get derived class from label
	if g.Nodes["a"] != nil && g.Nodes["a"].Attrs["class"] != "processing" {
		t.Errorf("node 'a' class = %q, want %q", g.Nodes["a"].Attrs["class"], "processing")
	}
}

func TestParseErrorMissingDigraph(t *testing.T) {
	input := `graph G {}`
	_, err := Parse(input)
	if err == nil {
		t.Fatal("expected error for missing 'digraph' keyword")
	}
	if !strings.Contains(err.Error(), "digraph") {
		t.Errorf("error = %q, should mention 'digraph'", err.Error())
	}
}

func TestParseErrorStrictModifier(t *testing.T) {
	input := `strict digraph G {}`
	_, err := Parse(input)
	if err == nil {
		t.Fatal("expected error for 'strict' modifier")
	}
	if !strings.Contains(err.Error(), "strict") {
		t.Errorf("error = %q, should mention 'strict'", err.Error())
	}
}

func TestParseErrorUndirectedEdge(t *testing.T) {
	input := `digraph G { a -- b }`
	_, err := Parse(input)
	if err == nil {
		t.Fatal("expected error for undirected edge (--)")
	}
	if !strings.Contains(err.Error(), "undirected") {
		t.Errorf("error = %q, should mention 'undirected'", err.Error())
	}
}

func TestParseErrorMultipleDigraphs(t *testing.T) {
	input := `digraph A {} digraph B {}`
	_, err := Parse(input)
	if err == nil {
		t.Fatal("expected error for multiple digraphs")
	}
	if !strings.Contains(err.Error(), "multiple digraphs") {
		t.Errorf("error = %q, should mention 'multiple digraphs'", err.Error())
	}
}

func TestParseFullPipeline(t *testing.T) {
	input := `digraph Pipeline {
		graph [goal="Build and test"]
		node [shape=box]
		edge [color=black]

		start [shape=Mdiamond, label="Start"]
		process [label="Process"]
		finish [shape=Msquare, label="End"]

		start -> process -> finish
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if g.Name != "Pipeline" {
		t.Errorf("graph name = %q, want %q", g.Name, "Pipeline")
	}
	if g.Attrs["goal"] != "Build and test" {
		t.Errorf("graph goal = %q, want %q", g.Attrs["goal"], "Build and test")
	}
	if len(g.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(g.Edges))
	}

	// Start node should have its explicit shape, not the default
	startNode := g.Nodes["start"]
	if startNode == nil {
		t.Fatal("expected 'start' node")
	}
	if startNode.Attrs["shape"] != "Mdiamond" {
		t.Errorf("start shape = %q, want %q", startNode.Attrs["shape"], "Mdiamond")
	}

	// Process node should have the default shape=box
	processNode := g.Nodes["process"]
	if processNode == nil {
		t.Fatal("expected 'process' node")
	}
	if processNode.Attrs["shape"] != "box" {
		t.Errorf("process shape = %q, want %q", processNode.Attrs["shape"], "box")
	}

	// Edges should have the default color
	for i, e := range g.Edges {
		if e.Attrs["color"] != "black" {
			t.Errorf("edge[%d] color = %q, want %q (from defaults)", i, e.Attrs["color"], "black")
		}
	}
}

func TestParseAssignEdgeIDs(t *testing.T) {
	input := `digraph G {
		a -> b
		b -> c
		a -> c
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(g.Edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(g.Edges))
	}

	// Every edge should have a non-empty ID (assigned by AssignEdgeIDs at end of Parse)
	for i, e := range g.Edges {
		if e.ID == "" {
			t.Errorf("edge[%d] (%s->%s) has empty ID; AssignEdgeIDs should have run", i, e.From, e.To)
		}
	}

	// IDs should be unique
	seen := make(map[string]bool)
	for _, e := range g.Edges {
		if seen[e.ID] {
			t.Errorf("duplicate edge ID %q", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestParseEdgeAttrOverridesDefault(t *testing.T) {
	input := `digraph G {
		edge [color=red]
		a -> b [color=blue]
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.Edges[0].Attrs["color"] != "blue" {
		t.Errorf("edge color = %q, want %q (explicit overrides default)", g.Edges[0].Attrs["color"], "blue")
	}
}

func TestParseNodeAttrOverridesDefault(t *testing.T) {
	input := `digraph G {
		node [shape=box]
		a [shape=circle]
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.Nodes["a"].Attrs["shape"] != "circle" {
		t.Errorf("node shape = %q, want %q (explicit overrides default)", g.Nodes["a"].Attrs["shape"], "circle")
	}
}

func TestParseSemicolonSeparators(t *testing.T) {
	input := `digraph G {
		a; b; a -> b;
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(g.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(g.Edges))
	}
}

func TestParseQuotedNodeIDs(t *testing.T) {
	input := `digraph G {
		"my node" [label="My Node"]
		"other node" [label="Other"]
		"my node" -> "other node"
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.Nodes["my node"] == nil {
		t.Error("expected node 'my node' to exist")
	}
	if g.Nodes["other node"] == nil {
		t.Error("expected node 'other node' to exist")
	}
	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	if g.Edges[0].From != "my node" || g.Edges[0].To != "other node" {
		t.Errorf("edge = %q->%q, want 'my node'->'other node'", g.Edges[0].From, g.Edges[0].To)
	}
}

func TestParseEmptyAttrBlock(t *testing.T) {
	input := `digraph G {
		a []
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.Nodes["a"] == nil {
		t.Error("expected node 'a' to exist")
	}
}

func TestParseTrailingCommaInAttrs(t *testing.T) {
	input := `digraph G {
		a [shape=box, label="A",]
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if g.Nodes["a"].Attrs["shape"] != "box" {
		t.Errorf("node shape = %q, want %q", g.Nodes["a"].Attrs["shape"], "box")
	}
}

func TestParseSubgraphDerivedClass(t *testing.T) {
	input := `digraph G {
		subgraph cluster_loop {
			label = "Loop Alpha"
			x
			y
		}
	}`
	g, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// "Loop Alpha" should derive to "loop-alpha"
	if g.Nodes["x"] != nil && g.Nodes["x"].Attrs["class"] != "loop-alpha" {
		t.Errorf("node 'x' class = %q, want %q", g.Nodes["x"].Attrs["class"], "loop-alpha")
	}
}
