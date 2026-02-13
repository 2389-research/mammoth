// ABOUTME: Consolidated AST types for the unified DOT parser, merging mammoth attractor and dot-editor ASTs.
// ABOUTME: Defines Graph, Node, Edge, Subgraph, and Diagnostic types with traversal helpers and edge ID assignment.
package dot

import (
	"fmt"
	"sort"
)

// Graph represents a parsed DOT digraph with its nodes, edges, attributes, and subgraphs.
type Graph struct {
	Name         string
	Nodes        map[string]*Node
	Edges        []*Edge
	Attrs        map[string]string // graph-level attributes
	NodeDefaults map[string]string // node [...] defaults
	EdgeDefaults map[string]string // edge [...] defaults
	Subgraphs    []*Subgraph
}

// Node represents a node in the graph with an ID and key-value attributes.
type Node struct {
	ID    string
	Attrs map[string]string
}

// Edge represents a directed edge from one node to another with an optional ID and attributes.
type Edge struct {
	ID    string
	From  string
	To    string
	Attrs map[string]string
}

// Subgraph represents a subgraph scope containing nodes and scoped defaults.
type Subgraph struct {
	ID           string
	Name         string
	Attrs        map[string]string // subgraph attributes
	NodeIDs      []string          // node IDs in this subgraph
	NodeDefaults map[string]string // scoped node defaults
}

// Diagnostic represents a validation finding associated with a node or edge.
type Diagnostic struct {
	Severity string // "error", "warning", "info"
	Message  string
	NodeID   string
	EdgeID   string
	Rule     string
}

// AddNode adds a node to the graph, initializing the Nodes map if needed.
func (g *Graph) AddNode(n *Node) {
	if g.Nodes == nil {
		g.Nodes = make(map[string]*Node)
	}
	g.Nodes[n.ID] = n
}

// AddEdge appends an edge to the graph.
func (g *Graph) AddEdge(e *Edge) {
	g.Edges = append(g.Edges, e)
}

// FindNode returns the node with the given ID, or nil if not found.
func (g *Graph) FindNode(id string) *Node {
	if g.Nodes == nil {
		return nil
	}
	return g.Nodes[id]
}

// OutgoingEdges returns all edges originating from the given node ID.
func (g *Graph) OutgoingEdges(nodeID string) []*Edge {
	var result []*Edge
	for _, e := range g.Edges {
		if e.From == nodeID {
			result = append(result, e)
		}
	}
	return result
}

// IncomingEdges returns all edges terminating at the given node ID.
func (g *Graph) IncomingEdges(nodeID string) []*Edge {
	var result []*Edge
	for _, e := range g.Edges {
		if e.To == nodeID {
			result = append(result, e)
		}
	}
	return result
}

// FindStartNode returns the start node, or nil if not found.
// Recognized via shape=Mdiamond, node_type=start, or type=start.
func (g *Graph) FindStartNode() *Node {
	for _, node := range g.Nodes {
		if node.Attrs["shape"] == "Mdiamond" {
			return node
		}
		if node.Attrs["node_type"] == "start" || node.Attrs["type"] == "start" {
			return node
		}
	}
	return nil
}

// FindExitNode returns the exit/terminal node, or nil if not found.
// Recognized via shape=Msquare, node_type=exit, or type=exit.
func (g *Graph) FindExitNode() *Node {
	for _, node := range g.Nodes {
		if node.Attrs["shape"] == "Msquare" {
			return node
		}
		if node.Attrs["node_type"] == "exit" || node.Attrs["type"] == "exit" {
			return node
		}
	}
	return nil
}

// NodeIDs returns all node IDs in sorted order for deterministic output.
func (g *Graph) NodeIDs() []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// StableID returns a deterministic identifier for an edge based on its endpoints.
// The format is "from->to", which is stable across parses of the same DOT source.
func (e *Edge) StableID() string {
	return e.From + "->" + e.To
}

// AssignEdgeIDs assigns a unique ID to each edge that does not already have one.
// Edges that already have a non-empty ID are left unchanged. For edges sharing the
// same From->To pair, a numeric suffix disambiguates them.
func (g *Graph) AssignEdgeIDs() {
	counts := make(map[string]int)
	for _, e := range g.Edges {
		if e.ID != "" {
			continue
		}
		key := e.StableID()
		counts[key]++
		if counts[key] == 1 {
			e.ID = key
		} else {
			e.ID = fmt.Sprintf("%s#%d", key, counts[key])
		}
	}
}
