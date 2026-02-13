// ABOUTME: Serializer that converts a Graph AST back to a DOT-formatted source string.
// ABOUTME: Also provides color-coding for pipeline visualization based on node shape conventions.
package dot

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Serialize converts a Graph AST back to a DOT-formatted string with deterministic output.
// Nodes are sorted by ID, and attributes within each element are sorted by key.
func Serialize(g *Graph) string {
	var b strings.Builder

	// digraph header
	name := g.Name
	if needsQuoting(name) {
		name = quoteValue(name)
	}
	fmt.Fprintf(&b, "digraph %s {\n", name)

	// Graph attributes
	if len(g.Attrs) > 0 {
		fmt.Fprintf(&b, "  graph [%s]\n", formatAttrs(g.Attrs))
	}

	// Node defaults
	if len(g.NodeDefaults) > 0 {
		fmt.Fprintf(&b, "  node [%s]\n", formatAttrs(g.NodeDefaults))
	}

	// Edge defaults
	if len(g.EdgeDefaults) > 0 {
		fmt.Fprintf(&b, "  edge [%s]\n", formatAttrs(g.EdgeDefaults))
	}

	// Blank line after defaults if any were emitted
	if len(g.Attrs) > 0 || len(g.NodeDefaults) > 0 || len(g.EdgeDefaults) > 0 {
		b.WriteString("\n")
	}

	// Nodes sorted by ID for deterministic output
	nodeIDs := sortedKeys(g.Nodes)
	for _, id := range nodeIDs {
		node := g.Nodes[id]
		nodeID := id
		if needsQuoting(nodeID) {
			nodeID = quoteValue(nodeID)
		}
		if len(node.Attrs) > 0 {
			fmt.Fprintf(&b, "  %s [%s]\n", nodeID, formatAttrs(node.Attrs))
		} else {
			fmt.Fprintf(&b, "  %s\n", nodeID)
		}
	}

	// Blank line before subgraphs if there are nodes
	if len(nodeIDs) > 0 && len(g.Subgraphs) > 0 {
		b.WriteString("\n")
	}

	// Subgraphs
	for _, sg := range g.Subgraphs {
		sgName := sg.Name
		if sg.ID != "" {
			sgName = sg.ID
		}
		fmt.Fprintf(&b, "  subgraph %s {\n", sgName)

		// Subgraph attributes
		if len(sg.Attrs) > 0 {
			keys := sortedKeys(sg.Attrs)
			for _, k := range keys {
				fmt.Fprintf(&b, "    %s=%s\n", k, quoteValue(sg.Attrs[k]))
			}
		}

		// Scoped node defaults
		if len(sg.NodeDefaults) > 0 {
			fmt.Fprintf(&b, "    node [%s]\n", formatAttrs(sg.NodeDefaults))
		}

		// Node references
		for _, nodeID := range sg.NodeIDs {
			nid := nodeID
			if needsQuoting(nid) {
				nid = quoteValue(nid)
			}
			fmt.Fprintf(&b, "    %s\n", nid)
		}

		b.WriteString("  }\n")
	}

	// Blank line before edges if there are nodes or subgraphs
	if (len(nodeIDs) > 0 || len(g.Subgraphs) > 0) && len(g.Edges) > 0 {
		b.WriteString("\n")
	}

	// Edges
	for _, e := range g.Edges {
		from := e.From
		if needsQuoting(from) {
			from = quoteValue(from)
		}
		to := e.To
		if needsQuoting(to) {
			to = quoteValue(to)
		}
		if len(e.Attrs) > 0 {
			fmt.Fprintf(&b, "  %s -> %s [%s]\n", from, to, formatAttrs(e.Attrs))
		} else {
			fmt.Fprintf(&b, "  %s -> %s\n", from, to)
		}
	}

	b.WriteString("}\n")
	return b.String()
}

// ApplyColorCoding adds fillcolor and style attributes to nodes based on their shape,
// and colors edges based on their label (success/fail conditions).
func ApplyColorCoding(g *Graph) {
	// Shape-to-color mapping for pipeline visualization
	shapeColors := map[string]string{
		"Mdiamond":      "#90EE90", // start → green
		"Msquare":       "#FFB6C1", // exit → red
		"box":           "#ADD8E6", // codergen → blue
		"diamond":       "#FFFFE0", // conditional → yellow
		"hexagon":       "#DDA0DD", // human → purple
		"parallelogram": "#FFA500", // tool → orange
	}

	for _, node := range g.Nodes {
		if node.Attrs == nil {
			continue
		}
		shape := node.Attrs["shape"]
		if color, ok := shapeColors[shape]; ok {
			node.Attrs["fillcolor"] = color
			node.Attrs["style"] = "filled"
		}
	}

	for _, edge := range g.Edges {
		if edge.Attrs == nil {
			continue
		}
		label := strings.ToLower(edge.Attrs["label"])
		if strings.Contains(label, "success") {
			edge.Attrs["color"] = "green"
		} else if strings.Contains(label, "fail") {
			edge.Attrs["color"] = "red"
			edge.Attrs["style"] = "dashed"
		}
	}
}

// formatAttrs renders a map of key=value pairs as a comma-separated string with sorted keys.
func formatAttrs(attrs map[string]string) string {
	keys := sortedKeys(attrs)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, quoteValue(attrs[k])))
	}
	return strings.Join(parts, ", ")
}

// quoteValue returns a DOT-safe representation of a value.
// Simple identifiers (lowercase letters, digits, underscores, dots for numbers) are returned bare.
// Everything else is double-quoted with proper escaping.
func quoteValue(val string) string {
	if val == "" {
		return `""`
	}

	if isBareIdentifier(val) {
		return val
	}

	// Quote the value with escaping
	var b strings.Builder
	b.WriteByte('"')
	for _, ch := range val {
		switch ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// isBareIdentifier returns true if val can be represented without quotes in DOT.
// A bare identifier consists of lowercase letters, digits, underscores, dots,
// and hyphens (with leading minus for negative numbers).
func isBareIdentifier(val string) bool {
	if val == "" {
		return false
	}

	// Check for numeric values (including negatives and floats)
	if isNumeric(val) {
		return true
	}

	// Check for simple bare identifiers
	for _, ch := range val {
		if ch != '_' && !unicode.IsLower(ch) && !unicode.IsDigit(ch) {
			return false
		}
	}
	return true
}

// isNumeric returns true if val looks like a number (integer or float, possibly negative).
func isNumeric(val string) bool {
	if val == "" {
		return false
	}
	start := 0
	if val[0] == '-' {
		if len(val) == 1 {
			return false
		}
		start = 1
	}
	hasDot := false
	hasDigit := false
	for i := start; i < len(val); i++ {
		ch := val[i]
		if ch == '.' {
			if hasDot {
				return false
			}
			hasDot = true
		} else if ch >= '0' && ch <= '9' {
			hasDigit = true
		} else {
			return false
		}
	}
	return hasDigit
}

// needsQuoting returns true if a DOT identifier needs quoting.
func needsQuoting(val string) bool {
	return !isBareIdentifier(val)
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
