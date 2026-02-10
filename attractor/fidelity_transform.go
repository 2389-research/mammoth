// ABOUTME: Applies fidelity-based context compaction and generates preamble strings.
// ABOUTME: Transforms pipeline context based on the fidelity mode before passing to the next node.
package attractor

import (
	"fmt"
	"sort"
	"strings"
)

// FidelityOptions configures the behavior of fidelity-based context compaction.
type FidelityOptions struct {
	MaxKeys        int      // max context keys to keep (default 50 for truncate)
	MaxValueLength int      // max string value length before truncation (default 1024 for compact, 500 for summary:high)
	MaxLogs        int      // max log entries to keep (default 20 for compact)
	Whitelist      []string // custom whitelist keys for summary modes (overrides defaults)
}

// defaultSummaryLowWhitelist is the default set of keys preserved in summary:low mode.
var defaultSummaryLowWhitelist = []string{"last_stage", "outcome", "goal", "error"}

// summaryMediumPatterns are substrings matched against key names in summary:medium mode.
var summaryMediumPatterns = []string{"result", "output", "status"}

// ApplyFidelity applies the given fidelity mode to the context and returns
// the transformed context and a preamble string describing what was done.
func ApplyFidelity(pctx *Context, mode FidelityMode, opts FidelityOptions) (*Context, string) {
	switch mode {
	case FidelityFull:
		return pctx, ""

	case FidelityTruncate:
		return applyTruncate(pctx, opts)

	case FidelityCompact:
		return applyCompact(pctx, opts)

	case FidelitySummaryLow:
		return applySummaryLow(pctx, opts)

	case FidelitySummaryMedium:
		return applySummaryMedium(pctx, opts)

	case FidelitySummaryHigh:
		return applySummaryHigh(pctx, opts)

	default:
		return applyCompact(pctx, opts)
	}
}

// GeneratePreamble produces a human-readable string describing what fidelity
// transformation was applied when transitioning from a previous node.
func GeneratePreamble(prevNode string, mode FidelityMode, removedKeys int) string {
	nodeDesc := prevNode
	if nodeDesc == "" {
		nodeDesc = "previous node"
	}

	switch mode {
	case FidelityFull:
		return fmt.Sprintf("Context from %s passed in full fidelity mode (all keys preserved).", nodeDesc)

	case FidelityTruncate:
		return fmt.Sprintf("Context from %s was truncated to limit keys; %d keys removed.", nodeDesc, removedKeys)

	case FidelityCompact:
		return fmt.Sprintf("Context from %s was compacted; %d keys removed.", nodeDesc, removedKeys)

	case FidelitySummaryLow:
		return fmt.Sprintf("Context from %s was summarized at low detail; %d keys removed.", nodeDesc, removedKeys)

	case FidelitySummaryMedium:
		return fmt.Sprintf("Context from %s was summarized at medium detail; %d keys removed.", nodeDesc, removedKeys)

	case FidelitySummaryHigh:
		return fmt.Sprintf("Context from %s was summarized at high detail; %d keys removed.", nodeDesc, removedKeys)

	default:
		return fmt.Sprintf("Context from %s was transformed; %d keys removed.", nodeDesc, removedKeys)
	}
}

// applyTruncate keeps only the first maxKeys keys (sorted alphabetically).
func applyTruncate(pctx *Context, opts FidelityOptions) (*Context, string) {
	maxKeys := opts.MaxKeys
	if maxKeys == 0 {
		maxKeys = 50
	}

	snap := pctx.Snapshot()
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := NewContext()
	kept := 0
	for _, k := range keys {
		if kept >= maxKeys {
			break
		}
		result.Set(k, snap[k])
		kept++
	}

	removed := len(snap) - kept
	preamble := fmt.Sprintf("Context was truncated to %d keys; %d keys removed.", maxKeys, removed)
	return result, preamble
}

// applyCompact removes internal keys (prefixed with _), truncates long string
// values, and limits log entries.
func applyCompact(pctx *Context, opts FidelityOptions) (*Context, string) {
	maxValueLen := opts.MaxValueLength
	if maxValueLen == 0 {
		maxValueLen = 1024
	}
	maxLogs := opts.MaxLogs
	if maxLogs == 0 {
		maxLogs = 20
	}

	snap := pctx.Snapshot()
	result := NewContext()
	removed := 0

	for k, v := range snap {
		if strings.HasPrefix(k, "_") {
			removed++
			continue
		}
		if s, ok := v.(string); ok && len(s) > maxValueLen {
			result.Set(k, "[truncated]")
		} else {
			result.Set(k, v)
		}
	}

	// Copy and limit logs
	logs := pctx.Logs()
	if len(logs) > maxLogs {
		logs = logs[len(logs)-maxLogs:]
	}
	for _, l := range logs {
		result.AppendLog(l)
	}

	preamble := fmt.Sprintf("Context was compacted; %d keys removed.", removed)
	return result, preamble
}

// applySummaryLow keeps only whitelisted keys.
func applySummaryLow(pctx *Context, opts FidelityOptions) (*Context, string) {
	whitelist := opts.Whitelist
	if whitelist == nil {
		whitelist = defaultSummaryLowWhitelist
	}

	snap := pctx.Snapshot()
	result := NewContext()
	kept := 0

	wlSet := make(map[string]bool, len(whitelist))
	for _, k := range whitelist {
		wlSet[k] = true
	}

	for k, v := range snap {
		if wlSet[k] {
			result.Set(k, v)
			kept++
		}
	}

	removed := len(snap) - kept
	preamble := fmt.Sprintf("Context was summarized at low detail; %d keys removed.", removed)
	return result, preamble
}

// applySummaryMedium keeps whitelisted keys plus keys containing result/output/status patterns.
func applySummaryMedium(pctx *Context, opts FidelityOptions) (*Context, string) {
	whitelist := opts.Whitelist
	if whitelist == nil {
		whitelist = defaultSummaryLowWhitelist
	}

	wlSet := make(map[string]bool, len(whitelist))
	for _, k := range whitelist {
		wlSet[k] = true
	}

	snap := pctx.Snapshot()
	result := NewContext()
	kept := 0

	for k, v := range snap {
		if wlSet[k] || matchesPatterns(k) {
			if !strings.HasPrefix(k, "_") {
				result.Set(k, v)
				kept++
			}
		}
	}

	removed := len(snap) - kept
	preamble := fmt.Sprintf("Context was summarized at medium detail; %d keys removed.", removed)
	return result, preamble
}

// applySummaryHigh keeps all keys but truncates long string values.
func applySummaryHigh(pctx *Context, opts FidelityOptions) (*Context, string) {
	maxValueLen := opts.MaxValueLength
	if maxValueLen == 0 {
		maxValueLen = 500
	}

	snap := pctx.Snapshot()
	result := NewContext()

	for k, v := range snap {
		if s, ok := v.(string); ok && len(s) > maxValueLen {
			result.Set(k, s[:maxValueLen])
		} else {
			result.Set(k, v)
		}
	}

	preamble := fmt.Sprintf("Context was summarized at high detail; %d keys removed.", 0)
	return result, preamble
}

// matchesPatterns checks if a key contains any of the summary medium patterns.
func matchesPatterns(key string) bool {
	lower := strings.ToLower(key)
	for _, p := range summaryMediumPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
