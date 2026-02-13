// ABOUTME: Converts attractor Graph structures to DOT text and renders to SVG/PNG via graphviz.
// ABOUTME: Provides ToDOT, ToDOTWithStatus (with execution status color overlay), and Render functions.
package render

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/2389-research/mammoth/attractor"
)

// Status color constants used for node fill colors in status overlay rendering.
const (
	StatusColorSuccess = "#4CAF50" // green
	StatusColorFailed  = "#F44336" // red
	StatusColorRunning = "#FFC107" // yellow
	StatusColorPending = "#9E9E9E" // gray
)

// ToDOT serializes an attractor Graph back into valid DOT digraph text.
// Node order is deterministic (sorted by ID) for reproducible output.
func ToDOT(g *attractor.Graph) string {
	if g == nil {
		return ""
	}

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("digraph %s {\n", g.Name))

	// Graph-level attributes
	writeAttrsBlock(&buf, g.Attrs, "")

	// Node defaults
	if len(g.NodeDefaults) > 0 {
		buf.WriteString(fmt.Sprintf("  node [%s]\n", formatAttrs(g.NodeDefaults)))
	}

	// Edge defaults
	if len(g.EdgeDefaults) > 0 {
		buf.WriteString(fmt.Sprintf("  edge [%s]\n", formatAttrs(g.EdgeDefaults)))
	}

	// Subgraphs
	for _, sg := range g.Subgraphs {
		writeSubgraph(&buf, sg)
	}

	// Nodes in sorted order for determinism
	nodeIDs := g.NodeIDs()
	for _, id := range nodeIDs {
		node := g.Nodes[id]
		writeNode(&buf, node, nil)
	}

	// Edges
	for _, edge := range g.Edges {
		writeEdge(&buf, edge)
	}

	buf.WriteString("}\n")
	return buf.String()
}

// ToDOTWithStatus serializes a Graph to DOT text with color overlays based on execution status.
// Nodes with outcomes are colored: green for success/partial_success, red for fail,
// yellow for retry (running), and gray for pending (no outcome or skipped).
func ToDOTWithStatus(g *attractor.Graph, outcomes map[string]*attractor.Outcome) string {
	if g == nil {
		return ""
	}

	if outcomes == nil {
		outcomes = map[string]*attractor.Outcome{}
	}

	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("digraph %s {\n", g.Name))

	// Graph-level attributes
	writeAttrsBlock(&buf, g.Attrs, "")

	// Node defaults
	if len(g.NodeDefaults) > 0 {
		buf.WriteString(fmt.Sprintf("  node [%s]\n", formatAttrs(g.NodeDefaults)))
	}

	// Edge defaults
	if len(g.EdgeDefaults) > 0 {
		buf.WriteString(fmt.Sprintf("  edge [%s]\n", formatAttrs(g.EdgeDefaults)))
	}

	// Subgraphs
	for _, sg := range g.Subgraphs {
		writeSubgraph(&buf, sg)
	}

	// Nodes in sorted order with status coloring
	nodeIDs := g.NodeIDs()
	for _, id := range nodeIDs {
		node := g.Nodes[id]
		statusAttrs := statusAttrsForNode(id, outcomes)
		writeNode(&buf, node, statusAttrs)
	}

	// Edges
	for _, edge := range g.Edges {
		writeEdge(&buf, edge)
	}

	buf.WriteString("}\n")
	return buf.String()
}

// Render produces rendered output from a Graph in the specified format.
// Supported formats: "dot" (returns DOT text), "svg", "png" (shell out to graphviz dot command).
// Returns an error if the format is unsupported or graphviz is not installed for svg/png.
func Render(ctx context.Context, g *attractor.Graph, format string) ([]byte, error) {
	if g == nil {
		return nil, fmt.Errorf("cannot render nil graph")
	}

	switch format {
	case "dot":
		return []byte(ToDOT(g)), nil
	case "svg", "png":
		return renderWithGraphviz(ctx, g, format)
	default:
		return nil, fmt.Errorf("unsupported format %q: supported formats are dot, svg, png", format)
	}
}

// GraphvizAvailable checks whether the graphviz dot command is installed and reachable.
func GraphvizAvailable() bool {
	return graphvizAvailable()
}

// graphvizAvailable checks whether the graphviz dot command is installed and reachable.
func graphvizAvailable() bool {
	_, err := exec.LookPath("dot")
	return err == nil
}

// RenderDOTSource takes raw DOT text and renders it to the specified format (svg, png).
// For "dot" format, it returns the input text as-is.
// This is useful when the DOT text has been augmented (e.g. with status colors) and
// should not be re-parsed before rendering.
func RenderDOTSource(ctx context.Context, dotText string, format string) ([]byte, error) {
	if dotText == "" {
		return nil, fmt.Errorf("cannot render empty DOT text")
	}

	switch format {
	case "dot":
		return []byte(dotText), nil
	case "svg", "png":
		return renderDOTSourceWithGraphviz(ctx, dotText, format)
	default:
		return nil, fmt.Errorf("unsupported format %q: supported formats are dot, svg, png", format)
	}
}

// renderWithGraphviz pipes DOT text to the graphviz dot command and returns the output.
func renderWithGraphviz(ctx context.Context, g *attractor.Graph, format string) ([]byte, error) {
	if !graphvizAvailable() {
		return nil, fmt.Errorf("graphviz dot command not found: install graphviz to render %s output", format)
	}

	dotText := ToDOT(g)
	return renderDOTSourceWithGraphviz(ctx, dotText, format)
}

// renderDOTSourceWithGraphviz pipes raw DOT text to the graphviz dot command and returns the output.
func renderDOTSourceWithGraphviz(ctx context.Context, dotText string, format string) ([]byte, error) {
	if !graphvizAvailable() {
		return nil, fmt.Errorf("graphviz dot command not found: install graphviz to render %s output", format)
	}

	cmd := exec.CommandContext(ctx, "dot", "-T"+format)
	cmd.Stdin = strings.NewReader(dotText)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("graphviz dot command failed: %w: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// statusAttrsForNode returns fill color and style attributes based on the node's execution outcome.
func statusAttrsForNode(nodeID string, outcomes map[string]*attractor.Outcome) map[string]string {
	color := StatusColorPending

	if outcome, ok := outcomes[nodeID]; ok {
		switch outcome.Status {
		case attractor.StatusSuccess, attractor.StatusPartialSuccess:
			color = StatusColorSuccess
		case attractor.StatusFail:
			color = StatusColorFailed
		case attractor.StatusRetry:
			color = StatusColorRunning
		case attractor.StatusSkipped:
			color = StatusColorPending
		default:
			color = StatusColorPending
		}
	}

	return map[string]string{
		"style":     "filled",
		"fillcolor": color,
	}
}

// writeNode writes a node declaration to the buffer, merging the node's own attributes
// with any extra attributes (e.g. status coloring).
func writeNode(buf *strings.Builder, node *attractor.Node, extraAttrs map[string]string) {
	merged := make(map[string]string)
	for k, v := range node.Attrs {
		merged[k] = v
	}
	for k, v := range extraAttrs {
		merged[k] = v
	}

	if len(merged) == 0 {
		fmt.Fprintf(buf, "  %s;\n", quoteID(node.ID))
		return
	}

	fmt.Fprintf(buf, "  %s [%s]\n", quoteID(node.ID), formatAttrs(merged))
}

// writeEdge writes an edge declaration to the buffer.
func writeEdge(buf *strings.Builder, edge *attractor.Edge) {
	if len(edge.Attrs) == 0 {
		fmt.Fprintf(buf, "  %s -> %s\n", quoteID(edge.From), quoteID(edge.To))
		return
	}

	fmt.Fprintf(buf, "  %s -> %s [%s]\n", quoteID(edge.From), quoteID(edge.To), formatAttrs(edge.Attrs))
}

// writeSubgraph writes a subgraph block to the buffer.
func writeSubgraph(buf *strings.Builder, sg *attractor.Subgraph) {
	fmt.Fprintf(buf, "  subgraph %s {\n", sg.Name)

	// Subgraph attributes
	writeAttrsBlock(buf, sg.Attrs, "  ")

	// Node defaults within subgraph scope
	if len(sg.NodeDefaults) > 0 {
		fmt.Fprintf(buf, "    node [%s]\n", formatAttrs(sg.NodeDefaults))
	}

	// Nodes in this subgraph
	for _, nodeID := range sg.NodeIDs {
		fmt.Fprintf(buf, "    %s;\n", quoteID(nodeID))
	}

	buf.WriteString("  }\n")
}

// writeAttrsBlock writes graph-level or subgraph-level attributes as individual lines.
func writeAttrsBlock(buf *strings.Builder, attrs map[string]string, indent string) {
	if len(attrs) == 0 {
		return
	}

	keys := sortedKeys(attrs)
	for _, k := range keys {
		fmt.Fprintf(buf, "  %s%s=%q\n", indent, k, attrs[k])
	}
}

// formatAttrs formats a map of attributes as a DOT attribute list (key="value", key="value").
func formatAttrs(attrs map[string]string) string {
	keys := sortedKeys(attrs)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, attrs[k]))
	}
	return strings.Join(parts, ", ")
}

// quoteID returns a DOT-safe identifier. Simple identifiers are returned as-is,
// identifiers with spaces or special characters are quoted.
func quoteID(id string) string {
	// Simple alphanumeric + underscore identifiers don't need quoting
	for _, c := range id {
		if !isIDChar(c) {
			return fmt.Sprintf("%q", id)
		}
	}
	return id
}

// isIDChar returns true if the rune is valid in a bare DOT identifier.
func isIDChar(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// sortedKeys returns the keys of a map in sorted order for deterministic output.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
