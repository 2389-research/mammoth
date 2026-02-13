// ABOUTME: Sub-pipeline composition for inlining child DOT graphs into a parent pipeline.
// ABOUTME: Provides LoadSubPipeline, ComposeGraphs, and SubPipelineTransform for graph merging with namespace isolation.
package attractor

import (
	"fmt"
	"os"
)

// LoadSubPipeline reads a DOT file from disk and parses it into a Graph.
func LoadSubPipeline(path string) (*Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read sub-pipeline file %q: %w", path, err)
	}

	g, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse sub-pipeline file %q: %w", path, err)
	}

	return g, nil
}

// ComposeGraphs merges a child graph into a parent graph by replacing the node
// identified by insertNodeID. Child node IDs are prefixed with "{namespace}."
// to avoid ID conflicts. Parent edges pointing to or from the insert node are
// reconnected to the child's start and terminal nodes respectively.
// Child graph attributes are copied, but parent attributes take precedence on conflicts.
func ComposeGraphs(parent *Graph, childGraph *Graph, insertNodeID string, namespace string) (*Graph, error) {
	// Validate that the insert node exists in the parent
	if parent.FindNode(insertNodeID) == nil {
		return nil, fmt.Errorf("insert node %q not found in parent graph", insertNodeID)
	}

	// Find the child's start and terminal nodes
	childStart := childGraph.FindStartNode()
	if childStart == nil {
		return nil, fmt.Errorf("child graph has no start node (shape=Mdiamond)")
	}

	childTerminal := childGraph.FindExitNode()
	if childTerminal == nil {
		return nil, fmt.Errorf("child graph has no terminal node (shape=Msquare)")
	}

	// Build the namespaced child start and terminal IDs
	namespacedStartID := namespace + "." + childStart.ID
	namespacedTerminalID := namespace + "." + childTerminal.ID

	// Create the result graph as a copy of the parent
	result := &Graph{
		Name:         parent.Name,
		Nodes:        make(map[string]*Node, len(parent.Nodes)+len(childGraph.Nodes)-1),
		Edges:        make([]*Edge, 0, len(parent.Edges)+len(childGraph.Edges)),
		Attrs:        make(map[string]string, len(parent.Attrs)+len(childGraph.Attrs)),
		NodeDefaults: make(map[string]string, len(parent.NodeDefaults)),
		EdgeDefaults: make(map[string]string, len(parent.EdgeDefaults)),
		Subgraphs:    make([]*Subgraph, len(parent.Subgraphs)),
	}

	// Copy child graph attributes first, so parent can override
	for k, v := range childGraph.Attrs {
		result.Attrs[k] = v
	}
	// Copy parent graph attributes (takes precedence on conflict)
	for k, v := range parent.Attrs {
		result.Attrs[k] = v
	}

	// Copy parent node defaults
	for k, v := range parent.NodeDefaults {
		result.NodeDefaults[k] = v
	}

	// Copy parent edge defaults
	for k, v := range parent.EdgeDefaults {
		result.EdgeDefaults[k] = v
	}

	// Copy parent subgraphs
	copy(result.Subgraphs, parent.Subgraphs)

	// Copy parent nodes, except the insert node
	for id, node := range parent.Nodes {
		if id == insertNodeID {
			continue
		}
		result.Nodes[id] = copyNode(node)
	}

	// Copy child nodes with namespace prefix
	for id, node := range childGraph.Nodes {
		namespacedID := namespace + "." + id
		nsNode := &Node{
			ID:    namespacedID,
			Attrs: make(map[string]string, len(node.Attrs)),
		}
		for k, v := range node.Attrs {
			nsNode.Attrs[k] = v
		}
		result.Nodes[namespacedID] = nsNode
	}

	// Process parent edges: reconnect edges that reference the insert node
	for _, edge := range parent.Edges {
		newEdge := &Edge{
			From:  edge.From,
			To:    edge.To,
			Attrs: copyAttrs(edge.Attrs),
		}

		if edge.To == insertNodeID {
			// Incoming edge to insert node -> reconnect to child start
			newEdge.To = namespacedStartID
		}
		if edge.From == insertNodeID {
			// Outgoing edge from insert node -> reconnect to child terminal
			newEdge.From = namespacedTerminalID
		}

		result.Edges = append(result.Edges, newEdge)
	}

	// Copy child edges with namespace prefix
	for _, edge := range childGraph.Edges {
		nsEdge := &Edge{
			From:  namespace + "." + edge.From,
			To:    namespace + "." + edge.To,
			Attrs: copyAttrs(edge.Attrs),
		}
		result.Edges = append(result.Edges, nsEdge)
	}

	return result, nil
}

// copyNode creates a deep copy of a Node.
func copyNode(n *Node) *Node {
	cp := &Node{
		ID:    n.ID,
		Attrs: make(map[string]string, len(n.Attrs)),
	}
	for k, v := range n.Attrs {
		cp.Attrs[k] = v
	}
	return cp
}

// copyAttrs creates a shallow copy of an attribute map.
func copyAttrs(attrs map[string]string) map[string]string {
	if attrs == nil {
		return map[string]string{}
	}
	cp := make(map[string]string, len(attrs))
	for k, v := range attrs {
		cp[k] = v
	}
	return cp
}

// SubPipelineTransform is a Transform that scans for nodes with a "sub_pipeline"
// attribute, loads the referenced DOT file, and composes it into the graph.
// The insert node's ID is used as the namespace to avoid conflicts.
type SubPipelineTransform struct{}

// Apply scans the graph for nodes with a sub_pipeline attribute and inlines
// the referenced child graphs. If a sub-pipeline file cannot be loaded or
// composed, the node is left intact and the error is silently skipped.
func (t *SubPipelineTransform) Apply(g *Graph) *Graph {
	// Collect node IDs with sub_pipeline attributes up front, since we
	// modify the graph during iteration.
	type subPipelineRef struct {
		nodeID string
		path   string
	}
	var refs []subPipelineRef

	for _, node := range g.Nodes {
		if path, ok := node.Attrs["sub_pipeline"]; ok && path != "" {
			refs = append(refs, subPipelineRef{nodeID: node.ID, path: path})
		}
	}

	if len(refs) == 0 {
		return g
	}

	result := g
	for _, ref := range refs {
		childGraph, err := LoadSubPipeline(ref.path)
		if err != nil {
			// File not found or parse error: leave the node intact
			continue
		}

		composed, err := ComposeGraphs(result, childGraph, ref.nodeID, ref.nodeID)
		if err != nil {
			// Composition error: leave the node intact
			continue
		}

		result = composed
	}

	return result
}
