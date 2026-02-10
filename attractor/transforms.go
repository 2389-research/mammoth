// ABOUTME: AST transforms applied between parsing and validation for the pipeline graph.
// ABOUTME: Implements variable expansion ($goal) and stylesheet application as a transform chain.
package attractor

import (
	"strings"
)

// Transform is the interface for AST transformations applied to a parsed graph.
type Transform interface {
	Apply(g *Graph) *Graph
}

// ApplyTransforms applies a sequence of transforms to a graph, returning the final result.
func ApplyTransforms(g *Graph, transforms ...Transform) *Graph {
	result := g
	for _, t := range transforms {
		result = t.Apply(result)
	}
	return result
}

// DefaultTransforms returns the standard ordered transform chain.
func DefaultTransforms() []Transform {
	return []Transform{
		&SubPipelineTransform{},
		&VariableExpansionTransform{},
		&StylesheetApplicationTransform{},
	}
}

// VariableExpansionTransform expands $variable references in node attributes
// using graph-level attribute values.
type VariableExpansionTransform struct{}

// Apply expands $variable references in node prompt and label attributes.
func (t *VariableExpansionTransform) Apply(g *Graph) *Graph {
	for _, node := range g.Nodes {
		for key, val := range node.Attrs {
			if strings.Contains(val, "$") {
				node.Attrs[key] = expandVariables(val, g.Attrs)
			}
		}
	}
	return g
}

// expandVariables replaces $key references with values from the vars map.
// Only replaces if the key exists in the vars map.
func expandVariables(s string, vars map[string]string) string {
	result := s
	for key, val := range vars {
		result = strings.ReplaceAll(result, "$"+key, val)
	}
	return result
}

// StylesheetApplicationTransform applies the model_stylesheet graph attribute
// to all nodes using CSS-like specificity rules.
type StylesheetApplicationTransform struct{}

// Apply parses and applies the model_stylesheet from graph attributes.
func (t *StylesheetApplicationTransform) Apply(g *Graph) *Graph {
	ssText, ok := g.Attrs["model_stylesheet"]
	if !ok || ssText == "" {
		return g
	}

	ss, err := ParseStylesheet(ssText)
	if err != nil {
		// If stylesheet is invalid, skip application silently.
		return g
	}

	ss.Apply(g)
	return g
}
