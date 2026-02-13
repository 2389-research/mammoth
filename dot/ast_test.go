// ABOUTME: Tests for the consolidated DOT AST types used by the unified parser.
// ABOUTME: Covers Graph, Node, Edge, Subgraph, Diagnostic types and all helper methods.
package dot

import (
	"testing"
)

// --- Type construction tests ---

func TestNewGraph(t *testing.T) {
	g := &Graph{Name: "test_pipeline"}
	if g.Name != "test_pipeline" {
		t.Errorf("expected graph name %q, got %q", "test_pipeline", g.Name)
	}
	if g.Nodes != nil {
		t.Error("expected nil Nodes map on zero-value graph")
	}
}

func TestNodeAttrs(t *testing.T) {
	n := &Node{
		ID:    "my_node",
		Attrs: map[string]string{"shape": "box", "label": "My Node"},
	}
	if n.ID != "my_node" {
		t.Errorf("expected node ID %q, got %q", "my_node", n.ID)
	}
	if n.Attrs["shape"] != "box" {
		t.Errorf("expected shape %q, got %q", "box", n.Attrs["shape"])
	}
	if n.Attrs["label"] != "My Node" {
		t.Errorf("expected label %q, got %q", "My Node", n.Attrs["label"])
	}
}

func TestEdgeHasID(t *testing.T) {
	e := &Edge{
		ID:    "e1",
		From:  "a",
		To:    "b",
		Attrs: map[string]string{"label": "next"},
	}
	if e.ID != "e1" {
		t.Errorf("expected edge ID %q, got %q", "e1", e.ID)
	}
	if e.From != "a" {
		t.Errorf("expected edge From %q, got %q", "a", e.From)
	}
	if e.To != "b" {
		t.Errorf("expected edge To %q, got %q", "b", e.To)
	}
	if e.Attrs["label"] != "next" {
		t.Errorf("expected edge label %q, got %q", "next", e.Attrs["label"])
	}
}

func TestSubgraphFields(t *testing.T) {
	sg := &Subgraph{
		ID:           "cluster_0",
		Name:         "setup",
		Attrs:        map[string]string{"style": "filled"},
		NodeIDs:      []string{"a", "b"},
		NodeDefaults: map[string]string{"shape": "box"},
	}
	if sg.ID != "cluster_0" {
		t.Errorf("expected subgraph ID %q, got %q", "cluster_0", sg.ID)
	}
	if sg.Name != "setup" {
		t.Errorf("expected subgraph Name %q, got %q", "setup", sg.Name)
	}
	if len(sg.NodeIDs) != 2 {
		t.Fatalf("expected 2 NodeIDs, got %d", len(sg.NodeIDs))
	}
	if sg.NodeDefaults["shape"] != "box" {
		t.Errorf("expected NodeDefaults shape %q, got %q", "box", sg.NodeDefaults["shape"])
	}
}

func TestDiagnosticFields(t *testing.T) {
	d := Diagnostic{
		Severity: "error",
		Message:  "node has no outgoing edges",
		NodeID:   "stuck_node",
		EdgeID:   "",
		Rule:     "reachability",
	}
	if d.Severity != "error" {
		t.Errorf("expected severity %q, got %q", "error", d.Severity)
	}
	if d.Message != "node has no outgoing edges" {
		t.Errorf("expected message %q, got %q", "node has no outgoing edges", d.Message)
	}
	if d.NodeID != "stuck_node" {
		t.Errorf("expected NodeID %q, got %q", "stuck_node", d.NodeID)
	}
	if d.Rule != "reachability" {
		t.Errorf("expected Rule %q, got %q", "reachability", d.Rule)
	}
}

// --- AddNode / AddEdge tests ---

func TestAddNode(t *testing.T) {
	g := &Graph{Name: "test"}
	n := &Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}}

	g.AddNode(n)

	if len(g.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(g.Nodes))
	}
	if g.Nodes["start"] != n {
		t.Error("expected the exact node pointer to be stored")
	}
}

func TestAddNodeInitializesMap(t *testing.T) {
	g := &Graph{}
	n := &Node{ID: "a"}
	g.AddNode(n)

	if g.Nodes == nil {
		t.Fatal("AddNode should initialize the Nodes map")
	}
	if g.Nodes["a"] != n {
		t.Error("expected node to be stored")
	}
}

func TestAddEdge(t *testing.T) {
	g := &Graph{Name: "test"}
	e := &Edge{From: "a", To: "b"}

	g.AddEdge(e)

	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}
	if g.Edges[0] != e {
		t.Error("expected the exact edge pointer to be stored")
	}
}

func TestAddMultipleEdges(t *testing.T) {
	g := &Graph{}
	g.AddEdge(&Edge{From: "a", To: "b"})
	g.AddEdge(&Edge{From: "b", To: "c"})
	g.AddEdge(&Edge{From: "a", To: "c"})

	if len(g.Edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(g.Edges))
	}
}

// --- FindNode tests ---

func TestFindNode(t *testing.T) {
	tests := []struct {
		name     string
		graph    *Graph
		nodeID   string
		wantNil  bool
		wantID   string
	}{
		{
			name:    "nil nodes map",
			graph:   &Graph{},
			nodeID:  "x",
			wantNil: true,
		},
		{
			name: "node exists",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"end":   {ID: "end"},
				},
			},
			nodeID: "start",
			wantID: "start",
		},
		{
			name: "node not found",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start"},
				},
			},
			nodeID:  "missing",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.graph.FindNode(tt.nodeID)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got node %q", got.ID)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil node")
			}
			if got.ID != tt.wantID {
				t.Errorf("expected node ID %q, got %q", tt.wantID, got.ID)
			}
		})
	}
}

// --- OutgoingEdges tests ---

func TestOutgoingEdges(t *testing.T) {
	g := buildTestGraph()

	tests := []struct {
		name   string
		nodeID string
		want   int
	}{
		{"start has 2 outgoing", "start", 2},
		{"process has 1 outgoing", "process", 1},
		{"review has 1 outgoing", "review", 1},
		{"end has 0 outgoing", "end", 0},
		{"nonexistent has 0 outgoing", "nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := g.OutgoingEdges(tt.nodeID)
			if len(edges) != tt.want {
				t.Errorf("expected %d outgoing edges for %q, got %d", tt.want, tt.nodeID, len(edges))
			}
		})
	}
}

// --- IncomingEdges tests ---

func TestIncomingEdges(t *testing.T) {
	g := buildTestGraph()

	tests := []struct {
		name   string
		nodeID string
		want   int
	}{
		{"start has 0 incoming", "start", 0},
		{"process has 1 incoming", "process", 1},
		{"review has 1 incoming", "review", 1},
		{"end has 2 incoming", "end", 2},
		{"nonexistent has 0 incoming", "nonexistent", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := g.IncomingEdges(tt.nodeID)
			if len(edges) != tt.want {
				t.Errorf("expected %d incoming edges for %q, got %d", tt.want, tt.nodeID, len(edges))
			}
		})
	}
}

// --- FindStartNode tests ---

func TestFindStartNode(t *testing.T) {
	tests := []struct {
		name   string
		graph  *Graph
		wantID string
		found  bool
	}{
		{
			name:  "empty graph",
			graph: &Graph{},
			found: false,
		},
		{
			name: "by shape Mdiamond",
			graph: &Graph{
				Nodes: map[string]*Node{
					"s": {ID: "s", Attrs: map[string]string{"shape": "Mdiamond"}},
					"e": {ID: "e"},
				},
			},
			wantID: "s",
			found:  true,
		},
		{
			name: "by node_type start",
			graph: &Graph{
				Nodes: map[string]*Node{
					"begin": {ID: "begin", Attrs: map[string]string{"node_type": "start"}},
				},
			},
			wantID: "begin",
			found:  true,
		},
		{
			name: "by type start",
			graph: &Graph{
				Nodes: map[string]*Node{
					"init": {ID: "init", Attrs: map[string]string{"type": "start"}},
				},
			},
			wantID: "init",
			found:  true,
		},
		{
			name: "no start node",
			graph: &Graph{
				Nodes: map[string]*Node{
					"a": {ID: "a", Attrs: map[string]string{"shape": "box"}},
				},
			},
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.graph.FindStartNode()
			if !tt.found {
				if got != nil {
					t.Errorf("expected nil, got %q", got.ID)
				}
				return
			}
			if got == nil {
				t.Fatal("expected start node, got nil")
			}
			if got.ID != tt.wantID {
				t.Errorf("expected start node ID %q, got %q", tt.wantID, got.ID)
			}
		})
	}
}

// --- FindExitNode tests ---

func TestFindExitNode(t *testing.T) {
	tests := []struct {
		name   string
		graph  *Graph
		wantID string
		found  bool
	}{
		{
			name:  "empty graph",
			graph: &Graph{},
			found: false,
		},
		{
			name: "by shape Msquare",
			graph: &Graph{
				Nodes: map[string]*Node{
					"done": {ID: "done", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			wantID: "done",
			found:  true,
		},
		{
			name: "by node_type exit",
			graph: &Graph{
				Nodes: map[string]*Node{
					"fin": {ID: "fin", Attrs: map[string]string{"node_type": "exit"}},
				},
			},
			wantID: "fin",
			found:  true,
		},
		{
			name: "by type exit",
			graph: &Graph{
				Nodes: map[string]*Node{
					"terminus": {ID: "terminus", Attrs: map[string]string{"type": "exit"}},
				},
			},
			wantID: "terminus",
			found:  true,
		},
		{
			name: "no exit node",
			graph: &Graph{
				Nodes: map[string]*Node{
					"a": {ID: "a", Attrs: map[string]string{"shape": "box"}},
				},
			},
			found: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.graph.FindExitNode()
			if !tt.found {
				if got != nil {
					t.Errorf("expected nil, got %q", got.ID)
				}
				return
			}
			if got == nil {
				t.Fatal("expected exit node, got nil")
			}
			if got.ID != tt.wantID {
				t.Errorf("expected exit node ID %q, got %q", tt.wantID, got.ID)
			}
		})
	}
}

// --- NodeIDs tests ---

func TestNodeIDs(t *testing.T) {
	tests := []struct {
		name  string
		graph *Graph
		want  []string
	}{
		{
			name:  "nil nodes",
			graph: &Graph{},
			want:  []string{},
		},
		{
			name: "sorted output",
			graph: &Graph{
				Nodes: map[string]*Node{
					"zebra":    {ID: "zebra"},
					"alpha":    {ID: "alpha"},
					"middle":   {ID: "middle"},
				},
			},
			want: []string{"alpha", "middle", "zebra"},
		},
		{
			name: "single node",
			graph: &Graph{
				Nodes: map[string]*Node{
					"only": {ID: "only"},
				},
			},
			want: []string{"only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.graph.NodeIDs()
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d IDs, got %d", len(tt.want), len(got))
			}
			for i, id := range got {
				if id != tt.want[i] {
					t.Errorf("position %d: expected %q, got %q", i, tt.want[i], id)
				}
			}
		})
	}
}

// --- StableID tests ---

func TestEdgeStableID(t *testing.T) {
	tests := []struct {
		name string
		edge *Edge
		want string
	}{
		{
			name: "basic edge",
			edge: &Edge{From: "a", To: "b"},
			want: "a->b",
		},
		{
			name: "with spaces in IDs",
			edge: &Edge{From: "start node", To: "end node"},
			want: "start node->end node",
		},
		{
			name: "edge with existing ID ignored for stable",
			edge: &Edge{ID: "e1", From: "x", To: "y"},
			want: "x->y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.edge.StableID()
			if got != tt.want {
				t.Errorf("expected StableID %q, got %q", tt.want, got)
			}
		})
	}
}

// --- AssignEdgeIDs tests ---

func TestAssignEdgeIDs(t *testing.T) {
	g := &Graph{}
	g.AddEdge(&Edge{From: "a", To: "b"})
	g.AddEdge(&Edge{From: "b", To: "c"})
	g.AddEdge(&Edge{From: "a", To: "c"})

	g.AssignEdgeIDs()

	// Each edge should have a non-empty ID
	for i, e := range g.Edges {
		if e.ID == "" {
			t.Errorf("edge %d (%s->%s) has empty ID after AssignEdgeIDs", i, e.From, e.To)
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

func TestAssignEdgeIDsPreservesExisting(t *testing.T) {
	g := &Graph{}
	g.AddEdge(&Edge{ID: "custom_id", From: "a", To: "b"})
	g.AddEdge(&Edge{From: "b", To: "c"})

	g.AssignEdgeIDs()

	if g.Edges[0].ID != "custom_id" {
		t.Errorf("expected preserved ID %q, got %q", "custom_id", g.Edges[0].ID)
	}
	if g.Edges[1].ID == "" {
		t.Error("second edge should have been assigned an ID")
	}
}

func TestAssignEdgeIDsHandlesDuplicateFromTo(t *testing.T) {
	g := &Graph{}
	g.AddEdge(&Edge{From: "a", To: "b"})
	g.AddEdge(&Edge{From: "a", To: "b"})

	g.AssignEdgeIDs()

	if g.Edges[0].ID == g.Edges[1].ID {
		t.Errorf("parallel edges should get distinct IDs, both got %q", g.Edges[0].ID)
	}
}

func TestAssignEdgeIDsEmptyGraph(t *testing.T) {
	g := &Graph{}
	g.AssignEdgeIDs() // should not panic
}

// --- helper to build a reusable test graph ---
//
// Graph structure:
//
//	start -> process -> end
//	start -> review  -> end
func buildTestGraph() *Graph {
	g := &Graph{Name: "pipeline"}
	g.AddNode(&Node{ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}})
	g.AddNode(&Node{ID: "process", Attrs: map[string]string{"shape": "box"}})
	g.AddNode(&Node{ID: "review", Attrs: map[string]string{"shape": "box"}})
	g.AddNode(&Node{ID: "end", Attrs: map[string]string{"shape": "Msquare"}})

	g.AddEdge(&Edge{From: "start", To: "process"})
	g.AddEdge(&Edge{From: "start", To: "review"})
	g.AddEdge(&Edge{From: "process", To: "end"})
	g.AddEdge(&Edge{From: "review", To: "end"})

	return g
}
