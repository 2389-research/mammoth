// ABOUTME: Tests for the DOT graph AST types and helper methods.
// ABOUTME: Covers node lookup, edge traversal, start/exit detection, and node ID listing.
package attractor

import (
	"testing"
)

func TestGraphFindNode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"work":  {ID: "work", Attrs: map[string]string{"label": "Do Work"}},
			"exit":  {ID: "exit", Attrs: map[string]string{"shape": "Msquare"}},
		},
	}

	tests := []struct {
		name    string
		nodeID  string
		wantNil bool
	}{
		{"find existing node", "start", false},
		{"find another existing node", "work", false},
		{"find nonexistent node", "nonexistent", true},
		{"empty string", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := g.FindNode(tt.nodeID)
			if tt.wantNil && node != nil {
				t.Errorf("FindNode(%q) = %v, want nil", tt.nodeID, node)
			}
			if !tt.wantNil && node == nil {
				t.Errorf("FindNode(%q) = nil, want non-nil", tt.nodeID)
			}
			if !tt.wantNil && node != nil && node.ID != tt.nodeID {
				t.Errorf("FindNode(%q).ID = %q, want %q", tt.nodeID, node.ID, tt.nodeID)
			}
		})
	}
}

func TestGraphOutgoingEdges(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"A": {ID: "A", Attrs: map[string]string{}},
			"B": {ID: "B", Attrs: map[string]string{}},
			"C": {ID: "C", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "A", To: "B", Attrs: map[string]string{"label": "first"}},
			{From: "A", To: "C", Attrs: map[string]string{"label": "second"}},
			{From: "B", To: "C", Attrs: map[string]string{"label": "third"}},
		},
	}

	tests := []struct {
		name    string
		nodeID  string
		wantLen int
		wantTos []string
	}{
		{"node with two outgoing", "A", 2, []string{"B", "C"}},
		{"node with one outgoing", "B", 1, []string{"C"}},
		{"node with no outgoing", "C", 0, nil},
		{"nonexistent node", "Z", 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := g.OutgoingEdges(tt.nodeID)
			if len(edges) != tt.wantLen {
				t.Errorf("OutgoingEdges(%q) returned %d edges, want %d", tt.nodeID, len(edges), tt.wantLen)
			}
			for i, e := range edges {
				if i < len(tt.wantTos) && e.To != tt.wantTos[i] {
					t.Errorf("OutgoingEdges(%q)[%d].To = %q, want %q", tt.nodeID, i, e.To, tt.wantTos[i])
				}
			}
		})
	}
}

func TestGraphIncomingEdges(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"A": {ID: "A", Attrs: map[string]string{}},
			"B": {ID: "B", Attrs: map[string]string{}},
			"C": {ID: "C", Attrs: map[string]string{}},
		},
		Edges: []*Edge{
			{From: "A", To: "B", Attrs: map[string]string{}},
			{From: "A", To: "C", Attrs: map[string]string{}},
			{From: "B", To: "C", Attrs: map[string]string{}},
		},
	}

	tests := []struct {
		name    string
		nodeID  string
		wantLen int
	}{
		{"node with no incoming", "A", 0},
		{"node with one incoming", "B", 1},
		{"node with two incoming", "C", 2},
		{"nonexistent node", "Z", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := g.IncomingEdges(tt.nodeID)
			if len(edges) != tt.wantLen {
				t.Errorf("IncomingEdges(%q) returned %d edges, want %d", tt.nodeID, len(edges), tt.wantLen)
			}
		})
	}
}

func TestGraphFindStartNode(t *testing.T) {
	tests := []struct {
		name    string
		graph   *Graph
		wantID  string
		wantNil bool
	}{
		{
			name: "find by Mdiamond shape",
			graph: &Graph{
				Nodes: map[string]*Node{
					"begin": {ID: "begin", Attrs: map[string]string{"shape": "Mdiamond"}},
					"work":  {ID: "work", Attrs: map[string]string{"shape": "box"}},
				},
			},
			wantID: "begin",
		},
		{
			name: "no start node",
			graph: &Graph{
				Nodes: map[string]*Node{
					"work": {ID: "work", Attrs: map[string]string{"shape": "box"}},
				},
			},
			wantNil: true,
		},
		{
			name: "empty graph",
			graph: &Graph{
				Nodes: map[string]*Node{},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := tt.graph.FindStartNode()
			if tt.wantNil && node != nil {
				t.Errorf("FindStartNode() = %v, want nil", node)
			}
			if !tt.wantNil && node == nil {
				t.Errorf("FindStartNode() = nil, want node with ID %q", tt.wantID)
			}
			if !tt.wantNil && node != nil && node.ID != tt.wantID {
				t.Errorf("FindStartNode().ID = %q, want %q", node.ID, tt.wantID)
			}
		})
	}
}

func TestGraphFindExitNode(t *testing.T) {
	tests := []struct {
		name    string
		graph   *Graph
		wantID  string
		wantNil bool
	}{
		{
			name: "find by Msquare shape",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			wantID: "end",
		},
		{
			name: "no exit node",
			graph: &Graph{
				Nodes: map[string]*Node{
					"work": {ID: "work", Attrs: map[string]string{"shape": "box"}},
				},
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := tt.graph.FindExitNode()
			if tt.wantNil && node != nil {
				t.Errorf("FindExitNode() = %v, want nil", node)
			}
			if !tt.wantNil && node == nil {
				t.Errorf("FindExitNode() = nil, want node with ID %q", tt.wantID)
			}
			if !tt.wantNil && node != nil && node.ID != tt.wantID {
				t.Errorf("FindExitNode().ID = %q, want %q", node.ID, tt.wantID)
			}
		})
	}
}

func TestGraphNodeIDs(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{
			"alpha": {ID: "alpha", Attrs: map[string]string{}},
			"beta":  {ID: "beta", Attrs: map[string]string{}},
			"gamma": {ID: "gamma", Attrs: map[string]string{}},
		},
	}

	ids := g.NodeIDs()
	if len(ids) != 3 {
		t.Fatalf("NodeIDs() returned %d IDs, want 3", len(ids))
	}

	// Check all IDs are present (order not guaranteed from map)
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	for _, want := range []string{"alpha", "beta", "gamma"} {
		if !idSet[want] {
			t.Errorf("NodeIDs() missing %q", want)
		}
	}
}

func TestGraphNodeIDsEmpty(t *testing.T) {
	g := &Graph{
		Nodes: map[string]*Node{},
	}

	ids := g.NodeIDs()
	if len(ids) != 0 {
		t.Errorf("NodeIDs() on empty graph returned %d IDs, want 0", len(ids))
	}
}
