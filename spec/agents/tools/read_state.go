// ABOUTME: Implements the read_state tool for reading current spec state via mux Tool interface.
// ABOUTME: Formats SpecState into a human-readable text summary for LLM consumption.
package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

// ReadStateTool reads the current spec state and returns a formatted text summary.
type ReadStateTool struct {
	Actor *core.SpecActorHandle
}

func (t *ReadStateTool) Name() string {
	return "read_state"
}

func (t *ReadStateTool) Description() string {
	return "Read the current spec state summary including cards, transcript, and metadata. Returns a text summary of the spec's current state."
}

func (t *ReadStateTool) RequiresApproval(_ map[string]any) bool {
	return false
}

func (t *ReadStateTool) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

func (t *ReadStateTool) Execute(_ context.Context, _ map[string]any) (*tool.Result, error) {
	var lines []string

	t.Actor.ReadState(func(state *core.SpecState) {
		// Spec core metadata
		if state.Core != nil {
			lines = append(lines, fmt.Sprintf("# %s", state.Core.Title))
			lines = append(lines, fmt.Sprintf("One-liner: %s", state.Core.OneLiner))
			lines = append(lines, fmt.Sprintf("Goal: %s", state.Core.Goal))
			if state.Core.Description != nil {
				lines = append(lines, fmt.Sprintf("Description: %s", *state.Core.Description))
			}
			if state.Core.Constraints != nil {
				lines = append(lines, fmt.Sprintf("Constraints: %s", *state.Core.Constraints))
			}
			if state.Core.SuccessCriteria != nil {
				lines = append(lines, fmt.Sprintf("Success Criteria: %s", *state.Core.SuccessCriteria))
			}
			if state.Core.Risks != nil {
				lines = append(lines, fmt.Sprintf("Risks: %s", *state.Core.Risks))
			}
			if state.Core.Notes != nil {
				lines = append(lines, fmt.Sprintf("Notes: %s", *state.Core.Notes))
			}
		} else {
			lines = append(lines, "(No spec created yet)")
		}

		// Lanes
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("## Lanes: %s", strings.Join(state.Lanes, ", ")))

		// Cards summary
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("## Cards (%d)", state.Cards.Len()))
		for _, card := range state.Cards.Values() {
			bodyPreview := ""
			if card.Body != nil {
				bodyPreview = truncateUTF8Safe(*card.Body, 80)
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s (type: %s, lane: %s) %s",
				card.CardID, card.Title, card.CardType, card.Lane, bodyPreview))
		}

		// Pending question
		lines = append(lines, "")
		if state.PendingQuestion != nil {
			lines = append(lines, fmt.Sprintf("## Pending Question: %v", state.PendingQuestion))
		} else {
			lines = append(lines, "## No pending question")
		}

		// Transcript summary
		lines = append(lines, "")
		transcriptLen := len(state.Transcript)
		lines = append(lines, fmt.Sprintf("## Transcript (%d messages)", transcriptLen))
		start := transcriptLen - 10
		if start < 0 {
			start = 0
		}
		for _, msg := range state.Transcript[start:] {
			prefix := msg.Kind.Prefix()
			lines = append(lines, fmt.Sprintf("  [%s] %s: %s%s",
				msg.Timestamp.Format("15:04:05"), msg.Sender, prefix, msg.Content))
		}
	})

	output := strings.Join(lines, "\n")
	return tool.NewResult("read_state", true, output, ""), nil
}

// truncateUTF8Safe truncates a string to at most maxChars characters,
// appending "..." if truncated. Safe for multibyte UTF-8.
func truncateUTF8Safe(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars]) + "..."
}
