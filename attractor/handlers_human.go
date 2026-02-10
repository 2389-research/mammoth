// ABOUTME: Wait for human handler for the attractor pipeline runner.
// ABOUTME: Presents choices derived from outgoing edges to a human via the Interviewer interface.
package attractor

import (
	"context"
	"fmt"
	"strings"
	"time"
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
//
// Supports optional node attributes:
//   - timeout: Duration string (e.g. "5m", "1h") limiting how long to wait for human input.
//   - default_choice: Edge label to select if the timeout expires.
//   - reminder_interval: Duration string for periodic re-prompting (parsed and validated,
//     but only effective if the Interviewer implementation supports it).
//
// Context updates always include human.timed_out (bool) and human.response_time_ms (int64).
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

	// Parse timeout attribute
	var timeout time.Duration
	var hasTimeout bool
	if timeoutStr := node.Attrs["timeout"]; timeoutStr != "" {
		var err error
		timeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("Invalid timeout duration %q: %v", timeoutStr, err),
			}, nil
		}
		hasTimeout = true
	}

	// Parse default_choice attribute
	defaultChoice := node.Attrs["default_choice"]

	// Parse and validate reminder_interval attribute
	if riStr := node.Attrs["reminder_interval"]; riStr != "" {
		if _, err := time.ParseDuration(riStr); err != nil {
			return &Outcome{
				Status:        StatusFail,
				FailureReason: fmt.Sprintf("Invalid reminder_interval duration %q: %v", riStr, err),
			}, nil
		}
	}

	// Build question
	question := node.Attrs["label"]
	if question == "" {
		question = "Select an option:"
	}

	// Build the context for the interviewer call, applying timeout if configured
	askCtx := ctx
	var cancelTimeout context.CancelFunc
	if hasTimeout {
		askCtx, cancelTimeout = context.WithTimeout(ctx, timeout)
		defer cancelTimeout()
	}

	// Ask the human and track response time
	startTime := time.Now()
	answer, err := h.Interviewer.Ask(askCtx, question, options)
	elapsed := time.Since(startTime)
	responseTimeMs := elapsed.Milliseconds()

	// Handle timeout: context.DeadlineExceeded from our timeout (not the parent)
	if err != nil && hasTimeout && askCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		return h.handleTimeout(defaultChoice, edges, edgeMap, node, responseTimeMs)
	}

	// Handle parent context cancellation or other errors
	if err != nil {
		// If the parent context was cancelled, propagate as an error
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return &Outcome{
			Status:        StatusFail,
			FailureReason: "Interviewer error: " + err.Error(),
			ContextUpdates: map[string]any{
				"human.timed_out":        false,
				"human.response_time_ms": responseTimeMs,
			},
		}, nil
	}

	// Find matching edge by normalized answer
	selectedEdge := h.findEdgeByAnswer(answer, edges, edgeMap)

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
			"human.gate.selected":    selectedKey,
			"human.gate.label":       selectedLabel,
			"human.timed_out":        false,
			"human.response_time_ms": responseTimeMs,
		},
	}, nil
}

// handleTimeout processes a timeout event, selecting the default_choice edge if
// configured, or returning a failure if no default is set or the default doesn't
// match any edge.
func (h *WaitForHumanHandler) handleTimeout(defaultChoice string, edges []*Edge, edgeMap map[string]*Edge, node *Node, responseTimeMs int64) (*Outcome, error) {
	if defaultChoice == "" {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: fmt.Sprintf("Human gate %q timed out with no default_choice configured", node.ID),
			ContextUpdates: map[string]any{
				"human.timed_out":        true,
				"human.response_time_ms": responseTimeMs,
			},
		}, nil
	}

	// Find the edge matching default_choice
	selectedEdge := h.findEdgeByAnswer(defaultChoice, edges, edgeMap)

	// Verify the selected edge actually matches the default_choice (not just a fallback)
	selectedLabel := selectedEdge.Attrs["label"]
	if selectedLabel == "" {
		selectedLabel = selectedEdge.To
	}
	if normalizeLabel(selectedLabel) != normalizeLabel(defaultChoice) {
		return &Outcome{
			Status:        StatusFail,
			FailureReason: fmt.Sprintf("default_choice %q does not match any outgoing edge of node %q", defaultChoice, node.ID),
			ContextUpdates: map[string]any{
				"human.timed_out":        true,
				"human.response_time_ms": responseTimeMs,
			},
		}, nil
	}

	selectedKey := parseAcceleratorKey(selectedLabel)

	return &Outcome{
		Status:           StatusSuccess,
		PreferredLabel:   defaultChoice,
		SuggestedNextIDs: []string{selectedEdge.To},
		Notes:            fmt.Sprintf("Human gate timed out; selected default choice: %s", defaultChoice),
		ContextUpdates: map[string]any{
			"human.gate.selected":    selectedKey,
			"human.gate.label":       selectedLabel,
			"human.timed_out":        true,
			"human.response_time_ms": responseTimeMs,
		},
	}, nil
}

// findEdgeByAnswer looks up an edge by normalized label match, accelerator key
// match, or falls back to the first edge.
func (h *WaitForHumanHandler) findEdgeByAnswer(answer string, edges []*Edge, edgeMap map[string]*Edge) *Edge {
	// Try normalized label match
	normalizedAnswer := normalizeLabel(answer)
	for normLabel, e := range edgeMap {
		if normLabel == normalizedAnswer {
			return e
		}
	}

	// Fallback: try matching by accelerator key
	for _, e := range edges {
		label := e.Attrs["label"]
		if label == "" {
			label = e.To
		}
		key := parseAcceleratorKey(label)
		if strings.EqualFold(key, answer) {
			return e
		}
	}

	// Fallback: first edge
	if len(edges) > 0 {
		return edges[0]
	}
	return nil
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
