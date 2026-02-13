// ABOUTME: CSS-like model stylesheet parser and applicator for assigning LLM properties to graph nodes.
// ABOUTME: Supports universal (*), class (.name), and ID (#name) selectors with specificity-based resolution.
package attractor

import (
	"fmt"
	"strings"
	"unicode"
)

// StyleRule represents a single CSS-like rule with a selector, properties, and specificity.
type StyleRule struct {
	Selector    string
	Properties  map[string]string
	Specificity int
}

// Stylesheet is a collection of style rules parsed from a CSS-like DSL.
type Stylesheet struct {
	Rules []StyleRule
}

// ParseStylesheet parses a CSS-like stylesheet string into a Stylesheet.
// Supported selectors: * (universal, specificity=0), .class (specificity=1), #id (specificity=2).
func ParseStylesheet(input string) (*Stylesheet, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("empty stylesheet")
	}

	ss := &Stylesheet{}
	rest := trimmed

	for rest != "" {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}

		// Parse selector
		braceIdx := strings.Index(rest, "{")
		if braceIdx < 0 {
			return nil, fmt.Errorf("expected '{' in stylesheet")
		}
		selector := strings.TrimSpace(rest[:braceIdx])
		if selector == "" {
			return nil, fmt.Errorf("empty selector")
		}

		// Validate selector
		specificity, err := selectorSpecificity(selector)
		if err != nil {
			return nil, err
		}

		rest = rest[braceIdx+1:]

		// Parse properties until closing brace
		closeBraceIdx := strings.Index(rest, "}")
		if closeBraceIdx < 0 {
			return nil, fmt.Errorf("expected '}' to close rule for selector %q", selector)
		}
		propsStr := rest[:closeBraceIdx]
		rest = rest[closeBraceIdx+1:]

		props, err := parseProperties(propsStr)
		if err != nil {
			return nil, fmt.Errorf("parsing properties for %q: %w", selector, err)
		}

		ss.Rules = append(ss.Rules, StyleRule{
			Selector:    selector,
			Properties:  props,
			Specificity: specificity,
		})
	}

	if len(ss.Rules) == 0 {
		return nil, fmt.Errorf("no rules found in stylesheet")
	}

	return ss, nil
}

// selectorSpecificity returns the specificity for a selector and validates it.
func selectorSpecificity(selector string) (int, error) {
	if selector == "*" {
		return 0, nil
	}
	if strings.HasPrefix(selector, ".") {
		name := selector[1:]
		if name == "" || !isValidIdentifier(name) {
			return 0, fmt.Errorf("invalid class selector %q", selector)
		}
		return 1, nil
	}
	if strings.HasPrefix(selector, "#") {
		name := selector[1:]
		if name == "" || !isValidIdentifier(name) {
			return 0, fmt.Errorf("invalid ID selector %q", selector)
		}
		return 2, nil
	}
	return 0, fmt.Errorf("invalid selector %q: must be *, .class, or #id", selector)
}

// isValidIdentifier checks if a string is a valid CSS-like identifier.
func isValidIdentifier(s string) bool {
	for i, r := range s {
		if i == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return len(s) > 0
}

// parseProperties parses semicolon-delimited "key: value;" property declarations.
func parseProperties(s string) (map[string]string, error) {
	props := make(map[string]string)
	parts := strings.Split(s, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		colonIdx := strings.Index(part, ":")
		if colonIdx < 0 {
			return nil, fmt.Errorf("expected ':' in property declaration %q", part)
		}
		key := strings.TrimSpace(part[:colonIdx])
		val := strings.TrimSpace(part[colonIdx+1:])
		if key == "" {
			return nil, fmt.Errorf("empty property name in %q", part)
		}
		props[key] = val
	}
	return props, nil
}

// Apply applies the stylesheet rules to all nodes in the graph.
// Higher specificity rules override lower ones. Explicit node attributes override all.
func (ss *Stylesheet) Apply(g *Graph) {
	for _, node := range g.Nodes {
		resolved := ss.MatchNode(node)
		for key, val := range resolved {
			// Explicit node attributes take precedence over stylesheet.
			if _, exists := node.Attrs[key]; !exists {
				node.Attrs[key] = val
			}
		}
	}
}

// MatchNode resolves all properties that apply to a node from the stylesheet rules,
// applying specificity ordering (higher specificity overrides lower).
func (ss *Stylesheet) MatchNode(node *Node) map[string]string {
	resolved := make(map[string]string)
	// Track the specificity that set each property.
	specMap := make(map[string]int)

	for _, rule := range ss.Rules {
		if !selectorMatches(rule.Selector, node) {
			continue
		}
		for key, val := range rule.Properties {
			prevSpec, exists := specMap[key]
			if !exists || rule.Specificity >= prevSpec {
				resolved[key] = val
				specMap[key] = rule.Specificity
			}
		}
	}

	return resolved
}

// selectorMatches checks whether a CSS-like selector matches a node.
func selectorMatches(selector string, node *Node) bool {
	if selector == "*" {
		return true
	}
	if strings.HasPrefix(selector, "#") {
		return node.ID == selector[1:]
	}
	if strings.HasPrefix(selector, ".") {
		className := selector[1:]
		nodeClass := node.Attrs["class"]
		if nodeClass == "" {
			return false
		}
		// Support comma-separated classes.
		classes := strings.Split(nodeClass, ",")
		for _, c := range classes {
			if strings.TrimSpace(c) == className {
				return true
			}
		}
		return false
	}
	return false
}
