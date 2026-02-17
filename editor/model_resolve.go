// ABOUTME: Resolves which LLM model applies to a graph node from direct attributes or stylesheet rules.
// ABOUTME: Provides lightweight stylesheet parsing for the editor without importing the attractor package.

package editor

import (
	"fmt"
	"strings"

	"github.com/2389-research/mammoth/dot"
)

// resolveNodeModel determines which LLM model applies to a node.
// It checks (in priority order):
//  1. Direct llm_model attribute on the node (highest priority)
//  2. Stylesheet rules from the graph's model_stylesheet attribute
//
// Returns the model ID and a human-readable source description.
// Returns ("", "") if no model is configured.
func resolveNodeModel(node *dot.Node, stylesheet string) (model string, source string) {
	// Direct attribute takes highest priority
	if m, ok := node.Attrs["llm_model"]; ok && strings.TrimSpace(m) != "" {
		return strings.TrimSpace(m), "node attribute"
	}

	// Parse stylesheet
	if strings.TrimSpace(stylesheet) == "" {
		return "", ""
	}

	rules, err := parseModelStylesheet(stylesheet)
	if err != nil {
		return "", ""
	}

	// Apply rules in specificity order
	var bestModel, bestSelector string
	bestSpec := -1

	for _, rule := range rules {
		if !stylesheetSelectorMatches(rule.selector, node) {
			continue
		}
		if rule.specificity >= bestSpec {
			if m, ok := rule.properties["llm_model"]; ok {
				bestModel = m
				bestSelector = rule.selector
				bestSpec = rule.specificity
			}
		}
	}

	if bestModel != "" {
		return bestModel, fmt.Sprintf("stylesheet (%s rule)", bestSelector)
	}
	return "", ""
}

type stylesheetRule struct {
	selector    string
	properties  map[string]string
	specificity int
}

// parseModelStylesheet parses a CSS-like model_stylesheet string.
func parseModelStylesheet(input string) ([]stylesheetRule, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("empty stylesheet")
	}

	var rules []stylesheetRule
	rest := trimmed

	for rest != "" {
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}

		braceIdx := strings.Index(rest, "{")
		if braceIdx < 0 {
			break
		}
		selector := strings.TrimSpace(rest[:braceIdx])
		rest = rest[braceIdx+1:]

		closeBraceIdx := strings.Index(rest, "}")
		if closeBraceIdx < 0 {
			break
		}
		propsStr := rest[:closeBraceIdx]
		rest = rest[closeBraceIdx+1:]

		props := make(map[string]string)
		for _, part := range strings.Split(propsStr, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			colonIdx := strings.Index(part, ":")
			if colonIdx < 0 {
				continue
			}
			key := strings.TrimSpace(part[:colonIdx])
			val := strings.TrimSpace(part[colonIdx+1:])
			if key != "" {
				props[key] = val
			}
		}

		spec := 0
		if strings.HasPrefix(selector, "#") {
			spec = 2
		} else if strings.HasPrefix(selector, ".") {
			spec = 1
		}

		rules = append(rules, stylesheetRule{
			selector:    selector,
			properties:  props,
			specificity: spec,
		})
	}
	return rules, nil
}

// stylesheetSelectorMatches checks if a CSS-like selector matches a node.
func stylesheetSelectorMatches(selector string, node *dot.Node) bool {
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
		for _, c := range strings.Split(nodeClass, ",") {
			if strings.TrimSpace(c) == className {
				return true
			}
		}
	}
	return false
}
