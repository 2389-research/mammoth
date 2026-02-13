// ABOUTME: Type aliases bridging attractor/ to the consolidated dot/ package.
// ABOUTME: Provides backward-compatible Graph, Node, Edge, Subgraph types and Parse wrapper.
package attractor

import "github.com/2389-research/mammoth/dot"

// Type aliases: all attractor code continues to use Graph, Node, Edge, Subgraph
// without any changes, but the actual types come from the dot/ package.
type Graph = dot.Graph
type Node = dot.Node
type Edge = dot.Edge
type Subgraph = dot.Subgraph

// Parse delegates to the consolidated dot/ parser. This maintains backward
// compatibility for all callers within attractor/ and its tests.
func Parse(input string) (*Graph, error) {
	return dot.Parse(input)
}
