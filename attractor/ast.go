// ABOUTME: AST types for the DOT digraph model used by the attractor pipeline runner.
// ABOUTME: Defines Graph, Node, Edge, and Subgraph types with helper methods for traversal and lookup.
package attractor

import (
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

// Edge represents a directed edge from one node to another with optional attributes.
type Edge struct {
	From  string
	To    string
	Attrs map[string]string
}

// Subgraph represents a subgraph scope containing nodes and scoped defaults.
type Subgraph struct {
	Name         string
	Nodes        []string          // node IDs in this subgraph
	NodeDefaults map[string]string // scoped node defaults
	Attrs        map[string]string // subgraph attributes
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

// FindStartNode returns the node with shape=Mdiamond, or nil if not found.
func (g *Graph) FindStartNode() *Node {
	for _, node := range g.Nodes {
		if node.Attrs["shape"] == "Mdiamond" {
			return node
		}
	}
	return nil
}

// FindExitNode returns the node with shape=Msquare, or nil if not found.
func (g *Graph) FindExitNode() *Node {
	for _, node := range g.Nodes {
		if node.Attrs["shape"] == "Msquare" {
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
