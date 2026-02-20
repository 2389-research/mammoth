// ABOUTME: Edge selection algorithm for choosing the next edge during pipeline graph traversal.
// ABOUTME: Implements five-step priority: condition match > preferred label > suggested IDs > weight > lexical.
package attractor

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// acceleratorPatterns matches accelerator prefixes like "[Y] ", "Y) ", "Y - " at the start of a label.
var acceleratorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\[\w\]\s+`), // [Y] Yes
	regexp.MustCompile(`^\w\)\s*`),   // Y) Yes
	regexp.MustCompile(`^\w\s*-\s+`), // Y - Yes
}

// NormalizeLabel lowercases a label, trims whitespace, and strips accelerator prefixes
// like "[Y] ", "Y) ", "Y - " that are used for keyboard shortcuts in human interaction nodes.
func NormalizeLabel(label string) string {
	s := strings.TrimSpace(label)
	s = strings.ToLower(s)
	for _, pat := range acceleratorPatterns {
		s = pat.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(s)
}

// bestByWeightThenLexical picks the edge with the highest weight attribute.
// If weights are tied, the edge whose To field comes first lexicographically wins.
// Returns nil for an empty slice.
func bestByWeightThenLexical(edges []*Edge) *Edge {
	if len(edges) == 0 {
		return nil
	}

	sort.Slice(edges, func(i, j int) bool {
		wi := edgeWeight(edges[i])
		wj := edgeWeight(edges[j])
		if wi != wj {
			return wi > wj
		}
		return edges[i].To < edges[j].To
	})

	return edges[0]
}

// edgeWeight parses the "weight" attribute of an edge, defaulting to 0.
func edgeWeight(e *Edge) int {
	if e.Attrs == nil {
		return 0
	}
	w, err := strconv.Atoi(e.Attrs["weight"])
	if err != nil {
		return 0
	}
	return w
}

// SelectEdge chooses the next edge from a node using five-step priority:
// 1. Condition-matching edges (non-empty condition that evaluates true), best by weight then lexical
// 2. Preferred label match (outcome.PreferredLabel matches edge label after normalization)
// 3. Suggested next IDs (outcome.SuggestedNextIDs matches edge.To)
// 4. Highest weight among unconditional edges (no condition attribute or empty condition)
// 5. Lexical tiebreak on To field
// Returns nil if no outgoing edges exist.
func SelectEdge(node *Node, outcome *Outcome, ctx *Context, graph *Graph) *Edge {
	edges := graph.OutgoingEdges(node.ID)
	if len(edges) == 0 {
		return nil
	}

	// Step 1: Condition-matching edges
	var condMatches []*Edge
	for _, e := range edges {
		cond, hasCond := e.Attrs["condition"]
		if !hasCond || strings.TrimSpace(cond) == "" {
			continue
		}
		if EvaluateCondition(cond, outcome, ctx) {
			condMatches = append(condMatches, e)
		}
	}
	if len(condMatches) > 0 {
		return bestByWeightThenLexical(condMatches)
	}

	// Step 2: Preferred label match
	if outcome.PreferredLabel != "" {
		normalizedPref := NormalizeLabel(outcome.PreferredLabel)
		for _, e := range edges {
			edgeLabel, ok := e.Attrs["label"]
			if !ok {
				continue
			}
			if NormalizeLabel(edgeLabel) == normalizedPref {
				return e
			}
		}
	}

	// Step 3: Suggested next IDs
	if len(outcome.SuggestedNextIDs) > 0 {
		suggestedSet := make(map[string]bool, len(outcome.SuggestedNextIDs))
		for _, id := range outcome.SuggestedNextIDs {
			suggestedSet[id] = true
		}
		for _, e := range edges {
			if suggestedSet[e.To] {
				return e
			}
		}
	}

	// Steps 4 & 5: Unconditional edges by weight then lexical.
	// Only follow unconditional edges on success/partial_success — a failed node
	// must have an explicit condition="outcome = fail" edge to continue.
	if outcome.Status != StatusFail {
		var unconditional []*Edge
		for _, e := range edges {
			cond, hasCond := e.Attrs["condition"]
			if !hasCond || strings.TrimSpace(cond) == "" {
				unconditional = append(unconditional, e)
			}
		}
		if len(unconditional) > 0 {
			return bestByWeightThenLexical(unconditional)
		}
	}

	// All edges had conditions but none matched -- return nil
	return nil
}
