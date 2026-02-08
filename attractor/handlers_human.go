// ABOUTME: Wait for human handler for the attractor pipeline runner.
// ABOUTME: Presents choices derived from outgoing edges to a human via the Interviewer interface.
package attractor

import (
	"context"
	"strings"
)

// WaitForHumanHandler handles human gate nodes (shape=hexagon).
// It presents choices derived from outgoing edges to a human via the
// Interviewer interface and returns their selection.
type WaitForHumanHandler struct {
	// Interviewer is the human interaction frontend. If nil, the handler
	// returns a failure indicating no interviewer is available.
	Interviewer Interviewer
}

// Type returns the handler type string "wait.human".
func (h *WaitForHumanHandler) Type() string {
	return "wait.human"
}

// Execute presents choices to a human and returns their selection.
// Choices are derived from outgoing edges of the node.
func (h *WaitForHumanHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Get the graph from context to find outgoing edges
	var edges []*Edge
	if graphVal := pctx.Get("_graph"); graphVal != nil {
		if g, ok := graphVal.(*Graph); ok {
			edges = g.OutgoingEdges(node.ID)
		}
	}

	if len(edges) == 0 {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "No outgoing edges for human gate: " + node.ID,
		}, nil
	}

	// Build options from edge labels
	options := make([]string, 0, len(edges))
	edgeMap := make(map[string]*Edge) // normalized label -> edge
	for _, e := range edges {
		label := e.Attrs["label"]
		if label == "" {
			label = e.To
		}
		options = append(options, label)
		edgeMap[normalizeLabel(label)] = e
	}

	// Check for interviewer
	if h.Interviewer == nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "No interviewer available for human gate: " + node.ID,
		}, nil
	}

	// Build question
	question := node.Attrs["label"]
	if question == "" {
		question = "Select an option:"
	}

	// Ask the human
	answer, err := h.Interviewer.Ask(ctx, question, options)
	if err != nil {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "Interviewer error: " + err.Error(),
		}, nil
	}

	// Find matching edge by normalized answer
	normalizedAnswer := normalizeLabel(answer)
	var selectedEdge *Edge
	for normLabel, e := range edgeMap {
		if normLabel == normalizedAnswer {
			selectedEdge = e
			break
		}
	}

	// Fallback: try matching by accelerator key
	if selectedEdge == nil {
		for _, e := range edges {
			label := e.Attrs["label"]
			if label == "" {
				label = e.To
			}
			key := parseAcceleratorKey(label)
			if strings.EqualFold(key, answer) {
				selectedEdge = e
				break
			}
		}
	}

	// Fallback: first edge
	if selectedEdge == nil && len(edges) > 0 {
		selectedEdge = edges[0]
	}

	selectedLabel := selectedEdge.Attrs["label"]
	if selectedLabel == "" {
		selectedLabel = selectedEdge.To
	}
	selectedKey := parseAcceleratorKey(selectedLabel)

	return &Outcome{
		Status:           StatusSuccess,
		SuggestedNextIDs: []string{selectedEdge.To},
		Notes:            "Human selected: " + selectedLabel,
		ContextUpdates: map[string]any{
			"human.gate.selected": selectedKey,
			"human.gate.label":    selectedLabel,
		},
	}, nil
}

// normalizeLabel lowercases, trims whitespace, and strips accelerator prefixes.
func normalizeLabel(label string) string {
	s := strings.TrimSpace(strings.ToLower(label))
	// Strip accelerator prefixes: [K] , K) , K -
	if len(s) >= 4 && s[0] == '[' && s[2] == ']' && s[3] == ' ' {
		s = strings.TrimSpace(s[4:])
	} else if len(s) >= 3 && s[1] == ')' && s[2] == ' ' {
		s = strings.TrimSpace(s[3:])
	} else if len(s) >= 4 && s[1] == ' ' && s[2] == '-' && s[3] == ' ' {
		s = strings.TrimSpace(s[4:])
	}
	return s
}

// parseAcceleratorKey extracts shortcut keys from edge labels.
// Patterns: [K] Label -> K, K) Label -> K, K - Label -> K, Label -> first char
func parseAcceleratorKey(label string) string {
	s := strings.TrimSpace(label)
	if s == "" {
		return ""
	}
	// [K] Label
	if len(s) >= 4 && s[0] == '[' && s[2] == ']' {
		return string(s[1])
	}
	// K) Label
	if len(s) >= 2 && s[1] == ')' {
		return string(s[0])
	}
	// K - Label
	if len(s) >= 4 && s[1] == ' ' && s[2] == '-' {
		return string(s[0])
	}
	// First character
	return string(s[0])
}
