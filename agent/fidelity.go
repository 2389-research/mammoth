// ABOUTME: Maps attractor fidelity modes to agent session context management.
// ABOUTME: Provides ApplyFidelity to filter/compress conversation history based on the configured fidelity mode.

package agent

import (
	"fmt"
	"strings"
	"time"
)

// Fidelity mode constants mirroring attractor.FidelityMode values.
// These are defined here to avoid a circular dependency between agent and attractor packages.
const (
	fidelityFull          = "full"
	fidelityTruncate      = "truncate"
	fidelityCompact       = "compact"
	fidelitySummaryLow    = "summary:low"
	fidelitySummaryMedium = "summary:medium"
	fidelitySummaryHigh   = "summary:high"
)

// minTurnsForReduction is the minimum number of turns before any fidelity mode
// will attempt to reduce history. Below this threshold, all turns are preserved.
const minTurnsForReduction = 10

// ApplyFidelity filters or compresses conversation history based on the fidelity mode.
// It returns a (possibly reduced) copy of the history. The contextWindow parameter
// represents the provider's context window size in tokens, used as a heuristic
// for how aggressively to trim.
//
// Modes:
//   - "full" or "": preserve all history
//   - "truncate": drop older turns from the middle, keeping system/first and recent
//   - "compact": aggressively reduce, keeping only system and recent turns
//   - "summary:low/medium/high": condense older turns into a summary, keeping recent
func ApplyFidelity(history []Turn, mode string, contextWindow int) []Turn {
	// No reduction for empty or unrecognized modes
	if mode == "" || mode == fidelityFull {
		return copyTurns(history)
	}

	// Don't reduce small histories
	if len(history) < minTurnsForReduction {
		return copyTurns(history)
	}

	switch {
	case mode == fidelityTruncate:
		return applyTruncate(history)
	case mode == fidelityCompact:
		return applyCompact(history)
	case strings.HasPrefix(mode, "summary:"):
		return applySummary(history, mode)
	default:
		// Unrecognized mode: preserve everything
		return copyTurns(history)
	}
}

// applyTruncate keeps system turns, the first user turn, and the most recent
// half of the conversation. Drops older turns from the middle.
func applyTruncate(history []Turn) []Turn {
	// Keep the first few turns (system + first user/assistant pair)
	// and the most recent 2/3 of turns
	keepRecent := len(history) * 2 / 3
	if keepRecent < 6 {
		keepRecent = 6
	}

	// Find the boundary between "head" and "tail"
	headEnd := findHeadBoundary(history)
	tailStart := len(history) - keepRecent
	if tailStart <= headEnd {
		// Not enough turns to truncate meaningfully
		return copyTurns(history)
	}

	result := make([]Turn, 0, headEnd+keepRecent)
	// Copy head (system + first exchange)
	result = append(result, history[:headEnd]...)
	// Copy tail (recent turns)
	result = append(result, history[tailStart:]...)

	return result
}

// applyCompact aggressively reduces history: keeps only system turns and the
// most recent quarter of turns.
func applyCompact(history []Turn) []Turn {
	keepRecent := len(history) / 4
	if keepRecent < 4 {
		keepRecent = 4
	}

	// Collect system turns from the head
	var systemTurns []Turn
	for _, turn := range history {
		if turn.TurnType() == "system" {
			systemTurns = append(systemTurns, turn)
		}
	}

	tailStart := len(history) - keepRecent
	if tailStart < 0 {
		tailStart = 0
	}

	result := make([]Turn, 0, len(systemTurns)+keepRecent)
	result = append(result, systemTurns...)

	// Avoid duplicating system turns that are in the tail
	for _, turn := range history[tailStart:] {
		if turn.TurnType() == "system" {
			// Skip system turns already included
			continue
		}
		result = append(result, turn)
	}

	return result
}

// applySummary condenses older turns into a summary SystemTurn, then appends
// recent turns. The detail level controls how many recent turns to keep.
func applySummary(history []Turn, mode string) []Turn {
	// Determine how many recent turns to keep based on detail level
	var keepRecent int
	switch mode {
	case fidelitySummaryLow:
		keepRecent = len(history) / 4
	case fidelitySummaryMedium:
		keepRecent = len(history) / 3
	case fidelitySummaryHigh:
		keepRecent = len(history) / 2
	default:
		keepRecent = len(history) / 3
	}
	if keepRecent < 4 {
		keepRecent = 4
	}

	tailStart := len(history) - keepRecent
	if tailStart <= 0 {
		return copyTurns(history)
	}

	// Build a summary of the older turns
	oldTurns := history[:tailStart]
	summary := buildSummary(oldTurns)

	result := make([]Turn, 0, 1+keepRecent)
	result = append(result, SystemTurn{
		Content:   summary,
		Timestamp: time.Now(),
	})
	result = append(result, history[tailStart:]...)

	return result
}

// buildSummary creates a text summary of the given turns for context condensation.
func buildSummary(turns []Turn) string {
	var b strings.Builder
	b.WriteString("[Context Summary]\n")
	b.WriteString("The following is a condensed summary of earlier conversation:\n\n")

	userCount := 0
	assistantCount := 0
	toolCallCount := 0
	var toolNames []string
	toolNameSeen := make(map[string]bool)

	for _, turn := range turns {
		switch t := turn.(type) {
		case UserTurn:
			userCount++
		case AssistantTurn:
			assistantCount++
			for _, tc := range t.ToolCalls {
				toolCallCount++
				if !toolNameSeen[tc.Name] {
					toolNames = append(toolNames, tc.Name)
					toolNameSeen[tc.Name] = true
				}
			}
		case ToolResultsTurn:
			// Counted via assistant turn tool calls
		}
	}

	b.WriteString(fmt.Sprintf("- %d user messages exchanged\n", userCount))
	b.WriteString(fmt.Sprintf("- %d assistant responses generated\n", assistantCount))
	if toolCallCount > 0 {
		b.WriteString(fmt.Sprintf("- %d tool calls made using: %s\n", toolCallCount, strings.Join(toolNames, ", ")))
	}

	// Include the last user message for continuity
	for i := len(turns) - 1; i >= 0; i-- {
		if ut, ok := turns[i].(UserTurn); ok {
			b.WriteString(fmt.Sprintf("\nLast summarized user request: %s\n", truncateString(ut.Content, 200)))
			break
		}
	}

	return b.String()
}

// truncateString truncates a string to maxLen characters, appending "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// findHeadBoundary returns the index past the initial system turns and first
// user/assistant exchange.
func findHeadBoundary(history []Turn) int {
	idx := 0
	// Skip system turns
	for idx < len(history) && history[idx].TurnType() == "system" {
		idx++
	}
	// Include first user + assistant pair
	if idx < len(history) {
		idx++ // user turn
	}
	if idx < len(history) {
		idx++ // assistant turn
	}
	return idx
}

// copyTurns returns a shallow copy of the turns slice.
func copyTurns(turns []Turn) []Turn {
	result := make([]Turn, len(turns))
	copy(result, turns)
	return result
}
