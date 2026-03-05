// ABOUTME: Parse command for the conformance CLI, translating dot.Graph to conformance JSON.
// ABOUTME: Reads a DOT file, parses it, and outputs the graph structure as AttractorBench-compatible JSON.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"

	"github.com/2389-research/mammoth/dot"
)

// translateGraphToParseOutput converts a dot.Graph to a ConformanceParseOutput.
// Nodes are sorted by ID for deterministic output. Shape and label are extracted
// as top-level fields; remaining attributes go into the attributes map.
func translateGraphToParseOutput(g *dot.Graph) ConformanceParseOutput {
	nodeIDs := g.NodeIDs()
	nodes := make([]ConformanceNode, 0, len(nodeIDs))

	for _, id := range nodeIDs {
		n := g.Nodes[id]
		cn := ConformanceNode{
			ID:         n.ID,
			Attributes: make(map[string]string),
		}
		for k, v := range n.Attrs {
			switch k {
			case "shape":
				cn.Shape = v
			case "label":
				cn.Label = v
			default:
				cn.Attributes[k] = v
			}
		}
		nodes = append(nodes, cn)
	}

	edges := make([]ConformanceEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		ce := ConformanceEdge{
			From: e.From,
			To:   e.To,
		}
		if label, ok := e.Attrs["label"]; ok {
			ce.Label = label
		}
		if cond, ok := e.Attrs["condition"]; ok {
			ce.Condition = cond
		}
		if weightStr, ok := e.Attrs["weight"]; ok {
			if w, err := strconv.Atoi(weightStr); err == nil {
				ce.Weight = w
			}
		}
		edges = append(edges, ce)
	}

	attrs := make(map[string]string)
	// Copy graph-level attributes, sorting keys for deterministic output
	keys := make([]string, 0, len(g.Attrs))
	for k := range g.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		attrs[k] = g.Attrs[k]
	}

	return ConformanceParseOutput{
		Nodes:      nodes,
		Edges:      edges,
		Attributes: attrs,
	}
}

// cmdParse reads a DOT file, parses it, translates to conformance JSON, and writes to stdout.
// Returns 0 on success, 1 on error.
func cmdParse(dotfile string) int {
	data, err := os.ReadFile(dotfile)
	if err != nil {
		writeError(fmt.Sprintf("reading file: %v", err))
		return 1
	}

	graph, err := dot.Parse(string(data))
	if err != nil {
		writeError(fmt.Sprintf("parsing DOT: %v", err))
		return 1
	}

	output := translateGraphToParseOutput(graph)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		writeError(fmt.Sprintf("encoding JSON: %v", err))
		return 1
	}

	return 0
}

// writeError writes a conformance error JSON object to stdout.
func writeError(msg string) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(ConformanceError{Error: msg})
}
